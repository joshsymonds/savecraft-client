//! ZIP envelope handling for Clausewitz saves.
//!
//! Modern Paradox saves (Vic3, CK3, EU5, etc.) are ZIP archives containing
//! `gamestate` and `meta` files. This module extracts them from the ZIP
//! and provides the raw bytes to the parser.

use std::io::{Cursor, Read};

pub struct SaveEnvelope {
    pub meta: Vec<u8>,
    pub gamestate: Vec<u8>,
}

/// Extract `meta` and `gamestate` from a ZIP-encoded save file.
/// Returns the raw bytes of each — the caller decides whether to parse
/// them as text or binary Clausewitz format (jomini handles both).
pub fn extract(data: &[u8]) -> Result<SaveEnvelope, String> {
    let cursor = Cursor::new(data);
    let mut archive =
        zip::ZipArchive::new(cursor).map_err(|e| format!("failed to open ZIP archive: {e}"))?;

    let meta = read_entry(&mut archive, "meta")?;
    let gamestate = read_entry(&mut archive, "gamestate")?;

    Ok(SaveEnvelope { meta, gamestate })
}

fn read_entry(archive: &mut zip::ZipArchive<Cursor<&[u8]>>, name: &str) -> Result<Vec<u8>, String> {
    let mut file = archive
        .by_name(name)
        .map_err(|_| format!("ZIP archive missing '{name}' entry"))?;
    let mut buf = Vec::with_capacity(file.size() as usize);
    file.read_to_end(&mut buf)
        .map_err(|e| format!("failed to read '{name}' from ZIP: {e}"))?;
    Ok(buf)
}
