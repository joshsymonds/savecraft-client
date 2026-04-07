use std::process::Command;

/// Feed a real Stellaris save to the parser binary and verify ndjson output.
#[test]
fn parse_mid_game_save() {
    let save_path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../testdata/autosave_2327.07.01.sav"
    );
    let save_data = std::fs::read(save_path).expect("failed to read test save");

    // Build and run the parser binary natively (not WASM — for test speed)
    let output = Command::new(env!("CARGO_BIN_EXE_stellaris-parser"))
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .and_then(|mut child| {
            use std::io::Write;
            child
                .stdin
                .take()
                .unwrap()
                .write_all(&save_data)
                .unwrap();
            child.wait_with_output()
        })
        .expect("failed to run parser");

    assert!(
        output.status.success(),
        "parser exited with error: {}",
        String::from_utf8_lossy(&output.stderr)
    );

    let stdout = String::from_utf8(output.stdout).expect("non-UTF8 output");
    let lines: Vec<&str> = stdout.lines().collect();

    // Should have at least 2 lines: status messages + result
    assert!(
        lines.len() >= 2,
        "expected at least 2 ndjson lines, got {}:\n{}",
        lines.len(),
        stdout
    );

    // Last line should be the result
    let result_line = lines.last().unwrap();
    let result: serde_json::Value =
        serde_json::from_str(result_line).expect("result line is not valid JSON");

    assert_eq!(result["type"], "result");
    assert_eq!(result["identity"]["gameId"], "stellaris");
    assert_eq!(
        result["identity"]["extra"]["date"], "2327.07.01",
        "date mismatch"
    );

    // Verify overview section exists
    let sections = &result["sections"];
    assert!(
        sections["overview"].is_object(),
        "overview section missing"
    );

    let overview = &sections["overview"]["data"];
    assert_eq!(overview["empire_name"], "Termanid Host 7");
    assert_eq!(overview["date"], "2327.07.01");
    assert!(
        overview["game_version"]
            .as_str()
            .unwrap()
            .contains("4.2"),
        "expected game version containing 4.2"
    );
    assert!(
        overview["required_dlcs"].as_array().unwrap().len() > 10,
        "expected many DLCs"
    );
    assert_eq!(overview["meta_fleets"], 653);
    assert_eq!(overview["meta_planets"], 12);

    // Gamestate-derived fields
    assert_eq!(
        overview["ethics"],
        serde_json::json!(["ethic_gestalt_consciousness"]),
        "ethics mismatch"
    );
    assert_eq!(overview["authority"], "auth_hive_mind");
    assert_eq!(overview["government_type"], "gov_devouring_swarm");
    assert_eq!(overview["origin"], "origin_subterranean");
    assert!(
        overview["civics"].as_array().unwrap().len() >= 2,
        "expected at least 2 civics"
    );

    // Power and rank
    let military_power = overview["military_power"].as_f64().unwrap();
    assert!(
        military_power > 100000.0,
        "expected military power > 100k, got {military_power}"
    );
    assert_eq!(overview["victory_rank"], 8);

    // Resource stockpiles
    let resources = &overview["resources"];
    assert!(
        resources["energy"].as_f64().unwrap() > 30000.0,
        "expected energy > 30k"
    );
    assert!(
        resources["minerals"].as_f64().unwrap() > 30000.0,
        "expected minerals > 30k"
    );
    assert!(
        resources["alloys"].as_f64().unwrap() > 20000.0,
        "expected alloys > 20k"
    );
    assert!(
        resources["food"].as_f64().unwrap() > 40000.0,
        "expected food > 40k"
    );

    // --- Economy section ---
    assert!(
        sections["economy"].is_object(),
        "economy section missing"
    );
    let economy = &sections["economy"]["data"];

    // Income should have multiple resource types with positive values
    let income = &economy["income"];
    assert!(
        income["energy"].as_f64().unwrap() > 0.0,
        "expected positive energy income"
    );
    assert!(
        income["minerals"].as_f64().unwrap() > 0.0,
        "expected positive minerals income"
    );

    // Expenses should have negative or positive values
    let expenses = &economy["expenses"];
    assert!(
        expenses["energy"].as_f64().unwrap() > 0.0,
        "expected positive energy expenses"
    );

    // Net balance should exist
    let net = &economy["net"];
    assert!(
        net.is_object(),
        "net balance should be an object"
    );

    // --- Technology section ---
    assert!(
        sections["technology"].is_object(),
        "technology section missing"
    );
    let technology = &sections["technology"]["data"];

    // Researched techs
    let researched = &technology["researched"];
    assert!(
        researched.as_array().unwrap().len() > 50,
        "expected many researched techs"
    );

    // In-progress research
    let in_progress = &technology["in_progress"];
    assert_eq!(
        in_progress["physics"]["tech"], "tech_zero_point_power",
        "expected physics research"
    );
    assert_eq!(
        in_progress["society"]["tech"], "tech_hive_cluster",
        "expected society research"
    );
    assert_eq!(
        in_progress["engineering"]["tech"], "tech_autocannons_3",
        "expected engineering research"
    );
    // Progress should be positive
    assert!(
        in_progress["physics"]["progress"].as_f64().unwrap() > 0.0,
        "expected positive physics progress"
    );

    // Alternatives (available techs)
    let alternatives = &technology["alternatives"];
    assert!(
        alternatives["physics"].as_array().unwrap().len() > 0,
        "expected physics alternatives"
    );
    assert!(
        alternatives["society"].as_array().unwrap().len() > 0,
        "expected society alternatives"
    );
    assert!(
        alternatives["engineering"].as_array().unwrap().len() > 0,
        "expected engineering alternatives"
    );

    // --- Military section ---
    assert!(
        sections["military"].is_object(),
        "military section missing"
    );
    let military = &sections["military"]["data"];
    assert!(
        military["military_power"].as_f64().unwrap() > 100000.0,
        "expected military power > 100k"
    );
    assert!(
        military["fleet_size"].as_i64().unwrap() > 600,
        "expected fleet_size > 600"
    );

    // --- Wars section ---
    assert!(
        sections["wars"].is_object(),
        "wars section missing"
    );
    let wars = &sections["wars"]["data"];
    let active_wars = wars["active_wars"].as_array().unwrap();
    // Wars section should be present and be an array (may be empty if player isn't at war)
    assert!(
        wars["player_at_war"].is_boolean(),
        "player_at_war should be a boolean"
    );
    // If there are wars, they should have proper structure
    if !active_wars.is_empty() {
        let first_war = &active_wars[0];
        assert!(
            first_war["attackers"].is_array(),
            "war should have attackers"
        );
        assert!(
            first_war["defenders"].is_array(),
            "war should have defenders"
        );
    }

    // --- Diplomacy section ---
    assert!(
        sections["diplomacy"].is_object(),
        "diplomacy section missing"
    );
    let diplomacy = &sections["diplomacy"]["data"];
    let relations = diplomacy["relations"].as_array().unwrap();
    assert!(
        !relations.is_empty(),
        "expected at least one diplomatic relation"
    );
    // Relations should have country ID and opinion value
    let first_rel = &relations[0];
    assert!(
        first_rel["country"].is_number(),
        "relation should have country ID"
    );
    assert!(
        first_rel["opinion"].is_number(),
        "relation should have opinion value"
    );

    // --- Progression section ---
    assert!(sections["progression"].is_object(), "progression section missing");
    let progression = &sections["progression"]["data"];
    let traditions = progression["traditions"].as_array().unwrap();
    assert!(traditions.len() > 30, "expected many traditions");
    let ascension_perks = progression["ascension_perks"].as_array().unwrap();
    assert!(ascension_perks.len() >= 5, "expected at least 5 ascension perks");
    let edicts = progression["edicts"].as_array().unwrap();
    assert!(!edicts.is_empty(), "expected at least one active edict");

    // --- Leaders section ---
    assert!(sections["leaders"].is_object(), "leaders section missing");
    let leaders = &sections["leaders"]["data"];
    let leader_list = leaders["leaders"].as_array().unwrap();
    assert!(!leader_list.is_empty(), "expected at least one leader");
    // Each leader should have class and level
    let first_leader = &leader_list[0];
    assert!(first_leader["class"].is_string(), "leader should have class");
    assert!(first_leader["level"].is_number(), "leader should have level");

    // --- Species section ---
    assert!(sections["species"].is_object(), "species section missing");
    let species = &sections["species"]["data"];
    let species_list = species["species"].as_array().unwrap();
    assert!(!species_list.is_empty(), "expected at least one species");

    // --- Factions section ---
    assert!(sections["factions"].is_object(), "factions section missing");
    // Gestalt consciousness may have no factions — just verify the section exists

    // --- Exploration section ---
    assert!(sections["exploration"].is_object(), "exploration section missing");

    // --- Geography section ---
    assert!(sections["geography"].is_object(), "geography section missing");
    let geography = &sections["geography"]["data"];
    let owned_planets = geography["owned_planet_ids"].as_array().unwrap();
    assert_eq!(owned_planets.len(), 12, "expected 12 owned planets");

    // --- Planets section ---
    assert!(sections["planets"].is_object(), "planets section missing");
    let planets = &sections["planets"]["data"];
    let colony_list = planets["colonies"].as_array().unwrap();
    assert!(colony_list.len() >= 10, "expected at least 10 colonies");
    // First colony should have key data
    let first_colony = &colony_list[0];
    assert!(first_colony["planet_id"].is_number(), "colony should have planet_id");
    assert!(first_colony["planet_class"].is_string(), "colony should have planet_class");
    assert!(first_colony["num_pops"].is_number(), "colony should have num_pops");
}
