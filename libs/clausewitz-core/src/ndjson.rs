//! ndjson output protocol for Savecraft plugins.
//!
//! Plugins emit newline-delimited JSON to stdout:
//! - `{"type":"status","message":"..."}` — progress updates
//! - `{"type":"result","identity":{...},"summary":"...","sections":{...}}` — final output
//! - `{"type":"error","errorType":"...","message":"..."}` — fatal errors

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

pub fn emit_result(identity: Identity, summary: String, sections: HashMap<String, Section>) {
    let out = Output::Result {
        identity,
        summary,
        sections,
    };
    let mut stdout = io::stdout().lock();
    let _ = serde_json::to_writer(&mut stdout, &out);
    let _ = stdout.write_all(b"\n");
}

pub fn emit_error(error_type: &str, message: &str) {
    let out = Output::Error {
        error_type: error_type.to_string(),
        message: message.to_string(),
    };
    let mut stdout = io::stdout().lock();
    let _ = serde_json::to_writer(&mut stdout, &out);
    let _ = stdout.write_all(b"\n");
}
