//! Bounds-checked little-endian byte reader for Palworld's game-native
//! `RawData` payloads.
//!
//! uesave's own archive reader (`SaveGameArchive`) is private to that
//! crate, so this is hand-rolled against the same wire format uesave
//! itself parses for the outer save file -- ported from
//! cheahjs/palworld-save-tools' `FArchiveReader` (`archive.py`, git main).
//! Every read is bounds-checked and returns a [`RawDataError`] instead of
//! panicking on truncated input.

/// A structured classification of a [`RawDataError`], mirroring the
/// variant-enum shape of this crate's other error types (`ContainerError`,
/// `TarError`, `GvasError`) so callers can match on failure kind instead of
/// substring-matching `message`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RawDataErrorKind {
    /// Not enough bytes remained for a read of a known or declared size.
    Truncated { needed: usize, remaining: usize },
    /// A property or array element type this decoder doesn't recognize.
    Unsupported(String),
    /// Recognized but internally inconsistent data (a declared size that
    /// disagrees with what was actually read, a missing required wrapper,
    /// an overflowing length, ...).
    Malformed(String),
}

/// A decode failure naming the property path being read when it failed,
/// mirroring `gvas::GvasError::Parse`'s "at offset: message" style (but
/// path-based, since these payloads carry no absolute byte offsets once
/// nested inside a `RawData` blob).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RawDataError {
    pub path: String,
    pub message: String,
    pub kind: RawDataErrorKind,
}

impl std::fmt::Display for RawDataError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "at {}: {}", self.path, self.message)
    }
}

impl std::error::Error for RawDataError {}

/// A cursor over a `RawData` byte slice, tracking a property-path scope
/// (pushed/popped by callers around each named field) so a truncation or
/// unsupported-type error can name exactly where it happened.
pub(crate) struct Reader<'a> {
    data: &'a [u8],
    pos: usize,
    scope: Vec<String>,
}

impl<'a> Reader<'a> {
    pub fn new(data: &'a [u8], root: &str) -> Self {
        Self {
            data,
            pos: 0,
            scope: vec![root.to_string()],
        }
    }

    pub fn push(&mut self, name: impl Into<String>) {
        self.scope.push(name.into());
    }

    pub fn pop(&mut self) {
        self.scope.pop();
    }

    fn path(&self) -> String {
        self.scope.join(".")
    }

    /// Builds a [`RawDataError`] at the current scope without consuming
    /// any bytes -- for callers that detect a malformed-but-recognized
    /// shape themselves (a declared size that doesn't match what was
    /// read, a missing required wrapper, ...) rather than via a failed
    /// primitive read or an unrecognized type name.
    pub fn error(&self, message: impl Into<String>) -> RawDataError {
        let message = message.into();
        RawDataError {
            path: self.path(),
            kind: RawDataErrorKind::Malformed(message.clone()),
            message,
        }
    }

    /// Builds a [`RawDataError`] for a property or array element type name
    /// this decoder doesn't recognize.
    pub fn unsupported(&self, message: impl Into<String>) -> RawDataError {
        let message = message.into();
        RawDataError {
            path: self.path(),
            kind: RawDataErrorKind::Unsupported(message.clone()),
            message,
        }
    }

    pub fn position(&self) -> usize {
        self.pos
    }

    /// Rewinds to a previously observed position. Used to back out of a
    /// speculative read (e.g. trying to parse an optional nested shape)
    /// without consuming any bytes.
    pub fn seek(&mut self, pos: usize) {
        self.pos = pos.min(self.data.len());
    }

    pub fn remaining(&self) -> usize {
        self.data.len() - self.pos
    }

    /// Every byte from the current position to the end, without consuming
    /// them. Codecs call this once they've decoded every field they
    /// understand, to capture whatever this save's format revision
    /// appends afterward as opaque bytes instead of guessing at further
    /// structure.
    pub fn rest(&self) -> &'a [u8] {
        &self.data[self.pos..]
    }

    fn take(&mut self, n: usize, what: &str) -> Result<&'a [u8], RawDataError> {
        let remaining = self.remaining();
        if remaining < n {
            return Err(RawDataError {
                path: self.path(),
                kind: RawDataErrorKind::Truncated {
                    needed: n,
                    remaining,
                },
                message: format!(
                    "truncated reading {what}: needed {n} bytes, {remaining} remaining"
                ),
            });
        }
        let s = &self.data[self.pos..self.pos + n];
        self.pos += n;
        Ok(s)
    }

    /// Reads a `u32` element count and validates that `count *
    /// min_elem_bytes` could possibly fit in the bytes remaining, without
    /// allocating -- guards the caller's subsequent `Vec::with_capacity`
    /// against a hostile or misaligned huge count overflowing/panicking
    /// on 32-bit targets (wasm32) before a single element has been read.
    /// `min_elem_bytes` is the smallest possible wire width of one
    /// element (e.g. 16 for a guid, 1 for a byte, 4 for the length prefix
    /// of an at-least-empty `FString`).
    pub fn count(&mut self, min_elem_bytes: usize) -> Result<usize, RawDataError> {
        let n = self.u32()?;
        self.checked_count(n, min_elem_bytes)
    }

    /// Validates an already-read element count against the bytes
    /// remaining, without consuming any more input. Used where the count
    /// itself must be read before the element width is known (e.g.
    /// `ArrayProperty(StructProperty)`'s count precedes its element type
    /// name on the wire) -- callers read the raw count via [`Self::u32`]
    /// and validate it here once the width is known.
    pub fn checked_count(&self, n: u32, min_elem_bytes: usize) -> Result<usize, RawDataError> {
        let remaining = self.remaining();
        let needed = (n as u64).saturating_mul(min_elem_bytes as u64);
        if needed > remaining as u64 {
            return Err(RawDataError {
                path: self.path(),
                kind: RawDataErrorKind::Truncated {
                    needed: needed.try_into().unwrap_or(usize::MAX),
                    remaining,
                },
                message: format!(
                    "count {n} × at-least-{min_elem_bytes} bytes exceeds {remaining} remaining"
                ),
            });
        }
        Ok(n as usize)
    }

    /// Reads `n` arbitrary bytes -- for fields whose meaning isn't
    /// (yet) understood but whose fixed width is known.
    pub fn bytes(&mut self, n: usize) -> Result<&'a [u8], RawDataError> {
        self.take(n, "raw bytes")
    }

    pub fn u8(&mut self) -> Result<u8, RawDataError> {
        Ok(self.take(1, "u8")?[0])
    }

    pub fn u16(&mut self) -> Result<u16, RawDataError> {
        Ok(u16::from_le_bytes(self.take(2, "u16")?.try_into().unwrap()))
    }

    pub fn u32(&mut self) -> Result<u32, RawDataError> {
        Ok(u32::from_le_bytes(self.take(4, "u32")?.try_into().unwrap()))
    }

    pub fn i32(&mut self) -> Result<i32, RawDataError> {
        Ok(i32::from_le_bytes(self.take(4, "i32")?.try_into().unwrap()))
    }

    pub fn i64(&mut self) -> Result<i64, RawDataError> {
        Ok(i64::from_le_bytes(self.take(8, "i64")?.try_into().unwrap()))
    }

    pub fn u64(&mut self) -> Result<u64, RawDataError> {
        Ok(u64::from_le_bytes(self.take(8, "u64")?.try_into().unwrap()))
    }

    pub fn f32(&mut self) -> Result<f32, RawDataError> {
        Ok(f32::from_le_bytes(self.take(4, "f32")?.try_into().unwrap()))
    }

    /// UE5 switched `FVector`/`FQuat` components from `float` to `double`;
    /// this fixture's `Vector`/`Quat` structs are 24/32 bytes, confirming
    /// the double-precision layout.
    pub fn f64(&mut self) -> Result<f64, RawDataError> {
        Ok(f64::from_le_bytes(self.take(8, "f64")?.try_into().unwrap()))
    }

    pub fn bool(&mut self) -> Result<bool, RawDataError> {
        Ok(self.u8()? != 0)
    }

    pub fn guid(&mut self) -> Result<[u8; 16], RawDataError> {
        Ok(self.take(16, "guid")?.try_into().unwrap())
    }

    /// A property tag's optional per-instance guid: a presence byte,
    /// followed by 16 guid bytes only if it's non-zero.
    pub fn optional_guid(&mut self) -> Result<Option<[u8; 16]>, RawDataError> {
        if self.u8()? != 0 {
            Ok(Some(self.guid()?))
        } else {
            Ok(None)
        }
    }

    /// Palworld's length-prefixed string (`FString`): an `i32` length; `0`
    /// means empty; positive means that many ASCII bytes including a
    /// trailing NUL; negative means that many (negated) UTF-16LE code
    /// units, also including a trailing NUL.
    pub fn fstring(&mut self) -> Result<String, RawDataError> {
        let len = self.i32()?;
        if len == 0 {
            return Ok(String::new());
        }
        if len < 0 {
            // `-len` panics in debug (and silently wraps on release
            // wasm32) when `len == i32::MIN`, since `i32::MIN` has no
            // positive counterpart -- `unsigned_abs` handles that value
            // correctly, and the `units * 2` byte count is checked
            // separately since it can itself overflow `usize` on wasm32.
            let units = len.unsigned_abs() as usize;
            let byte_len = units
                .checked_mul(2)
                .ok_or_else(|| self.error("utf16 fstring length overflows"))?;
            let bytes = self.take(byte_len, "utf16 fstring")?;
            let codepoints: Vec<u16> = bytes
                .chunks_exact(2)
                .map(|c| u16::from_le_bytes([c[0], c[1]]))
                .collect();
            Ok(String::from_utf16_lossy(
                &codepoints[..codepoints.len().saturating_sub(1)],
            ))
        } else {
            let n = len as usize;
            let bytes = self.take(n, "ascii fstring")?;
            Ok(String::from_utf8_lossy(&bytes[..bytes.len().saturating_sub(1)]).into_owned())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fstring_empty() {
        let data = [0u8, 0, 0, 0];
        let mut r = Reader::new(&data, "root");
        assert_eq!(r.fstring().unwrap(), "");
        assert_eq!(r.remaining(), 0);
    }

    #[test]
    fn fstring_ascii_round_trip_shape() {
        // len=4 ("abc\0"), matching FString's ASCII encoding (length
        // includes the trailing NUL).
        let mut data = vec![4u8, 0, 0, 0];
        data.extend_from_slice(b"abc\0");
        let mut r = Reader::new(&data, "root");
        assert_eq!(r.fstring().unwrap(), "abc");
        assert_eq!(r.remaining(), 0);
    }

    #[test]
    fn fstring_utf16_negative_length() {
        // len=-2 => 2 UTF-16 code units ("a" + NUL).
        let mut data = vec![0xFEu8, 0xFF, 0xFF, 0xFF]; // -2 as i32 LE
        data.extend_from_slice(&[b'a', 0, 0, 0]);
        let mut r = Reader::new(&data, "root");
        assert_eq!(r.fstring().unwrap(), "a");
    }

    #[test]
    fn fstring_i32_min_length_errors_instead_of_panicking() {
        // len=i32::MIN as i32 LE: the naive `-len` negation has no positive
        // i32 counterpart and panics in debug builds on any target. Must
        // error instead -- on this 64-bit host that surfaces as an
        // ordinary truncation (2^32 requested bytes vs. none remaining);
        // on a 32-bit target (wasm32) `units * 2` itself overflows
        // `usize`, caught separately by `checked_mul`. Either way: no
        // panic, and a clean `Err`.
        let data = [0x00u8, 0x00, 0x00, 0x80];
        let mut r = Reader::new(&data, "root");
        assert!(r.fstring().is_err());
    }

    #[test]
    fn guid_reads_16_bytes_in_order() {
        let data: [u8; 16] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];
        let mut r = Reader::new(&data, "root");
        assert_eq!(r.guid().unwrap(), data);
    }

    #[test]
    fn truncated_read_errors_with_path_not_panic() {
        let data = [0u8; 2];
        let mut r = Reader::new(&data, "root");
        r.push("field");
        let err = r.u32().unwrap_err();
        assert_eq!(err.path, "root.field");
        assert!(err.message.contains("truncated"));
        assert_eq!(
            err.kind,
            RawDataErrorKind::Truncated {
                needed: 4,
                remaining: 2
            }
        );
    }

    #[test]
    fn count_rejects_a_hostile_count_without_allocating_or_panicking() {
        // A count claiming far more 32-byte elements than 8 remaining
        // bytes could possibly hold -- must error, not overflow
        // `Vec::with_capacity`'s multiplication on a 32-bit target.
        let mut data = 0x0800_0000u32.to_le_bytes().to_vec();
        data.extend_from_slice(&[0u8; 8]);
        let mut r = Reader::new(&data, "root");
        let err = r.count(32).unwrap_err();
        assert!(err.message.contains("exceeds"));
        assert_eq!(
            err.kind,
            RawDataErrorKind::Truncated {
                needed: 0x0800_0000 * 32,
                remaining: 8
            }
        );
    }

    #[test]
    fn count_accepts_a_count_that_fits_remaining_bytes() {
        let mut data = 2u32.to_le_bytes().to_vec();
        data.extend_from_slice(&[0u8; 32]); // exactly 2 × 16-byte elements
        let mut r = Reader::new(&data, "root");
        assert_eq!(r.count(16).unwrap(), 2);
        assert_eq!(r.remaining(), 32);
    }

    #[test]
    fn checked_count_validates_an_already_read_count_without_reading_more() {
        let data = [0u8; 8];
        let r = Reader::new(&data, "root");
        assert_eq!(r.checked_count(2, 4).unwrap(), 2);
        assert!(r.checked_count(3, 4).is_err());
        assert_eq!(r.remaining(), 8, "checked_count must not consume input");
    }

    #[test]
    fn rest_returns_unconsumed_tail() {
        let data = [1u8, 2, 3, 4, 5];
        let mut r = Reader::new(&data, "root");
        let _ = r.u16().unwrap();
        assert_eq!(r.rest(), &[3, 4, 5]);
    }

    #[test]
    fn seek_rewinds_for_speculative_reads() {
        let data = [1u8, 2, 3, 4];
        let mut r = Reader::new(&data, "root");
        let checkpoint = r.position();
        let _ = r.u32().unwrap();
        assert_eq!(r.remaining(), 0);
        r.seek(checkpoint);
        assert_eq!(r.remaining(), 4);
    }
}

/// Shared synthetic-payload builders for this module's tests and its
/// siblings' -- factored here (rather than duplicated per submodule) since
/// every submodule under `rawdata/` builds ASCII `FString` wire bytes to
/// construct fixtures for its own decoder.
#[cfg(test)]
pub(crate) mod test_support {
    /// Builds an ASCII-encoded `FString`'s wire bytes: an `i32` length
    /// (including the trailing NUL it counts), followed by the string's
    /// bytes and that NUL.
    pub(crate) fn ascii_fstring(s: &str) -> Vec<u8> {
        let mut bytes = Vec::new();
        let len = (s.len() + 1) as i32;
        bytes.extend_from_slice(&len.to_le_bytes());
        bytes.extend_from_slice(s.as_bytes());
        bytes.push(0);
        bytes
    }
}
