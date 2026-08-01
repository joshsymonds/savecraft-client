//! ndjson output protocol for Savecraft plugins.
//!
//! Plugins emit newline-delimited JSON to stdout:
//! - `{"type":"status","message":"..."}` — progress updates
//! - `{"type":"result","identity":{...},"summary":"...","sections":{...}}` — final output
//! - `{"type":"error","errorType":"...","message":"...","byteOffset":...}` — fatal errors
//!
//! This mirrors the shape `clausewitz-core::ndjson` emits (and what
//! `internal/runner/wazero.go` parses), duplicated locally rather than shared:
//! clausewitz-core is Clausewitz-specific and Apache-licensed, while this
//! plugin is GPL-3.0-or-later and self-contained.

use serde::Serialize;
use std::collections::HashMap;
use std::io::{self, Write};

#[derive(Serialize)]
pub struct Identity {
    #[serde(rename = "saveName")]
    pub save_name: String,
    #[serde(rename = "gameId")]
    pub game_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub extra: Option<serde_json::Value>,
}

#[derive(Serialize)]
pub struct Section {
    pub description: String,
    pub data: serde_json::Value,
}

#[derive(Serialize)]
#[serde(tag = "type")]
enum Output {
    #[serde(rename = "status")]
    Status { message: String },
    #[serde(rename = "result")]
    Result {
        identity: Identity,
        summary: String,
        sections: HashMap<String, Section>,
    },
    #[serde(rename = "error")]
    Error {
        #[serde(rename = "errorType")]
        error_type: String,
        message: String,
        #[serde(rename = "byteOffset", skip_serializing_if = "Option::is_none")]
        byte_offset: Option<i64>,
    },
}

pub fn emit_status(message: &str) {
    let out = Output::Status {
        message: message.to_string(),
    };
    let mut stdout = io::stdout().lock();
    let _ = serde_json::to_writer(&mut stdout, &out);
    let _ = stdout.write_all(b"\n");
}

/// Encodes a result line's exact bytes (including the trailing newline)
/// without writing them anywhere -- factored out of [`emit_result`] so
/// tests can assert on the real encoded envelope instead of hand-building
/// a parallel one that could drift from what the daemon actually receives.
pub(crate) fn encode_result(
    identity: Identity,
    summary: String,
    sections: HashMap<String, Section>,
) -> Vec<u8> {
    let out = Output::Result {
        identity,
        summary,
        sections,
    };
    let mut bytes = serde_json::to_vec(&out).unwrap_or_default();
    bytes.push(b'\n');
    bytes
}

pub fn emit_result(identity: Identity, summary: String, sections: HashMap<String, Section>) {
    let bytes = encode_result(identity, summary, sections);
    let mut stdout = io::stdout().lock();
    let _ = stdout.write_all(&bytes);
}

pub fn emit_error(error_type: &str, message: &str) {
    emit_error_at(error_type, message, None);
}

pub fn emit_error_at(error_type: &str, message: &str, byte_offset: Option<i64>) {
    let out = Output::Error {
        error_type: error_type.to_string(),
        message: message.to_string(),
        byte_offset,
    };
    let mut stdout = io::stdout().lock();
    let _ = serde_json::to_writer(&mut stdout, &out);
    let _ = stdout.write_all(b"\n");
}
