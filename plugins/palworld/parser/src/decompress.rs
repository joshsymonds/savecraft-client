//! Kraken decompression, isolated behind a small free function so the
//! backend can be swapped for a clean-room implementation later without
//! touching callers. The `oozextract` crate is a reverse-engineered decoder
//! and has been observed to panic (rather than return an error) on some
//! corrupted input, so every call is wrapped in `catch_unwind` and surfaced
//! as a normal `Result` — a corrupt save file must never crash the plugin
//! process. (On native builds, where `panic=unwind`; see `main.rs`'s panic
//! hook for the wasm32-wasip1 case, where `panic=abort` makes this
//! `catch_unwind` a no-op.)

use std::panic;

/// Decompress `payload` into exactly `expected_len` bytes, or return an
/// error. Never lets a panic escape. This is the clean-room swap point: a
/// future replacement decoder only needs to change the body of this
/// function, not any caller.
pub fn decompress(payload: &[u8], expected_len: usize) -> Result<Vec<u8>, String> {
    let result = panic::catch_unwind(|| {
        let mut out = vec![0u8; expected_len];
        let mut extractor = oozextract::Extractor::new();
        let written = extractor
            .read_from_slice(payload, &mut out)
            .map_err(|e| format!("kraken decompress failed: {e}"))?;
        // oozextract's read_sync breaks out of its loop early (`0 => break`)
        // whenever a quantum produces no bytes, returning short of the
        // pre-zeroed `out` buffer instead of an error. Without this check a
        // truncated stream would silently "succeed" as a zero-padded save.
        if written != expected_len {
            return Err(format!(
                "kraken decompress produced {written} bytes, expected {expected_len}"
            ));
        }
        Ok(out)
    });

    match result {
        Ok(inner) => inner,
        Err(_) => Err("kraken decompress panicked on corrupted input".to_string()),
    }
}
