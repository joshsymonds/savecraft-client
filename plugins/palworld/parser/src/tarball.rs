//! Bounded reading of the tar archive the daemon sends on stdin for a
//! directory-unit save (one entry per file under the world's save folder).

use std::io::Read;

/// Reject any single tar member whose declared size exceeds this. These are
/// PlM1-wrapped, still-compressed save files — real ones are well under a
/// megabyte, so this leaves generous headroom while still bounding a hostile
/// or corrupt tar header.
pub const MAX_MEMBER_SIZE: u64 = 64 * 1024 * 1024;

/// Reject an archive whose members sum past this total. Sized so that the
/// stdin buffer, member copies, and this cap together stay under the 1 GiB
/// wasm memory limit (`defaultMaxMemoryPages` in internal/runner/wazero.go)
/// alongside the 256 MiB Kraken decompress cap — a 512 MiB total cap left no
/// headroom and was unreachable in practice.
pub const MAX_TOTAL_SIZE: u64 = 128 * 1024 * 1024;

pub struct Member {
    /// Member path as stored in the tar, e.g. `1FCE.../Level.sav`.
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
    TotalTooLarge,
}

impl std::fmt::Display for TarError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TarError::Malformed(msg) => write!(f, "malformed tar archive: {msg}"),
            TarError::UnsafePath(path) => write!(f, "unsafe member path: {path}"),
            TarError::MemberTooLarge { path, size } => {
                write!(
                    f,
                    "member {path} too large: {size} bytes (cap {MAX_MEMBER_SIZE})"
                )
            }
            TarError::TotalTooLarge => {
                write!(
                    f,
                    "archive exceeds total size cap of {MAX_TOTAL_SIZE} bytes"
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
        let mut entry = entry.map_err(|e| TarError::Malformed(e.to_string()))?;

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

        let size = entry
            .header()
            .size()
            .map_err(|e| TarError::Malformed(e.to_string()))?;
        if size > max_member {
            return Err(TarError::MemberTooLarge { path, size });
        }
        total += size;
        if total > max_total {
            return Err(TarError::TotalTooLarge);
        }

        let mut buf = Vec::with_capacity(size as usize);
        entry
            .read_to_end(&mut buf)
            .map_err(|e| TarError::Malformed(e.to_string()))?;

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
    fn total_size_over_cap_is_rejected_without_real_gigabyte_files() {
        // Each member is comfortably under a tiny per-member cap; only their
        // sum exceeds a tiny total cap. Exercises TotalTooLarge without
        // allocating anywhere near MAX_TOTAL_SIZE.
        let a = vec![0u8; 600];
        let b = vec![0u8; 600];
        let c = vec![0u8; 600];
        let tar = build_tar(&[("a.sav", &a), ("b.sav", &b), ("c.sav", &c)]);

        match read_members_with_limits(&tar, 1000, 1500) {
            Err(TarError::TotalTooLarge) => {}
            Err(other) => panic!("expected TotalTooLarge, got {other:?}"),
            Ok(_) => panic!("expected an error, got Ok"),
        }
    }
}
