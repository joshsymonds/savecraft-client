//! End-to-end tests for the palworld-parser binary: build a tar in memory
//! from the real fixture files (byte-manipulated for the adversarial
//! cases), pipe it through the compiled binary's stdin, and assert on the
//! ndjson lines it emits on stdout. This exercises the exact contract
//! `internal/runner/wazero.go` parses, black-box, the same way the daemon
//! does — including passing the world id as argv[1], the way
//! `WithArgs(gameID, fileName)` does, and tarring members RELATIVE to the
//! save directory (no world-id path prefix).

use std::io::Write;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use uesave::{Properties, Property, StructValue, ValueVec};

const WORLD_ID: &str = "1FCE97C34D214643B96A23A20A9E27D1";
const LIVE_WORLD_ID: &str = "live-20260731";

fn push_path(prefix: &str, name: &str) -> String {
    if prefix.is_empty() {
        name.to_string()
    } else {
        format!("{prefix}.{name}")
    }
}

/// Recursively collects the dotted path of every `Property::Raw` (or
/// `StructValue::Raw`) reachable from `props` -- a property that failed to
/// parse and was silently downgraded by `error_to_raw(true)` (see
/// gvas.rs). Paths follow the exact scope convention uesave's own `Types`
/// lookup uses when resolving TYPE_HINTS (see gvas.rs's TYPE_HINTS doc
/// comment): no synthetic "Key"/"Value" component is inserted for a map
/// entry's struct fields -- the map's own property name is followed
/// directly by the nested field name.
fn collect_raw_paths(props: &Properties, prefix: &str, out: &mut Vec<String>) {
    for (key, prop) in props {
        let path = push_path(prefix, &key.1);
        collect_raw_paths_in_property(prop, &path, out);
    }
}

fn collect_raw_paths_in_property(prop: &Property, path: &str, out: &mut Vec<String>) {
    match prop {
        Property::Raw(_) => out.push(path.to_string()),
        Property::Struct(StructValue::Struct(inner)) => collect_raw_paths(inner, path, out),
        Property::Struct(StructValue::Raw(_)) => out.push(path.to_string()),
        Property::Map(entries) => {
            for entry in entries {
                collect_raw_paths_in_property(&entry.key, path, out);
                collect_raw_paths_in_property(&entry.value, path, out);
            }
        }
        Property::Set(ValueVec::Struct(structs)) | Property::Array(ValueVec::Struct(structs)) => {
            for s in structs {
                match s {
                    StructValue::Struct(inner) => collect_raw_paths(inner, path, out),
                    StructValue::Raw(_) => out.push(path.to_string()),
                    _ => {}
                }
            }
        }
        _ => {}
    }
}

fn assert_no_raw_degraded_properties(dir_name: &str) {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(dir_name)
        .join("Level.sav");
    let bytes = std::fs::read(&path).unwrap_or_else(|e| panic!("reading {path:?}: {e}"));
    let save = palworld_parser::gvas::decode(bytes)
        .unwrap_or_else(|e| panic!("decoding {path:?} should succeed: {e}"));
    let mut raw_paths = Vec::new();
    collect_raw_paths(&save.root.properties, "", &mut raw_paths);
    assert!(
        raw_paths.is_empty(),
        "{dir_name}/Level.sav has properties silently degraded to Property::Raw -- \
         this means a TYPE_HINTS entry in src/gvas.rs is missing or unreachable (a \
         dead path) for these locations: {raw_paths:#?}"
    );
}

#[test]
fn original_fixture_level_sav_parses_with_zero_raw_degraded_properties() {
    assert_no_raw_degraded_properties(WORLD_ID);
}

#[test]
fn live_fixture_level_sav_parses_with_zero_raw_degraded_properties() {
    assert_no_raw_degraded_properties(LIVE_WORLD_ID);
}

fn testdata_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(WORLD_ID)
}

fn read_fixture(name: &str) -> Vec<u8> {
    let path = testdata_dir().join(name);
    std::fs::read(&path).unwrap_or_else(|e| panic!("reading fixture {path:?}: {e}"))
}

/// Split a PlM1 container's 12-byte header from its declared payload.
/// Returns (uncompressed_len, compressed_len, payload_bytes).
fn split_container(raw: &[u8]) -> (u32, u32, &[u8]) {
    let ulen = u32::from_le_bytes(raw[0..4].try_into().unwrap());
    let clen = u32::from_le_bytes(raw[4..8].try_into().unwrap());
    (ulen, clen, &raw[12..12 + clen as usize])
}

fn build_container(ulen: u32, payload: &[u8], magic: &[u8; 4]) -> Vec<u8> {
    let mut out = Vec::with_capacity(12 + payload.len());
    out.extend_from_slice(&ulen.to_le_bytes());
    out.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    out.extend_from_slice(magic);
    out.extend_from_slice(payload);
    out
}

/// Build a tar archive in memory from (path, data) pairs, using the tar
/// crate's normal validated path-setting (safe for every case except the
/// deliberately-malicious-path test, which writes header bytes directly).
/// Paths are relative to the save directory, matching how the daemon tars
/// members (Level.sav, Players/x.sav — no world-id prefix).
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

/// Build a tar archive with one entry whose raw header `name` field is
/// written directly, bypassing the tar crate's own path validation (which
/// rejects absolute paths and normalizes `..` components) — simulating a
/// maliciously or accidentally crafted archive.
fn build_tar_with_raw_name(name_bytes: &[u8], data: &[u8]) -> Vec<u8> {
    let mut header = tar::Header::new_gnu();
    header.as_old_mut().name[..name_bytes.len()].copy_from_slice(name_bytes);
    header.set_size(data.len() as u64);
    header.set_mode(0o644);
    header.set_entry_type(tar::EntryType::Regular);
    header.set_cksum();

    let mut builder = tar::Builder::new(Vec::new());
    builder.append(&header, data).unwrap();
    builder.into_inner().unwrap()
}

fn happy_tar() -> Vec<u8> {
    let level = read_fixture("Level.sav");
    let level_meta = read_fixture("LevelMeta.sav");
    let local_data = read_fixture("LocalData.sav");
    let world_option = read_fixture("WorldOption.sav");
    let player = read_fixture("Players/00000000000000000000000000000001.sav");
    build_tar(&[
        ("Level.sav", &level),
        ("LevelMeta.sav", &level_meta),
        ("LocalData.sav", &local_data),
        ("WorldOption.sav", &world_option),
        ("Players/00000000000000000000000000000001.sav", &player),
    ])
}

fn live_testdata_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(LIVE_WORLD_ID)
}

fn read_live_fixture(name: &str) -> Vec<u8> {
    let path = live_testdata_dir().join(name);
    std::fs::read(&path).unwrap_or_else(|e| panic!("reading fixture {path:?}: {e}"))
}

/// The live-20260731 capture has no `LocalData.sav` member (unlike the
/// original fixture) -- that file is never read by this plugin (see
/// lib.rs), so its absence here is not a gap.
fn live_happy_tar() -> Vec<u8> {
    let level = read_live_fixture("Level.sav");
    let level_meta = read_live_fixture("LevelMeta.sav");
    let world_option = read_live_fixture("WorldOption.sav");
    let player = read_live_fixture("Players/00000000000000000000000000000001.sav");
    build_tar(&[
        ("Level.sav", &level),
        ("LevelMeta.sav", &level_meta),
        ("WorldOption.sav", &world_option),
        ("Players/00000000000000000000000000000001.sav", &player),
    ])
}

struct RunOutput {
    code: i32,
    lines: Vec<serde_json::Value>,
}

impl RunOutput {
    fn of_type(&self, ty: &str) -> Vec<&serde_json::Value> {
        self.lines.iter().filter(|l| l["type"] == ty).collect()
    }
}

/// Run the plugin the way the daemon does for a directory-unit save: the
/// world id (the save directory's base name) is passed as argv[1], the tar
/// contents on stdin.
fn run_plugin(stdin_data: &[u8]) -> RunOutput {
    run_plugin_with_args(stdin_data, &[WORLD_ID])
}

fn run_plugin_with_args(stdin_data: &[u8], args: &[&str]) -> RunOutput {
    let mut child = Command::new(env!("CARGO_BIN_EXE_palworld-parser"))
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn palworld-parser");
    child
        .stdin
        .take()
        .expect("child stdin")
        .write_all(stdin_data)
        .expect("write stdin");
    let output = child.wait_with_output().expect("wait for palworld-parser");
    let lines = String::from_utf8_lossy(&output.stdout)
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(|l| {
            serde_json::from_str(l).unwrap_or_else(|e| panic!("invalid ndjson line {l:?}: {e}"))
        })
        .collect();
    RunOutput {
        code: output.status.code().unwrap_or(-1),
        lines,
    }
}

// --- Happy path -------------------------------------------------------

#[test]
fn happy_path_emits_one_result_with_identity_and_overview() {
    let out = run_plugin(&happy_tar());
    assert_eq!(out.code, 0, "exit code, lines: {:?}", out.lines);

    let results = out.of_type("result");
    assert_eq!(results.len(), 1, "exactly one result line: {:?}", out.lines);
    let result = results[0];

    assert_eq!(result["identity"]["saveName"], "Palpagos Islands");
    assert_eq!(result["identity"]["gameId"], "palworld");
    assert_eq!(result["identity"]["extra"]["worldId"], WORLD_ID);

    let overview = &result["sections"]["overview"]["data"];
    assert_eq!(overview["worldName"], "Palpagos Islands");
    assert_eq!(overview["hostPlayerName"], "Atmus");
    assert_eq!(overview["hostPlayerLevel"], 9);
    assert_eq!(overview["inGameDay"], 4);
    assert_eq!(overview["levelMetaVersion"], 100);
    assert_eq!(overview["engineVersion"], "++UE5+Release-5.1");
    assert_eq!(overview["saveGameVersion"], 3);
    assert_eq!(overview["packageVersionUe4"], 522);
    assert_eq!(overview["packageVersionUe5"], 1008);
    assert_eq!(overview["guildName"], "00000000000000000000000000000001");
    assert_eq!(overview["baseCount"], 1);
    assert_eq!(overview["playerCount"], 1);

    // Exactly the four progress statuses and nothing else: every join
    // across players/pals/guild/bases/inventory resolves cleanly against
    // this real fixture, so no degrade warning should be appended.
    // LevelMeta.sav and player saves decode (and drop their bytes) before
    // Level.sav -- the largest, most memory-hungry member -- decodes last
    // (see P1+P2), so this order does not match the members' tar order.
    let statuses: Vec<&str> = out
        .of_type("status")
        .iter()
        .filter_map(|s| s["message"].as_str())
        .collect();
    assert_eq!(
        statuses,
        vec![
            "Reading save directory...",
            "Decoding LevelMeta.sav...",
            "Decoding player saves...",
            "Decoding Level.sav...",
        ],
        "expected exactly these four progress statuses and no degrade warnings: {statuses:?}"
    );
}

#[test]
fn happy_path_players_section_has_the_host_players_tech_and_paldeck_progress() {
    let out = run_plugin(&happy_tar());
    let result = &out.of_type("result")[0];
    let players = result["sections"]["players"]["data"]["players"]
        .as_array()
        .unwrap();
    assert_eq!(players.len(), 1);
    let atmus = &players[0];
    assert_eq!(atmus["playerUId"], "00000000-0000-0000-0000-000000000001");
    assert_eq!(atmus["name"], "Atmus");
    assert_eq!(atmus["level"], 9);
    assert_eq!(atmus["technologyPoint"], 14);
    assert_eq!(atmus["paldeckUnlockedCount"], 10);
    assert_eq!(atmus["tribeCaptureCount"], 10);
    let unlocked = atmus["unlockedTechnologies"].as_array().unwrap();
    assert_eq!(unlocked.len(), 27);
    assert!(unlocked.iter().any(|t| t == "Workbench"));
    assert!(unlocked.iter().any(|t| t == "PalBox"));
}

#[test]
fn happy_path_pals_party_and_storage_have_full_detail_from_real_pals() {
    let out = run_plugin(&happy_tar());
    let result = &out.of_type("result")[0];

    let party = result["sections"]["pals_party"]["data"]["pals"]
        .as_array()
        .unwrap();
    assert_eq!(party.len(), 3);
    let dream_demon = party
        .iter()
        .find(|p| p["speciesId"] == "DreamDemon")
        .expect("DreamDemon in the party");
    assert_eq!(dream_demon["level"], 7);
    assert_eq!(dream_demon["gender"], "EPalGenderType::Male");
    assert_eq!(
        dream_demon["ownerPlayerUId"],
        "00000000-0000-0000-0000-000000000001"
    );
    assert!(
        dream_demon["passiveSkills"]
            .as_array()
            .unwrap()
            .iter()
            .any(|s| s == "WorkSuitabilityAddRank_MonsterFarm_1")
    );

    let storage = result["sections"]["pals_storage"]["data"]["pals"]
        .as_array()
        .unwrap();
    assert_eq!(storage.len(), 28);
    assert!(
        storage
            .iter()
            .any(|p| p["speciesId"] == "ChickenPal" && p["level"] == 5),
        "expected ChickenPal at level 5 in pal storage: {storage:?}"
    );
}

#[test]
fn happy_path_guild_section_has_the_hosts_guild_and_full_roster() {
    let out = run_plugin(&happy_tar());
    let result = &out.of_type("result")[0];

    let guilds = result["sections"]["guild"]["data"]["guilds"]
        .as_array()
        .unwrap();
    assert_eq!(guilds.len(), 1);
    let guild = &guilds[0];
    assert_eq!(guild["memberCount"], 37);
    let members = guild["members"].as_array().unwrap();
    assert_eq!(members.len(), 37);
    assert!(
        members
            .iter()
            .any(|m| m["isPlayer"] == true && m["name"] == "Atmus" && m["level"] == 9)
    );
    assert!(
        members
            .iter()
            .any(|m| m["isPlayer"] == false && m["speciesId"] == "ChickenPal")
    );
}

#[test]
fn happy_path_bases_section_has_the_real_base_camp_id() {
    let out = run_plugin(&happy_tar());
    let result = &out.of_type("result")[0];

    let bases = result["sections"]["bases"]["data"]["bases"]
        .as_array()
        .unwrap();
    assert_eq!(bases.len(), 1);
    assert_eq!(bases[0]["baseId"], "4e18f078-4717-eb0a-043c-01b96f527fff");
}

#[test]
fn happy_path_inventory_section_labels_containers_by_role_with_real_items() {
    let out = run_plugin(&happy_tar());
    let result = &out.of_type("result")[0];

    let inventories = result["sections"]["inventory"]["data"]["inventories"]
        .as_array()
        .unwrap();
    assert_eq!(inventories.len(), 1);
    let inv = &inventories[0];
    assert_eq!(inv["playerUId"], "00000000-0000-0000-0000-000000000001");

    let containers = inv["containers"].as_array().unwrap();
    let weapon_load_out = containers
        .iter()
        .find(|c| c["role"] == "weaponLoadOut")
        .expect("a weaponLoadOut container");
    let items = weapon_load_out["items"].as_array().unwrap();
    assert!(
        items
            .iter()
            .any(|i| i["staticId"] == "Axe_Tier_00" && i["count"] == 1),
        "expected Axe_Tier_00 in weaponLoadOut: {items:?}"
    );

    let common = containers
        .iter()
        .find(|c| c["role"] == "common")
        .expect("a common container");
    assert_eq!(common["items"].as_array().unwrap().len(), 23);
}

/// Regression test for the production OOM incident: the deployed plugin
/// aborted under wazero's 1 GiB allocation cap parsing this exact live save
/// because a dead TYPE_HINTS path let a nested map's key/value types go
/// unresolved, misaligning the byte stream into a garbage FString length.
/// This drives the full tar -> stdout pipeline (not just gvas::decode) to
/// prove the fixed TYPE_HINTS table produces a complete result -- all seven
/// sections -- from the owner's real, currently-live world.
#[test]
fn live_fixture_happy_path_emits_all_seven_sections_with_pinned_values() {
    let out = run_plugin_with_args(&live_happy_tar(), &[LIVE_WORLD_ID]);
    assert_eq!(out.code, 0, "lines: {:?}", out.lines);

    let results = out.of_type("result");
    assert_eq!(results.len(), 1, "exactly one result line: {:?}", out.lines);
    let result = results[0];

    assert_eq!(result["identity"]["saveName"], "Palpagos Islands");
    assert_eq!(result["identity"]["gameId"], "palworld");
    assert_eq!(result["identity"]["extra"]["worldId"], LIVE_WORLD_ID);

    let sections = &result["sections"];
    for name in [
        "overview",
        "players",
        "pals_party",
        "pals_storage",
        "guild",
        "bases",
        "inventory",
    ] {
        assert!(
            sections.get(name).is_some(),
            "expected section {name:?} present in the live-fixture result: {:?}",
            out.lines
        );
    }

    // Values pinned from this exact fixture's own parse (see testdata/README.md).
    let overview = &sections["overview"]["data"];
    assert_eq!(overview["hostPlayerLevel"], 9);
    assert_eq!(
        overview["levelMetaTimestampTicks"], 639211170474840000_u64,
        "this save's own LevelMeta timestamp, distinguishing it from the original fixture"
    );
    assert_eq!(
        sections["pals_storage"]["data"]["pals"]
            .as_array()
            .unwrap()
            .len(),
        28,
        "storage pal count for this fixture"
    );
}

#[test]
fn missing_world_id_arg_falls_back_to_world() {
    // The daemon always passes argv[1], but the fallback ("world") must
    // still hold if it's ever absent rather than panicking on an
    // out-of-bounds args() index.
    let level = read_fixture("Level.sav");
    let tar = build_tar(&[("Level.sav", &level)]);

    let out = run_plugin_with_args(&tar, &[]);
    assert_eq!(out.code, 0, "lines: {:?}", out.lines);

    let results = out.of_type("result");
    assert_eq!(results.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(results[0]["identity"]["extra"]["worldId"], "world");
    assert_eq!(results[0]["identity"]["saveName"], "world");

    // No Players/*.sav members at all -- the players/inventory sections
    // degrade to empty arrays, and pals_party/pals_storage are empty too:
    // both are built by iterating each player's own
    // OtomoCharacterContainerId/PalStorageContainerId, so pals sections DO
    // depend on players existing -- zero players means zero container ids
    // to look up in the first place.
    let sections = &results[0]["sections"];
    assert_eq!(
        sections["players"]["data"]["players"]
            .as_array()
            .unwrap()
            .len(),
        0
    );
    assert_eq!(
        sections["inventory"]["data"]["inventories"]
            .as_array()
            .unwrap()
            .len(),
        0
    );
    assert_eq!(
        sections["pals_party"]["data"]["pals"]
            .as_array()
            .unwrap()
            .len(),
        0
    );
    assert_eq!(
        sections["pals_storage"]["data"]["pals"]
            .as_array()
            .unwrap()
            .len(),
        0
    );
    assert_eq!(sections["overview"]["data"]["playerCount"], 0);
}

// --- Container-level adversarial cases ---------------------------------

#[test]
fn container_bad_magic_is_corrupt_file() {
    let mut level = read_fixture("Level.sav");
    level[8..12].copy_from_slice(b"XXXX");
    let tar = build_tar(&[("Level.sav", &level)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn container_truncated_header_is_corrupt_file() {
    let level = &read_fixture("Level.sav")[..5];
    let tar = build_tar(&[("Level.sav", level)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn container_clen_mismatch_vs_payload_length_is_corrupt_file() {
    let level = read_fixture("Level.sav");
    // Header still claims the original (much larger) compressed_len, but
    // only 50 bytes of payload actually follow.
    let truncated_member = level[..12 + 50].to_vec();
    let tar = build_tar(&[("Level.sav", &truncated_member)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn container_oversized_uncompressed_len_is_corrupt_file_before_allocation() {
    // 300 MiB, over the 256 MiB cap. clen and payload agree (4 bytes) so
    // only the uncompressed-size cap is exercised, not a truncation error.
    // Not "resource_limit": the daemon's proto ParseErrorType enum has no
    // such variant (see main.rs's tar_error_type/gvas_error_type comments).
    let oversized = build_container(300 * 1024 * 1024, &[0u8; 4], b"PlM1");
    let tar = build_tar(&[("Level.sav", &oversized)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
    // Names both the actual size and this plugin version's cap, and makes
    // clear this is a size limit, not corruption (see P1+P2).
    let message = errors[0]["message"].as_str().unwrap();
    assert!(
        message.contains("300.0 MiB"),
        "message should name the actual size: {message}"
    );
    assert!(
        message.contains("256 MiB"),
        "message should name this plugin version's cap: {message}"
    );
    assert!(
        message.contains("not corruption"),
        "message should make clear this is a size limit, not corruption: {message}"
    );
}

#[test]
fn container_unknown_variant_magic_is_unsupported_version_naming_member() {
    let mut level = read_fixture("Level.sav");
    level[8..12].copy_from_slice(b"PlM2");
    let tar = build_tar(&[("Level.sav", &level)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "unsupported_version");
    let message = errors[0]["message"].as_str().unwrap();
    assert!(
        message.contains("Level.sav"),
        "message should name the member: {message}"
    );
}

// --- Compressed-body adversarial cases ---------------------------------

#[test]
fn truncated_kraken_stream_is_corrupt_file_no_panic() {
    let level = read_fixture("Level.sav");
    let (ulen, _clen, payload) = split_container(&level);
    let half_payload = &payload[..payload.len() / 2];
    let truncated_member = build_container(ulen, half_payload, b"PlM1");
    let tar = build_tar(&[("Level.sav", &truncated_member)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn corrupted_kraken_stream_mid_stream_is_corrupt_file_no_panic() {
    let mut level = read_fixture("Level.sav");
    let (_ulen, clen, _payload) = split_container(&level);
    // Flip bytes near the end of the compressed stream. Validated (outside
    // this crate, against this exact fixture) to make the oozextract
    // decoder panic rather than return an error, which is exactly why this
    // case exists: the panic must be caught and turned into a normal
    // structured error, never crash the process. The panic hook installed
    // in main() fires first (see PANIC_REPORTED); this test's exactly-one-
    // error-line assertion is what proves that hook doesn't double-report.
    let start = 12 + clen as usize - 30;
    for b in level.iter_mut().skip(start).take(20) {
        *b ^= 0xFF;
    }
    let tar = build_tar(&[("Level.sav", &level)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(
        errors.len(),
        1,
        "exactly one error line, no panic-corrupted output: {:?}",
        out.lines
    );
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn truncated_stored_kraken_block_is_corrupt_file() {
    // A "stored" (uncompressed) Kraken block: 2-byte block header
    // [0x4C, 0x06] (uncompressed=true, decoder_type=Kraken) followed by raw
    // bytes copied straight into the output. Declaring expected_len=8 but
    // supplying only 4 raw bytes exercises a different oozextract code path
    // (the stored/uncompressed branch) than the genuinely-compressed
    // truncation case above — this one errors via oozextract's own
    // `Slice::read_to` (which hard-errors on an out-of-range read rather
    // than ever partially succeeding), not the new written-length check in
    // decompress.rs.
    let payload = [0x4Cu8, 0x06, 0xAA, 0xBB, 0xCC, 0xDD];
    let member = build_container(8, &payload, b"PlM1");
    let tar = build_tar(&[("Level.sav", &member)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn valid_kraken_stream_decoding_to_non_gvas_bytes_is_parse_error_with_byte_offset() {
    // Same "stored" block trick as above, but sized so the decompression
    // fully succeeds (4 declared, 4 supplied) — proving this case reaches
    // uesave's GVAS parser rather than failing in decompression. The 4
    // resulting bytes are consumed entirely by Header::read's magic field,
    // so the very next read (save_game_version) hits EOF and fails cleanly
    // inside uesave, never oozextract — this is a parse_error, not
    // corrupt_file.
    let payload = [0x4Cu8, 0x06, 0, 0, 0, 0];
    let member = build_container(4, &payload, b"PlM1");
    let tar = build_tar(&[("Level.sav", &member)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "parse_error");
    assert!(
        errors[0]["byteOffset"].is_number(),
        "expected a byteOffset on a parse_error: {:?}",
        errors[0]
    );
}

// --- Tar/container framing ----------------------------------------------

#[test]
fn raw_plm1_on_stdin_without_tar_is_unsupported_version() {
    let level = read_fixture("Level.sav");

    let out = run_plugin(&level);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "unsupported_version");
    let message = errors[0]["message"].as_str().unwrap();
    assert!(
        message.contains("tar"),
        "message should mention the missing tar wrapper: {message}"
    );
}

#[test]
fn garbage_that_is_neither_tar_nor_plm1_is_corrupt_file() {
    // Long enough to pass the ustar-magic length check but containing
    // neither the ustar magic nor the PlM1 magic anywhere relevant.
    let garbage = vec![0x42u8; 300];

    let out = run_plugin(&garbage);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

// These two name the malicious member `Level.sav` with *real* Level.sav
// bytes as its content — if the unsafe-path check were ever bypassed, the
// member would be picked up as a legitimate Level.sav and the pipeline
// would succeed (result, exit 0). That makes the check's effect (reject,
// exit 1, corrupt_file) observable, instead of coincidentally matching an
// unrelated "Level.sav is missing" error for a filename the pipeline was
// never going to recognize anyway.

#[test]
fn unsafe_absolute_member_path_is_rejected() {
    let level = read_fixture("Level.sav");
    let tar = build_tar_with_raw_name(b"/Level.sav", &level);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn unsafe_dotdot_member_path_is_rejected() {
    let level = read_fixture("Level.sav");
    let tar = build_tar_with_raw_name(b"../../etc/Level.sav", &level);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
}

#[test]
fn tar_member_exceeding_size_cap_is_corrupt_file() {
    // One byte past MAX_MEMBER_SIZE; real content (not just a header claim)
    // so the archive itself is well-formed and only the cap is exercised.
    // Not "resource_limit": see main.rs's tar_error_type comment.
    let oversized_content = vec![0u8; 64 * 1024 * 1024 + 1];
    let tar = build_tar(&[("Level.sav", &oversized_content)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    assert_eq!(errors[0]["errorType"], "corrupt_file");
    // Same clarity as the container-level uncompressed-size cap (see
    // P1+P2): the message should make clear this is a size limit, not
    // corruption.
    let message = errors[0]["message"].as_str().unwrap();
    assert!(
        message.contains("not corruption"),
        "message should make clear this is a size limit, not corruption: {message}"
    );
}

// --- Missing members ------------------------------------------------------

#[test]
fn missing_level_meta_degrades_to_world_id_identity_with_status_warning() {
    let level = read_fixture("Level.sav");
    let tar = build_tar(&[("Level.sav", &level)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 0, "lines: {:?}", out.lines);

    let results = out.of_type("result");
    assert_eq!(results.len(), 1, "lines: {:?}", out.lines);
    let result = results[0];
    assert_eq!(result["identity"]["saveName"], WORLD_ID);

    let overview = &result["sections"]["overview"]["data"];
    assert!(overview["worldName"].is_null());

    let statuses = out.of_type("status");
    assert!(
        statuses
            .iter()
            .any(|s| s["message"].as_str().unwrap_or("").contains("LevelMeta")),
        "expected a status warning mentioning LevelMeta: {:?}",
        out.lines
    );
}

#[test]
fn missing_level_sav_is_a_structured_error() {
    let level_meta = read_fixture("LevelMeta.sav");
    let tar = build_tar(&[("LevelMeta.sav", &level_meta)]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 1, "lines: {:?}", out.lines);
    let errors = out.of_type("error");
    assert_eq!(errors.len(), 1, "lines: {:?}", out.lines);
    let message = errors[0]["message"].as_str().unwrap();
    assert!(
        message.contains("Level.sav"),
        "message should name Level.sav: {message}"
    );
}

#[test]
fn corrupt_player_save_degrades_players_section_with_status_warning_result_still_emitted() {
    let level = read_fixture("Level.sav");
    let level_meta = read_fixture("LevelMeta.sav");
    // A PlM1 container whose Kraken payload is truncated -- fails inside
    // gvas::decode() the same way truncated_kraken_stream_is_corrupt_file_no_panic
    // exercises for Level.sav, but here for a Players/*.sav member instead,
    // proving one undecodable player save degrades that section rather than
    // aborting the whole run.
    let (ulen, _clen, payload) = split_container(&level);
    let half_payload = &payload[..payload.len() / 2];
    let bogus_player = build_container(ulen, half_payload, b"PlM1");
    let tar = build_tar(&[
        ("Level.sav", &level),
        ("LevelMeta.sav", &level_meta),
        (
            "Players/00000000000000000000000000000002.sav",
            &bogus_player,
        ),
    ]);

    let out = run_plugin(&tar);
    assert_eq!(out.code, 0, "lines: {:?}", out.lines);

    let results = out.of_type("result");
    assert_eq!(results.len(), 1, "lines: {:?}", out.lines);
    let sections = &results[0]["sections"];
    assert_eq!(
        sections["players"]["data"]["players"]
            .as_array()
            .unwrap()
            .len(),
        0
    );
    assert_eq!(sections["overview"]["data"]["playerCount"], 0);

    let statuses = out.of_type("status");
    assert!(
        statuses.iter().any(|s| {
            s["message"]
                .as_str()
                .unwrap_or("")
                .contains("00000000000000000000000000000002.sav")
        }),
        "expected a status warning naming the undecodable player save: {:?}",
        out.lines
    );
}
