//! PlM1 container format: the length-prefixed, Kraken-compressed wrapper
//! Palworld puts around every save file.
//!
//! Layout (12-byte header + payload):
//!   bytes 0..4   uncompressed_len (u32 LE)
//!   bytes 4..8   compressed_len (u32 LE)
//!   bytes 8..12  magic, always `PlM1` in every build observed so far
//!   bytes 12..   the Kraken-compressed payload, exactly `compressed_len` bytes

use std::fmt;

/// Reject uncompressed sizes above this before ever allocating an output
/// buffer for them. A hostile or corrupt header could otherwise claim an
/// enormous `uncompressed_len` and blow the process's memory budget. Sized
/// against the 3 GiB wasm budget together with `tarball::MAX_TOTAL_SIZE`
/// (see the rationale there); a real save past this is a `resource_limit`,
/// not corruption.
pub const MAX_UNCOMPRESSED_LEN: usize = 768 * 1024 * 1024;

const MAGIC: &[u8; 4] = b"PlM1";
const HEADER_LEN: usize = 12;

pub struct Header {
    pub uncompressed_len: usize,
    pub compressed_len: usize,
}

#[derive(Debug, PartialEq, Eq)]
pub enum ContainerError {
    /// Fewer than 12 bytes: the container header itself doesn't fit.
    TooShort { len: usize },
    /// The 4-byte magic doesn't start with `Pl` at all — not a recognized
    /// container family, most likely plain corruption.
    UnrecognizedMagic { magic: [u8; 4] },
    /// The magic starts with `Pl` but isn't the `PlM1` variant this parser
    /// speaks (e.g. a hypothetical `PlM2` or `PlZ1`) — a real container,
    /// just one from a newer or different build than this parser supports.
    UnsupportedVariant { magic: [u8; 4] },
    /// `uncompressed_len` exceeds `MAX_UNCOMPRESSED_LEN`.
    UncompressedTooLarge { uncompressed_len: usize },
    /// `compressed_len` claims more payload bytes than are actually present.
    TruncatedPayload { expected: usize, available: usize },
}

impl fmt::Display for ContainerError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ContainerError::TooShort { len } => {
                write!(f, "container header truncated: got {len} bytes, need 12")
            }
            ContainerError::UnrecognizedMagic { magic } => {
                write!(f, "unrecognized container magic: {:02x?}", magic)
            }
            ContainerError::UnsupportedVariant { magic } => {
                write!(
                    f,
                    "unsupported container variant: {:?} (expected PlM1)",
                    String::from_utf8_lossy(magic)
                )
            }
            ContainerError::UncompressedTooLarge { uncompressed_len } => {
                write!(
                    f,
                    "save data ({:.1} MiB uncompressed) exceeds the {} MiB this plugin version \
                     supports -- not corruption",
                    *uncompressed_len as f64 / (1024.0 * 1024.0),
                    MAX_UNCOMPRESSED_LEN / (1024 * 1024)
                )
            }
            ContainerError::TruncatedPayload {
                expected,
                available,
            } => {
                write!(
                    f,
                    "compressed payload truncated: header claims {expected} bytes, only {available} available"
                )
            }
        }
    }
}

/// Parse and validate the 12-byte PlM1 header from `data`. Does not touch
/// the payload bytes themselves — callers slice those out separately via
/// [`payload`].
pub fn parse_header(data: &[u8]) -> Result<Header, ContainerError> {
    if data.len() < HEADER_LEN {
        return Err(ContainerError::TooShort { len: data.len() });
    }

    let uncompressed_len = u32::from_le_bytes(data[0..4].try_into().unwrap()) as usize;
    let compressed_len = u32::from_le_bytes(data[4..8].try_into().unwrap()) as usize;
    let magic: [u8; 4] = data[8..12].try_into().unwrap();

    // Magic is checked before the size cap: garbage input's first four bytes
    // are as likely to look like a huge size claim as anything else, and
    // that's a coincidence, not evidence of a real oversized container. A
    // bad magic is the more specific, more useful diagnosis.
    if &magic != MAGIC {
        if magic.starts_with(b"Pl") {
            return Err(ContainerError::UnsupportedVariant { magic });
        }
        return Err(ContainerError::UnrecognizedMagic { magic });
    }

    if uncompressed_len > MAX_UNCOMPRESSED_LEN {
        return Err(ContainerError::UncompressedTooLarge { uncompressed_len });
    }

    let available = data.len() - HEADER_LEN;
    if compressed_len > available {
        return Err(ContainerError::TruncatedPayload {
            expected: compressed_len,
            available,
        });
    }

    Ok(Header {
        uncompressed_len,
        compressed_len,
    })
}

/// The compressed payload bytes described by `header`, sliced out of `data`.
/// Panics if `data` is shorter than what `parse_header` validated — callers
/// must always call `parse_header` on the same `data` first.
pub fn payload<'a>(data: &'a [u8], header: &Header) -> &'a [u8] {
    &data[HEADER_LEN..HEADER_LEN + header.compressed_len]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn garbage_with_huge_size_and_bad_magic_reports_bad_magic_not_size_cap() {
        // Bytes 0..4 decode as a huge uncompressed_len (over MAX_UNCOMPRESSED_LEN)
        // and bytes 8..12 are not a recognized `Pl*` magic. Before the fix this
        // reported UncompressedTooLarge because the size cap was checked first;
        // the magic check is the more specific diagnosis and must fire instead.
        let mut data = vec![0xFFu8; 20];
        data[8..12].copy_from_slice(b"XXXX");

        match parse_header(&data) {
            Err(ContainerError::UnrecognizedMagic { magic }) => {
                assert_eq!(magic, *b"XXXX");
            }
            Err(other) => panic!("expected UnrecognizedMagic, got {other:?}"),
            Ok(_) => panic!("expected an error, got Ok"),
        }
    }
}
