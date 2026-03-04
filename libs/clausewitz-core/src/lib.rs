//! Shared infrastructure for Clausewitz-engine (Paradox) Savecraft plugins.
//!
//! Provides jomini-based parsing, ZIP envelope handling, ndjson output,
//! and stdin/stdout WASI scaffolding. Game-specific plugins depend on this
//! crate and implement their own section extraction and reference data.

pub mod ndjson;
pub mod envelope;
