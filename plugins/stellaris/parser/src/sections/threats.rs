use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_string};

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

/// The threats section: crisis entities, fallen/awakened empires in the galaxy.
#[derive(Debug, Serialize)]
pub struct Threats {
    pub crisis_active: bool,
    pub crisis_type: Option<String>,
    pub crisis_countries: Vec<CrisisCountry>,
    pub fallen_empires: Vec<FallenEmpire>,
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
            // Use the first crisis type found (there's only ever one endgame crisis)
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

    threats
}
