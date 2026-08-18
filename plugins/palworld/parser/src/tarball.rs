//! Bounded reading of the tar archive the daemon sends on stdin for a
//! directory-unit save (one entry per file under the world's save folder).

use std::io::Read;

/// Reject any single tar member whose declared size exceeds this. These are
/// PlM1-wrapped, still-compressed save files. `Level.sav` -- the whole world
/// -- is the only one that grows with playtime (tens of MiB compressed on
/// long-running worlds); everything else is well under a megabyte. The cap
/// bounds a hostile or corrupt tar header while leaving room for the largest
/// `Level.sav` that `MAX_UNCOMPRESSED_LEN` can decode (Kraken on GVAS runs at
/// roughly 10x, so ~75 MiB compressed already saturates the decompress cap).
pub const MAX_MEMBER_SIZE: u64 = 192 * 1024 * 1024;

/// Reject an archive whose members sum past this total. Sized against the
/// 3 GiB wasm memory limit (`defaultMaxMemoryPages` in
/// internal/runner/wazero.go), which this cap shares with several other
/// things that are live *at the same time*, not in sequence -- at Level.sav
/// decode time (the last member decoded, see `lib.rs`'s `run()`), the
/// worst-case peak is this cap's own bytes (up to `MAX_TOTAL_SIZE`, until
/// `lib.rs` drops each already-decoded member's buffer) plus the 768 MiB
/// Kraken decompress cap (`container::MAX_UNCOMPRESSED_LEN`) plus the
/// resulting uesave `Save` property tree -- which is not free-riding on the
/// decompressed bytes but its own heap-allocated structure (a `String`/`Vec`
/// per field), roughly on par with or larger than the flat byte count --
/// plus `error_to_raw`'s worst case, where every unparseable property keeps
/// an *additional* raw byte copy of itself inside `Property::Raw` on top of
/// being part of that tree. The three caps keep the same 1:2:6 proportions
/// (total : decompress : budget) that were measured to hold under the old
/// 1 GiB budget (128 MiB : 256 MiB : 1 GiB); worlds past them fail with a
/// `resource_limit` error rather than being silently truncated. Removing the
/// ceiling altogether needs a selective GVAS reader that does not
/// materialize the whole Level.sav tree.
pub const MAX_TOTAL_SIZE: u64 = 384 * 1024 * 1024;

pub struct Member {
    /// Member path as stored in the tar, relative to the save directory
    /// (e.g. `Level.sav`, `Players/x.sav`) -- no world-id prefix, matching
    /// how the daemon tars members (see `lib.rs`'s `world_id` handling).
    pub path: String,
    pub data: Vec<u8>,
}

#[derive(Debug, PartialEq, Eq)]
pub enum TarError {
    /// The archive isn't valid tar at all, or an entry's header/data
    /// couldn't be read.
    Malformed(String),
    /// A member path is absolute or escapes the archive root via `..`.
    UnsafePath(String),
    /// A single member's declared size exceeds `MAX_MEMBER_SIZE`.
    MemberTooLarge { path: String, size: u64 },
    /// The archive's total member size exceeds `MAX_TOTAL_SIZE`.
    TotalTooLarge { total: u64 },
}

impl std::fmt::Display for TarError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TarError::Malformed(msg) => write!(f, "malformed tar archive: {msg}"),
            TarError::UnsafePath(path) => write!(f, "unsafe member path: {path}"),
            TarError::MemberTooLarge { path, size } => {
                write!(
                    f,
                    "member {path} ({:.1} MiB) exceeds the {} MiB this plugin version supports \
                     per file -- not corruption",
                    *size as f64 / (1024.0 * 1024.0),
                    MAX_MEMBER_SIZE / (1024 * 1024)
                )
            }
            TarError::TotalTooLarge { total } => {
                write!(
                    f,
                    "save directory ({:.1} MiB total) exceeds the {} MiB this plugin version \
                     supports -- not corruption",
                    *total as f64 / (1024.0 * 1024.0),
                    MAX_TOTAL_SIZE / (1024 * 1024)
                )
            }
        }
    }
}

/// Reject member paths that are absolute or contain a `..` component,
/// which could otherwise be used to write or read outside the archive root.
fn is_unsafe_path(path: &str) -> bool {
    if path.starts_with('/') {
        return true;
    }
    path.split('/').any(|component| component == "..")
}

/// Read every regular file out of a tar archive, enforcing per-member and
/// total size caps before any data is read into memory.
pub fn read_members(data: &[u8]) -> Result<Vec<Member>, TarError> {
    read_members_with_limits(data, MAX_MEMBER_SIZE, MAX_TOTAL_SIZE)
}

/// Same as [`read_members`] but with the per-member and total size caps
/// passed in explicitly, so the cap logic can be exercised with small,
/// fast-to-construct archives instead of ones that actually approach
/// [`MAX_MEMBER_SIZE`]/[`MAX_TOTAL_SIZE`] in real bytes.
fn read_members_with_limits(
    data: &[u8],
    max_member: u64,
    max_total: u64,
) -> Result<Vec<Member>, TarError> {
    let mut archive = tar::Archive::new(data);
    let entries = archive
        .entries()
        .map_err(|e| TarError::Malformed(e.to_string()))?;

    let mut members = Vec::new();
    let mut total: u64 = 0;

    for entry in entries {
        let entry = entry.map_err(|e| TarError::Malformed(e.to_string()))?;

        if !entry.header().entry_type().is_file() {
            continue;
        }

        let path = entry
            .path()
            .map_err(|e| TarError::Malformed(e.to_string()))?
            .to_string_lossy()
            .into_owned();

        if is_unsafe_path(&path) {
            return Err(TarError::UnsafePath(path));
        }

        // `entry.size()` (not `entry.header().size()`) honors a PAX
        // extended header overriding the base header's size field --
        // relying on the raw header alone would let a PAX-extended
        // member's declared size lie small while its real size (and the
        // bytes the reader actually produces) is far larger.
        let size = entry.size();
        if size > max_member {
            return Err(TarError::MemberTooLarge { path, size });
        }
        total += size;
        if total > max_total {
            return Err(TarError::TotalTooLarge { total });
        }

        let mut buf = Vec::with_capacity(size as usize);
        // Bound the actual read independently of the declared size, so
        // the cap holds regardless of header form -- not just on
        // whatever `size` claims.
        let read = entry
            .take(max_member + 1)
            .read_to_end(&mut buf)
            .map_err(|e| TarError::Malformed(e.to_string()))?;
        if read as u64 > max_member {
            return Err(TarError::MemberTooLarge {
                path,
                size: read as u64,
            });
        }

        members.push(Member { path, data: buf });
    }

    Ok(members)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn build_tar(entries: &[(&str, &[u8])]) -> Vec<u8> {
        let mut builder = tar::Builder::new(Vec::new());
        for (path, data) in entries {
            let mut header = tar::Header::new_gnu();
            header.set_size(data.len() as u64);
            header.set_mode(0o644);
            header.set_entry_type(tar::EntryType::Regular);
            builder.append_data(&mut header, path, *data).unwrap();
        }
        builder.into_inner().unwrap()
    }

    #[test]
    fn member_too_large_check_uses_pax_extension_size_not_lying_base_header() {
        // A PAX extension overriding the entry's size to something far
        // larger than the base header's own (small) size field --
        // `entry.size()` (which honors the PAX override) must be what the
        // cap checks against, not `entry.header().size()` (the base header
        // alone), or a PAX-extended member could slip past the cap
        // entirely.
        let mut builder = tar::Builder::new(Vec::new());
        builder
            .append_pax_extensions([("size", b"5000".as_slice())])
            .unwrap();

        let mut header = tar::Header::new_gnu();
        header.set_size(10); // base header lies small
        header.set_mode(0o644);
        header.set_entry_type(tar::EntryType::Regular);
        builder
            .append_data(&mut header, "big.sav", &[0u8; 10][..])
            .unwrap();

        let tar = builder.into_inner().unwrap();

        match read_members_with_limits(&tar, 1000, 1_000_000) {
            Err(TarError::MemberTooLarge { size, .. }) => assert_eq!(size, 5000),
            Err(other) => panic!("expected MemberTooLarge, got {other:?}"),
            Ok(_) => panic!("expected an error, got Ok"),
        }
    }

    #[test]
    fn total_size_over_cap_is_rejected_without_real_gigabyte_files() {
        // Each member is comfortably under a tiny per-member cap; only their
        // sum exceeds a tiny total cap. Exercises TotalTooLarge without
        // allocating anywhere near MAX_TOTAL_SIZE.
        let a = vec![0u8; 600];
        let b = vec![0u8; 600];
        let c = vec![0u8; 600];
        let tar = build_tar(&[("a.sav", &a), ("b.sav", &b), ("c.sav", &c)]);

        match read_members_with_limits(&tar, 1000, 1500) {
            Err(TarError::TotalTooLarge { total }) => assert_eq!(total, 1800),
            Err(other) => panic!("expected TotalTooLarge, got {other:?}"),
            Ok(_) => panic!("expected an error, got Ok"),
        }
    }
}
