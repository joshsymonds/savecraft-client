//! Palworld save parser library: PlM1 container -> Kraken -> GVAS pipeline,
//! Palworld's own `RawData` codecs, and the section builders that assemble
//! them into the plugin's ndjson output. `main.rs` is a thin stdin/argv
//! wrapper over [`run`].

mod container;
mod decompress;
pub mod gvas;
mod ndjson;
pub mod rawdata;
mod sections;
mod tarball;

use gvas::GvasError;
use std::collections::HashMap;
use std::io::Read;
use std::sync::atomic::{AtomicBool, Ordering};
use tarball::TarError;

/// Set by the panic hook installed in `run` when it has already emitted a
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

/// Reads the tar archive on stdin, decodes the save directory it contains,
/// and emits ndjson status/result/error lines to stdout — the plugin's
/// entire behavior, minus the process-level argv/exit-code plumbing that
/// `main.rs` handles. Returns the process exit code the caller should use.
pub fn run() -> i32 {
    install_panic_hook();

    let mut input = Vec::new();
    if let Err(e) = std::io::stdin().read_to_end(&mut input) {
        ndjson::emit_error("parse_error", &format!("failed to read stdin: {e}"));
        return 1;
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
            return 1;
        }
        ndjson::emit_error(
            "corrupt_file",
            "input is not a valid tar archive (missing ustar magic at byte 257) and is not a \
             recognized PlM1 container either",
        );
        return 1;
    }

    ndjson::emit_status("Reading save directory...");

    let members = match tarball::read_members(&input) {
        Ok(m) => m,
        Err(e) => {
            ndjson::emit_error(tar_error_type(&e), &e.to_string());
            return 1;
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

    // Take ownership of every member's bytes by path, keeping only the ones
    // this plugin actually reads -- anything else the daemon tarred (e.g.
    // WorldOption.sav, LocalData.sav) is dropped immediately rather than
    // held alive for the rest of the run (see P1+P2). `members` (and every
    // byte buffer it doesn't hand off here) is dropped when this loop ends.
    let mut owned: HashMap<String, Vec<u8>> = HashMap::new();
    for m in members {
        if m.path == "Level.sav"
            || m.path == "LevelMeta.sav"
            || (m.path.starts_with("Players/") && m.path.ends_with(".sav"))
        {
            owned.insert(m.path, m.data);
        }
    }

    // LevelMeta.sav and every Players/*.sav are decoded (and each one's
    // bytes dropped) before Level.sav -- by far the largest, most
    // memory-hungry member -- is decoded last, so its decompressed GVAS
    // buffer and resulting `Save` tree are never alive at the same time as
    // any other member's bytes (see P1+P2).
    let level_meta_fields = match owned.remove("LevelMeta.sav") {
        Some(data) => {
            ndjson::emit_status("Decoding LevelMeta.sav...");
            match gvas::decode(data) {
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

    ndjson::emit_status("Decoding player saves...");
    let player_paths: Vec<String> = owned
        .keys()
        .filter(|p| p.starts_with("Players/") && p.ends_with(".sav"))
        .cloned()
        .collect();
    let mut players = Vec::with_capacity(player_paths.len());
    for path in player_paths {
        let data = owned
            .remove(&path)
            .expect("path was just collected from owned's own keys");
        match gvas::decode(data) {
            Ok(s) => players.push(s),
            Err(e) => {
                ndjson::emit_status(&format!(
                    "{path} failed to decode ({e}); excluding it from the players/pals/inventory sections"
                ));
            }
        }
    }

    let level_data = match owned.remove("Level.sav") {
        Some(d) => d,
        None => {
            ndjson::emit_error("corrupt_file", "save directory is missing Level.sav");
            return 1;
        }
    };

    ndjson::emit_status("Decoding Level.sav...");
    let level_save = match gvas::decode(level_data) {
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
            return 1;
        }
    };

    let built = sections::build_all(&level_save, level_meta_fields.as_ref(), &players);
    for warning in &built.warnings {
        ndjson::emit_status(warning);
    }

    if !built.unsupported_paths.is_empty() {
        ndjson::emit_status(&format!(
            "save format newer than this plugin supports at: {}",
            built.unsupported_paths.join(", ")
        ));
    }

    // A mixed world (some RawData understood, some not) still degrades
    // gracefully via the warnings above -- but if *nothing* decoded
    // anywhere and at least one failure was specifically an unrecognized
    // property type, this save's format revision has drifted past what
    // this plugin's codecs support wholesale. Report that plainly instead
    // of a near-empty "successful" result (see sections::BuildResult's
    // `critical_unsupported` doc / C2).
    if built.critical_unsupported {
        let paths = built.unsupported_paths.join(", ");
        ndjson::emit_error(
            "unsupported_version",
            &format!(
                "save format is newer than this plugin version supports (unrecognized property \
                 types at: {paths}); the world could not be decoded"
            ),
        );
        return 1;
    }

    let sections_map = build_sections_map(built);

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
    0
}

/// Wraps every list-shaped section in a named object -- the daemon
/// (`internal/daemon/daemon.go`'s `protoSections`) silently drops any
/// section whose top-level JSON value isn't an object (see
/// `docs/plugins.md`'s "Arrays and scalar values must be nested under a
/// descriptive key"), so a bare array here would vanish before ever
/// reaching a player. Factored out of [`run`] so tests can exercise this
/// exact wrapping (see `sections::tests::scale_*`) without spawning the
/// compiled binary.
pub(crate) fn build_sections_map(built: sections::BuildResult) -> HashMap<String, ndjson::Section> {
    let sections::BuildResult {
        overview,
        players,
        pals_party,
        pals_storage,
        guild,
        bases,
        inventory,
        warnings: _,
        unsupported_paths: _,
        critical_unsupported: _,
    } = built;

    let mut sections_map = HashMap::new();
    sections_map.insert(
        "overview".to_string(),
        ndjson::Section {
            description:
                "World overview: name, host player, guild, base, and save version/engine info"
                    .to_string(),
            data: serde_json::to_value(&overview).unwrap_or_default(),
        },
    );
    sections_map.insert(
        "players".to_string(),
        ndjson::Section {
            description: "Each player: name, level, tech unlocks, and Paldeck completion counts"
                .to_string(),
            data: serde_json::to_value(&sections::PlayersSection { players }).unwrap_or_default(),
        },
    );
    sections_map.insert(
        "pals_party".to_string(),
        ndjson::Section {
            description: "Pals in the active party".to_string(),
            data: serde_json::to_value(&sections::PalsSection { pals: pals_party })
                .unwrap_or_default(),
        },
    );
    sections_map.insert(
        "pals_storage".to_string(),
        ndjson::Section {
            description: "Pals in the pal box".to_string(),
            data: serde_json::to_value(&sections::PalsSection { pals: pals_storage })
                .unwrap_or_default(),
        },
    );
    sections_map.insert(
        "guild".to_string(),
        ndjson::Section {
            description: "Guild name and member roster".to_string(),
            data: serde_json::to_value(&sections::GuildsSection { guilds: guild })
                .unwrap_or_default(),
        },
    );
    sections_map.insert(
        "bases".to_string(),
        ndjson::Section {
            description: sections::BASES_SECTION_DESCRIPTION.to_string(),
            data: serde_json::to_value(&sections::BasesSection {
                count: bases.len(),
                bases,
            })
            .unwrap_or_default(),
        },
    );
    sections_map.insert(
        "inventory".to_string(),
        ndjson::Section {
            description: "Player item containers and dynamic (egg) items".to_string(),
            data: serde_json::to_value(&sections::InventorySection {
                inventories: inventory,
            })
            .unwrap_or_default(),
        },
    );
    sections_map
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
        TarError::TotalTooLarge { .. } => "corrupt_file",
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
