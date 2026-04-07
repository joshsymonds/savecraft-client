use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_i64, read_repeated_strings, read_string};

/// A leader in the player's empire.
#[derive(Debug, Serialize)]
pub struct Leader {
    pub leader_id: String,
    pub class: Option<String>,
    pub level: Option<i64>,
    pub experience: Option<f64>,
    pub age: Option<i64>,
    pub traits: Vec<String>,
    pub species: Option<i64>,
}

/// The leaders section.
#[derive(Debug, Serialize)]
pub struct Leaders {
    pub leaders: Vec<Leader>,
}

/// Extract the leaders section by looking up the player's leader IDs
/// in the top-level `leaders` block.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    player_country_id: i64,
) -> Leaders {
    let mut result = Leaders {
        leaders: Vec::new(),
    };

    let leaders_val = match find_field(gamestate, "leaders") {
        Some(v) => v,
        None => return result,
    };
    let leaders_obj = match leaders_val.read_object() {
        Ok(o) => o,
        Err(_) => return result,
    };

    // Walk all leaders, filter by country == player
    for (key, _op, value) in leaders_obj.fields() {
        let leader_obj = match value.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        let country = match read_i64(&leader_obj, "country") {
            Some(c) => c,
            None => continue,
        };
        if country != player_country_id {
            continue;
        }

        result.leaders.push(Leader {
            leader_id: key.read_str().into_owned(),
            class: read_string(&leader_obj, "class"),
            level: read_i64(&leader_obj, "level"),
            experience: read_f64(&leader_obj, "experience"),
            age: read_i64(&leader_obj, "age"),
            traits: read_repeated_strings(&leader_obj, "traits"),
            species: read_i64(&leader_obj, "species"),
        });
    }

    // Sort by level descending
    result.leaders.sort_by(|a, b| b.level.cmp(&a.level));

    result
}
