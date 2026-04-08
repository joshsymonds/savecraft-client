use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_i64, read_string};

/// A crisis country detected in the galaxy.
#[derive(Debug, Serialize)]
pub struct CrisisCountry {
    pub country_id: String,
    pub country_type: String,
    pub military_power: Option<f64>,
}

/// A fallen or awakened fallen empire.
#[derive(Debug, Serialize)]
pub struct FallenEmpire {
    pub country_id: String,
    pub country_type: String,
    pub awakened: bool,
    pub military_power: Option<f64>,
}

/// A non-player country that has a casus belli targeting the player.
#[derive(Debug, Serialize)]
pub struct HostileNeighbor {
    pub country_id: String,
    pub military_power: Option<f64>,
    pub cb_types: Vec<String>,
}

/// The threats section: crisis entities, fallen/awakened empires, hostile neighbors.
#[derive(Debug, Serialize)]
pub struct Threats {
    pub crisis_active: bool,
    pub crisis_type: Option<String>,
    pub crisis_countries: Vec<CrisisCountry>,
    pub fallen_empires: Vec<FallenEmpire>,
    /// Countries that have a casus belli against the player, with their military power.
    pub hostile_neighbors: Vec<HostileNeighbor>,
}

/// Known crisis country types in Stellaris saves.
fn classify_crisis(country_type: &str) -> Option<&'static str> {
    match country_type {
        "swarm" | "swarm_2" => Some("prethoryn_scourge"),
        "ai_empire" => Some("contingency"),
        "extradimensional" | "extradimensional_2" | "extradimensional_3" => Some("unbidden"),
        // Become the Crisis perk (player-driven, but flagged if AI does it)
        "awakened_marauders" => None, // Great Khan — not an endgame crisis
        _ => None,
    }
}

/// Check if a country type is a fallen or awakened empire.
fn is_fallen_empire(country_type: &str) -> bool {
    country_type == "fallen_empire" || country_type == "awakened_fallen_empire"
}

/// Country types that are not real empires (primitives, enclaves, etc).
fn is_special_country(country_type: &str) -> bool {
    matches!(
        country_type,
        "primitive"
            | "enclave"
            | "nomad"
            | "pirate"
            | "rebel"
            | "gray_goo"
            | "shroud"
            | "tiyanki"
            | "amoeba"
            | "crystal"
            | "drone"
            | "cloud"
            | "portal_holder"
            | "salvager_enclave"
            | "mercenary_enclave"
            | "shroudwalker_enclave"
    )
}

/// Extract casus belli entries from a country's standard_diplomacy_module.
fn extract_casus_belli(
    country_obj: &ObjectReader<'_, '_, Windows1252Encoding>,
    target_country_id: i64,
) -> Vec<String> {
    let mut cb_types = Vec::new();

    let modules_val = match find_field(country_obj, "modules") {
        Some(v) => v,
        None => return cb_types,
    };
    let modules_obj = match modules_val.read_object() {
        Ok(o) => o,
        Err(_) => return cb_types,
    };
    let diplo_val = match find_field(&modules_obj, "standard_diplomacy_module") {
        Some(v) => v,
        None => return cb_types,
    };
    let diplo_obj = match diplo_val.read_object() {
        Ok(o) => o,
        Err(_) => return cb_types,
    };
    let cb_val = match find_field(&diplo_obj, "casus_belli") {
        Some(v) => v,
        None => return cb_types,
    };
    let cb_arr = match cb_val.read_array() {
        Ok(a) => a,
        Err(_) => return cb_types,
    };

    for item in cb_arr.values() {
        if let Ok(cb_obj) = item.read_object() {
            let target = read_i64(&cb_obj, "country");
            if target == Some(target_country_id) {
                if let Some(cb_type) = read_string(&cb_obj, "type") {
                    cb_types.push(cb_type);
                }
            }
        }
    }

    cb_types
}

/// Extract the threats section by scanning all non-player countries.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    player_country_id: i64,
) -> Threats {
    let mut threats = Threats {
        crisis_active: false,
        crisis_type: None,
        crisis_countries: Vec::new(),
        fallen_empires: Vec::new(),
        hostile_neighbors: Vec::new(),
    };

    let country_val = match find_field(gamestate, "country") {
        Some(v) => v,
        None => return threats,
    };
    let country_obj = match country_val.read_object() {
        Ok(o) => o,
        Err(_) => return threats,
    };

    for (key, _op, entry_val) in country_obj.fields() {
        // Skip `none` entries (dead countries)
        let entry_obj = match entry_val.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        // Skip the player's own country
        let id_str = key.read_str();
        if id_str.parse::<i64>().ok() == Some(player_country_id) {
            continue;
        }

        let country_type = match read_string(&entry_obj, "type") {
            Some(t) => t,
            None => continue,
        };

        // Check for crisis
        if let Some(crisis_name) = classify_crisis(&country_type) {
            let military_power = read_f64(&entry_obj, "military_power");
            threats.crisis_countries.push(CrisisCountry {
                country_id: id_str.into_owned(),
                country_type: country_type.clone(),
                military_power,
            });
            threats.crisis_active = true;
            if threats.crisis_type.is_none() {
                threats.crisis_type = Some(crisis_name.to_string());
            }
            continue;
        }

        // Check for fallen/awakened empires
        if is_fallen_empire(&country_type) {
            let military_power = read_f64(&entry_obj, "military_power");
            let awakened = country_type == "awakened_fallen_empire";
            threats.fallen_empires.push(FallenEmpire {
                country_id: id_str.into_owned(),
                country_type,
                awakened,
                military_power,
            });
            continue;
        }

        // Skip non-empire special countries (primitives, enclaves, etc.)
        if is_special_country(&country_type) {
            continue;
        }

        // Check if this country has casus belli against the player
        let cb_types = extract_casus_belli(&entry_obj, player_country_id);
        if !cb_types.is_empty() {
            let military_power = read_f64(&entry_obj, "military_power");
            threats.hostile_neighbors.push(HostileNeighbor {
                country_id: id_str.into_owned(),
                military_power,
                cb_types,
            });
        }
    }

    // Sort fallen empires: awakened first, then by military power descending
    threats.fallen_empires.sort_by(|a, b| {
        b.awakened
            .cmp(&a.awakened)
            .then_with(|| {
                b.military_power
                    .unwrap_or(0.0)
                    .partial_cmp(&a.military_power.unwrap_or(0.0))
                    .unwrap_or(std::cmp::Ordering::Equal)
            })
    });

    // Sort hostile neighbors by military power descending
    threats.hostile_neighbors.sort_by(|a, b| {
        b.military_power
            .unwrap_or(0.0)
            .partial_cmp(&a.military_power.unwrap_or(0.0))
            .unwrap_or(std::cmp::Ordering::Equal)
    });

    threats
}
