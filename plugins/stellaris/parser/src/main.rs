mod sections;

use clausewitz_core::{envelope, ndjson};
use jomini::TextTape;
use sections::gamestate::{find_entry, find_field, read_i64};
use std::io::Read;

fn main() {
    let mut input = Vec::new();
    if let Err(e) = std::io::stdin().read_to_end(&mut input) {
        ndjson::emit_error("read_error", &format!("failed to read stdin: {e}"));
        std::process::exit(1);
    }

    ndjson::emit_status("Extracting save file...");

    let save = match envelope::extract(&input) {
        Ok(s) => s,
        Err(e) => {
            ndjson::emit_error("envelope_error", &e);
            std::process::exit(1);
        }
    };

    ndjson::emit_status("Parsing meta...");

    let meta = match sections::meta::parse(&save.meta) {
        Ok(m) => m,
        Err(e) => {
            ndjson::emit_error("parse_error", &format!("failed to parse meta: {e}"));
            std::process::exit(1);
        }
    };

    ndjson::emit_status("Parsing gamestate...");

    let tape = match TextTape::from_slice(&save.gamestate) {
        Ok(t) => t,
        Err(e) => {
            ndjson::emit_error("parse_error", &format!("failed to parse gamestate: {e}"));
            std::process::exit(1);
        }
    };
    let reader = tape.windows1252_reader();

    // Find the player's country ID
    let player_country_id = find_player_country_id(&reader);
    let id_str = player_country_id.to_string();

    // Find the player's country object
    let country_val = match find_field(&reader, "country") {
        Some(v) => v,
        None => {
            ndjson::emit_error("parse_error", "country block not found in gamestate");
            std::process::exit(1);
        }
    };
    let country_obj = match country_val.read_object() {
        Ok(o) => o,
        Err(e) => {
            ndjson::emit_error("parse_error", &format!("country block not an object: {e}"));
            std::process::exit(1);
        }
    };
    let player_val = match find_entry(&country_obj, &id_str) {
        Some(v) => v,
        None => {
            ndjson::emit_error(
                "parse_error",
                &format!("player country {id_str} not found"),
            );
            std::process::exit(1);
        }
    };
    let player_country = match player_val.read_object() {
        Ok(o) => o,
        Err(e) => {
            ndjson::emit_error(
                "parse_error",
                &format!("player country not an object: {e}"),
            );
            std::process::exit(1);
        }
    };

    ndjson::emit_status("Extracting sections...");

    // Extract sections from the player's country
    let overview = sections::overview::extract(&meta, &player_country);
    let economy = sections::economy::extract(&player_country);
    let technology = sections::technology::extract(&player_country);
    let military = sections::military::extract(&player_country);
    let diplomacy = sections::diplomacy::extract(&player_country);
    let progression = sections::progression::extract(&player_country);
    let geography = sections::geography::extract(&player_country);

    // Extract sections that need the full gamestate
    let wars = sections::wars::extract(&reader, player_country_id);
    let leaders = sections::leaders::extract(&reader, player_country_id);
    let species = sections::species::extract(&reader, &player_country);
    let factions = sections::factions::extract(&reader, player_country_id);
    let exploration = sections::exploration::extract(&reader, player_country_id);
    let planets = sections::planets::extract(&reader, &player_country);

    let mut section_map = std::collections::HashMap::new();
    section_map.insert(
        "overview".to_string(),
        ndjson::Section {
            description: "Empire overview: identity, rank, resources, game state".to_string(),
            data: serde_json::to_value(&overview).unwrap_or_default(),
        },
    );
    section_map.insert(
        "economy".to_string(),
        ndjson::Section {
            description: "Economy: income, expenses, and net balance by resource".to_string(),
            data: serde_json::to_value(&economy).unwrap_or_default(),
        },
    );
    section_map.insert(
        "technology".to_string(),
        ndjson::Section {
            description: "Technology: researched, in-progress, and available techs".to_string(),
            data: serde_json::to_value(&technology).unwrap_or_default(),
        },
    );
    section_map.insert(
        "military".to_string(),
        ndjson::Section {
            description: "Military: fleet power, fleet size, naval capacity".to_string(),
            data: serde_json::to_value(&military).unwrap_or_default(),
        },
    );
    section_map.insert(
        "wars".to_string(),
        ndjson::Section {
            description: "Wars: active wars involving the player".to_string(),
            data: serde_json::to_value(&wars).unwrap_or_default(),
        },
    );
    section_map.insert(
        "diplomacy".to_string(),
        ndjson::Section {
            description: "Diplomacy: relations, casus belli".to_string(),
            data: serde_json::to_value(&diplomacy).unwrap_or_default(),
        },
    );
    section_map.insert(
        "progression".to_string(),
        ndjson::Section {
            description: "Progression: traditions, ascension perks, edicts".to_string(),
            data: serde_json::to_value(&progression).unwrap_or_default(),
        },
    );
    section_map.insert(
        "leaders".to_string(),
        ndjson::Section {
            description: "Leaders: ruler, scientists, admirals, generals".to_string(),
            data: serde_json::to_value(&leaders).unwrap_or_default(),
        },
    );
    section_map.insert(
        "species".to_string(),
        ndjson::Section {
            description: "Species: traits, pop counts, founder species".to_string(),
            data: serde_json::to_value(&species).unwrap_or_default(),
        },
    );
    section_map.insert(
        "factions".to_string(),
        ndjson::Section {
            description: "Factions: faction happiness, support, demands".to_string(),
            data: serde_json::to_value(&factions).unwrap_or_default(),
        },
    );
    section_map.insert(
        "exploration".to_string(),
        ndjson::Section {
            description: "Exploration: archaeological sites, discoveries".to_string(),
            data: serde_json::to_value(&exploration).unwrap_or_default(),
        },
    );
    section_map.insert(
        "geography".to_string(),
        ndjson::Section {
            description: "Geography: owned/controlled planets, sectors".to_string(),
            data: serde_json::to_value(&geography).unwrap_or_default(),
        },
    );
    section_map.insert(
        "planets".to_string(),
        ndjson::Section {
            description: "Planets: colonies with pops, stability, housing, districts".to_string(),
            data: serde_json::to_value(&planets).unwrap_or_default(),
        },
    );

    let save_name = meta.name.as_deref().unwrap_or("unknown").to_string();
    let tag = meta.player.as_deref().unwrap_or("???");
    let date = meta.date.as_deref().unwrap_or("unknown");
    let summary = format!(
        "{}, {} ({})",
        meta.name.as_deref().unwrap_or("Unknown Empire"),
        tag,
        date
    );

    let identity = ndjson::Identity {
        save_name,
        game_id: "stellaris".to_string(),
        extra: Some(serde_json::json!({
            "tag": tag,
            "date": date,
        })),
    };

    ndjson::emit_result(identity, summary, section_map);
}

/// Find the player's country ID from the `player` block.
fn find_player_country_id(
    reader: &jomini::text::ObjectReader<'_, '_, jomini::Windows1252Encoding>,
) -> i64 {
    if let Some(player_val) = find_field(reader, "player") {
        if let Ok(player_arr) = player_val.read_array() {
            for item in player_arr.values() {
                if let Ok(obj) = item.read_object() {
                    if let Some(id) = read_i64(&obj, "country") {
                        return id;
                    }
                }
            }
        }
    }
    0
}
