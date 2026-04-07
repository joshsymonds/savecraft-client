use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_i64, read_string};

/// A participant in a war (attacker or defender).
#[derive(Debug, Serialize)]
pub struct WarParticipant {
    pub country: i64,
    pub call_type: Option<String>,
}

/// An active war.
#[derive(Debug, Serialize)]
pub struct War {
    pub war_id: String,
    pub start_date: Option<String>,
    pub attacker_war_goal: Option<String>,
    pub defender_war_goal: Option<String>,
    pub attacker_war_exhaustion: Option<f64>,
    pub defender_war_exhaustion: Option<f64>,
    pub attackers: Vec<WarParticipant>,
    pub defenders: Vec<WarParticipant>,
    pub player_side: Option<String>,
}

/// The wars section: active wars involving the player.
#[derive(Debug, Serialize)]
pub struct Wars {
    pub active_wars: Vec<War>,
    pub player_at_war: bool,
}

/// Extract the wars section from the top-level gamestate.
/// Filters for wars involving the player country.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    player_country_id: i64,
) -> Wars {
    let mut wars = Wars {
        active_wars: Vec::new(),
        player_at_war: false,
    };

    let war_val = match find_field(gamestate, "war") {
        Some(v) => v,
        None => return wars,
    };
    let war_obj = match war_val.read_object() {
        Ok(o) => o,
        Err(_) => return wars,
    };

    for (_key, _op, entry_val) in war_obj.fields() {
        // Skip `none` entries (resolved wars)
        let entry_obj = match entry_val.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        let attackers = parse_participants(&entry_obj, "attackers");
        let defenders = parse_participants(&entry_obj, "defenders");

        // Check if player is involved
        let player_is_attacker = attackers.iter().any(|p| p.country == player_country_id);
        let player_is_defender = defenders.iter().any(|p| p.country == player_country_id);

        if !player_is_attacker && !player_is_defender {
            continue;
        }

        let player_side = if player_is_attacker {
            Some("attacker".to_string())
        } else {
            Some("defender".to_string())
        };

        // Extract war goals
        let attacker_war_goal = find_field(&entry_obj, "attacker_war_goal")
            .and_then(|v| v.read_object().ok())
            .and_then(|o| read_string(&o, "type"));
        let defender_war_goal = find_field(&entry_obj, "defender_war_goal")
            .and_then(|v| v.read_object().ok())
            .and_then(|o| read_string(&o, "type"));

        wars.active_wars.push(War {
            war_id: _key.read_str().into_owned(),
            start_date: read_string(&entry_obj, "start_date"),
            attacker_war_goal,
            defender_war_goal,
            attacker_war_exhaustion: read_f64(&entry_obj, "attacker_war_exhaustion"),
            defender_war_exhaustion: read_f64(&entry_obj, "defender_war_exhaustion"),
            attackers,
            defenders,
            player_side,
        });
    }

    wars.player_at_war = !wars.active_wars.is_empty();
    wars
}

fn parse_participants(
    war_obj: &ObjectReader<'_, '_, Windows1252Encoding>,
    field: &str,
) -> Vec<WarParticipant> {
    let mut participants = Vec::new();
    if let Some(val) = find_field(war_obj, field) {
        if let Ok(arr) = val.read_array() {
            for item in arr.values() {
                if let Ok(obj) = item.read_object() {
                    if let Some(country) = read_i64(&obj, "country") {
                        participants.push(WarParticipant {
                            country,
                            call_type: read_string(&obj, "call_type"),
                        });
                    }
                }
            }
        }
    }
    participants
}
