use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::find_field;

/// The geography section: owned/controlled planet IDs, sectors.
#[derive(Debug, Serialize)]
pub struct Geography {
    pub owned_planet_ids: Vec<i64>,
    pub controlled_planet_ids: Vec<i64>,
}

/// Extract the geography section from the player's country object.
pub fn extract(country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Geography {
    Geography {
        owned_planet_ids: read_id_array(country, "owned_planets"),
        controlled_planet_ids: read_id_array(country, "controlled_planets"),
    }
}

fn read_id_array(obj: &ObjectReader<'_, '_, Windows1252Encoding>, field: &str) -> Vec<i64> {
    let mut result = Vec::new();
    if let Some(val) = find_field(obj, field) {
        if let Ok(arr) = val.read_array() {
            for item in arr.values() {
                if let Ok(s) = item.read_str() {
                    if let Ok(id) = s.parse::<i64>() {
                        result.push(id);
                    }
                }
            }
        }
    }
    result
}
