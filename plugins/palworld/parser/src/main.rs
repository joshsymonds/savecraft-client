mod container;
mod decompress;
mod gvas;
mod ndjson;
mod sections;
mod tarball;

use gvas::GvasError;
use std::collections::HashMap;
use std::io::Read;
use std::sync::atomic::{AtomicBool, Ordering};
use tarball::{Member, TarError};

/// Set by the panic hook installed in `main` when it has already emitted a
/// structured ndjson error for an in-flight panic. wasm32-wasip1 compiles
/// with panic=abort by default (no override anywhere in this repo), so
/// `catch_unwind` around the Kraken decoder (see decompress.rs) is a no-op
/// there: the process aborts the instant the panic fires, before unwinding
/// ever reaches a `catch_unwind` call site, leaving a bare wasm trap with no
/// structured error. The panic hook runs before that abort and is the only
/// place left to emit `{"type":"error",...}` on stdout — internal/runner/
/// wazero.go gives an already-parsed ndjson error line priority over the
/// trap. On the host (panic=unwind, used by `cargo test`), the hook still
/// fires before `catch_unwind` recovers the panic, so this flag lets the
/// normal error-handling call site below skip re-emitting — otherwise every
/// caught panic would produce two ndjson error lines instead of one.
static PANIC_REPORTED: AtomicBool = AtomicBool::new(false);

fn install_panic_hook() {
    std::panic::set_hook(Box::new(|info| {
        PANIC_REPORTED.store(true, Ordering::SeqCst);
        ndjson::emit_error("corrupt_file", &format!("plugin panicked: {info}"));
    }));
}

/// Real tar archives (as written by the `tar` crate, which is what the
/// daemon uses) carry the ustar magic `ustar` at byte offset 257 of every
/// header. Checking for that is a more reliable tar/not-tar sniff than
/// guessing from the first member's content, and lets a raw PlM1 container
/// (no tar wrapper at all — an older daemon's single-file protocol) be told
/// apart from actual corruption.
const USTAR_MAGIC_OFFSET: usize = 257;
const USTAR_MAGIC: &[u8] = b"ustar";

fn looks_like_tar(input: &[u8]) -> bool {
    input.len() >= USTAR_MAGIC_OFFSET + USTAR_MAGIC.len()
        && &input[USTAR_MAGIC_OFFSET..USTAR_MAGIC_OFFSET + USTAR_MAGIC.len()] == USTAR_MAGIC
}

fn looks_like_raw_plm1(input: &[u8]) -> bool {
    input.len() >= 12 && &input[8..12] == b"PlM1"
}

fn main() {
    install_panic_hook();

    let mut input = Vec::new();
    if let Err(e) = std::io::stdin().read_to_end(&mut input) {
        ndjson::emit_error("parse_error", &format!("failed to read stdin: {e}"));
        std::process::exit(1);
    }

    if !looks_like_tar(&input) {
        if looks_like_raw_plm1(&input) {
            // A raw PlM1 container (no tar wrapper) means the daemon sent a
            // single file directly, the way an older, non-directory-unit-aware
            // daemon would.
            ndjson::emit_error(
                "unsupported_version",
                "input is a raw PlM1 save container, not a tar archive; this plugin needs \
                 directory-unit support (min_daemon_version) that this daemon does not provide",
            );
            std::process::exit(1);
        }
        ndjson::emit_error(
            "corrupt_file",
            "input is not a valid tar archive (missing ustar magic at byte 257) and is not a \
             recognized PlM1 container either",
        );
        std::process::exit(1);
    }

    ndjson::emit_status("Reading save directory...");

    let members = match tarball::read_members(&input) {
        Ok(m) => m,
        Err(e) => {
            ndjson::emit_error(tar_error_type(&e), &e.to_string());
            std::process::exit(1);
        }
    };
    // The daemon tars members relative to the save directory and passes the
    // directory's base name as argv[1] separately (see world_id below) — the
    // full stdin buffer is never needed again once its contents are copied
    // into `members`, so free it before decompression adds its own memory
    // pressure (256 MiB Kraken cap) on top.
    drop(input);

    // The daemon tars members RELATIVE to the save directory (Level.sav,
    // LevelMeta.sav, Players/x.sav — no world-id prefix) and passes the
    // directory's base name as argv[1] (internal/runner/wazero.go's
    // WithArgs(gameID, fileName)); the tar itself carries no world id at all.
    let world_id = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "world".to_string());

    let level_member = match find_member(&members, "Level.sav") {
        Some(m) => m,
        None => {
            ndjson::emit_error("corrupt_file", "save directory is missing Level.sav");
            std::process::exit(1);
        }
    };

    ndjson::emit_status("Decoding Level.sav...");
    let level_save = match gvas::decode(&level_member.data) {
        Ok(s) => s,
        Err(e) => {
            // If a panic already reported a structured error (see
            // PANIC_REPORTED above), emitting a second one here would give
            // the daemon two "error" lines for one failure.
            if !PANIC_REPORTED.load(Ordering::SeqCst) {
                ndjson::emit_error_at(
                    gvas_error_type(&e),
                    &format!("Level.sav: {e}"),
                    gvas_error_offset(&e),
                );
            }
            std::process::exit(1);
        }
    };

    let level_meta_fields = match find_member(&members, "LevelMeta.sav") {
        Some(m) => {
            ndjson::emit_status("Decoding LevelMeta.sav...");
            match gvas::decode(&m.data) {
                Ok(s) => Some(sections::level_meta_fields(&s)),
                Err(e) => {
                    ndjson::emit_status(&format!(
                        "LevelMeta.sav failed to decode ({e}); falling back to world id for identity"
                    ));
                    None
                }
            }
        }
        None => {
            ndjson::emit_status("LevelMeta.sav not found; falling back to world id for identity");
            None
        }
    };

    let world_name = level_meta_fields
        .as_ref()
        .and_then(|f| f.world_name.clone());
    let save_name = world_name.clone().unwrap_or_else(|| world_id.clone());

    let overview = sections::build_overview(level_meta_fields.as_ref(), &level_save);

    let mut sections_map = HashMap::new();
    sections_map.insert(
        "overview".to_string(),
        ndjson::Section {
            description: "World overview: name, host player, and save version/engine info"
                .to_string(),
            data: serde_json::to_value(&overview).unwrap_or_default(),
        },
    );

    let summary = match &world_name {
        Some(name) => name.clone(),
        None => format!("World {world_id}"),
    };

    let identity = ndjson::Identity {
        save_name,
        game_id: "palworld".to_string(),
        extra: Some(serde_json::json!({ "worldId": world_id })),
    };

    ndjson::emit_result(identity, summary, sections_map);
}

fn find_member<'a>(members: &'a [Member], filename: &str) -> Option<&'a Member> {
    members
        .iter()
        .find(|m| m.path.rsplit('/').next() == Some(filename))
}

fn tar_error_type(e: &TarError) -> &'static str {
    match e {
        TarError::Malformed(_) => "corrupt_file",
        TarError::UnsafePath(_) => "corrupt_file",
        // Not "resource_limit": the daemon's proto ParseErrorType enum has no
        // such variant (toParseErrorType in internal/daemon/daemon.go falls
        // through to PARSE_ERROR_TYPE_PARSE_ERROR for anything unrecognized),
        // so these caps were silently downgraded to a misleading parse error.
        TarError::MemberTooLarge { .. } => "corrupt_file",
        TarError::TotalTooLarge => "corrupt_file",
    }
}

fn gvas_error_type(e: &GvasError) -> &'static str {
    match e {
        GvasError::Container(container::ContainerError::UnsupportedVariant { .. }) => {
            "unsupported_version"
        }
        // Includes UncompressedTooLarge: not "resource_limit", for the same
        // reason as tar_error_type's caps above.
        GvasError::Container(_) => "corrupt_file",
        GvasError::Decompress(_) => "corrupt_file",
        GvasError::Parse { .. } => "parse_error",
    }
}

fn gvas_error_offset(e: &GvasError) -> Option<i64> {
    match e {
        GvasError::Parse { offset, .. } => Some(*offset as i64),
        _ => None,
    }
}
