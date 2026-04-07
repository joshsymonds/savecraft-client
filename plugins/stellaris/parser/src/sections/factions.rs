use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_i64, read_string};

/// A pop faction.
#[derive(Debug, Serialize)]
pub struct Faction {
    pub faction_id: String,
    pub faction_type: Option<String>,
    pub support: Option<f64>,
    pub happiness: Option<f64>,
}

/// The factions section.
#[derive(Debug, Serialize)]
pub struct Factions {
    pub factions: Vec<Faction>,
}

/// Extract the factions section from the top-level pop_factions block,
/// filtered by the player's country.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    player_country_id: i64,
) -> Factions {
    let mut result = Factions {
        factions: Vec::new(),
    };

    let factions_val = match find_field(gamestate, "pop_factions") {
        Some(v) => v,
        None => return result,
    };
    let factions_obj = match factions_val.read_object() {
        Ok(o) => o,
        Err(_) => return result,
    };

    for (key, _op, value) in factions_obj.fields() {
        let faction_obj = match value.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        let country = match read_i64(&faction_obj, "country") {
            Some(c) => c,
            None => continue,
        };
        if country != player_country_id {
            continue;
        }

        result.factions.push(Faction {
            faction_id: key.read_str().into_owned(),
            faction_type: read_string(&faction_obj, "type"),
            support: read_f64(&faction_obj, "support"),
            happiness: read_f64(&faction_obj, "happiness"),
        });
    }

    result
}
