use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_i64, read_string};

/// An archaeological site.
#[derive(Debug, Serialize)]
pub struct ArchSite {
    pub site_id: String,
    pub site_type: Option<String>,
    pub clue_progress: Option<f64>,
    pub planet: Option<i64>,
}

/// The exploration section.
#[derive(Debug, Serialize)]
pub struct Exploration {
    pub archaeological_sites: Vec<ArchSite>,
}

/// Extract the exploration section from the top-level archaeological_sites block.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    player_country_id: i64,
) -> Exploration {
    let mut result = Exploration {
        archaeological_sites: Vec::new(),
    };

    let sites_val = match find_field(gamestate, "archaeological_sites") {
        Some(v) => v,
        None => return result,
    };
    let sites_obj = match sites_val.read_object() {
        Ok(o) => o,
        Err(_) => return result,
    };

    for (key, _op, value) in sites_obj.fields() {
        let site_obj = match value.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        // Filter by owner/excavator
        let owner = read_i64(&site_obj, "owner");
        let excavator = read_i64(&site_obj, "excavator_country");
        if owner != Some(player_country_id) && excavator != Some(player_country_id) {
            continue;
        }

        result.archaeological_sites.push(ArchSite {
            site_id: key.read_str().into_owned(),
            site_type: read_string(&site_obj, "type"),
            clue_progress: read_f64(&site_obj, "clue_progress"),
            planet: read_i64(&site_obj, "planet"),
        });
    }

    result
}
