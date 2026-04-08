use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;
use std::collections::HashSet;

use super::gamestate::{find_field, read_display_name, read_f64, read_i64, read_string};

/// A colony (owned planet) with key data.
#[derive(Debug, Serialize)]
pub struct Colony {
    pub planet_id: i64,
    pub name: Option<String>,
    pub planet_class: Option<String>,
    pub planet_size: Option<i64>,
    pub designation: Option<String>,
    pub num_pops: Option<i64>,
    pub stability: Option<f64>,
    pub crime: Option<f64>,
    pub amenities: Option<f64>,
    pub amenities_usage: Option<f64>,
    pub free_housing: Option<f64>,
    pub total_housing: Option<f64>,
}

/// The planets section.
#[derive(Debug, Serialize)]
pub struct Planets {
    pub colonies: Vec<Colony>,
}

/// Extract the planets section from the top-level planets block,
/// filtered to the player's owned planets.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    country: &ObjectReader<'_, '_, Windows1252Encoding>,
) -> Planets {
    let mut result = Planets {
        colonies: Vec::new(),
    };

    // Get the player's owned planet IDs
    let owned_ids: HashSet<i64> = {
        let mut set = HashSet::new();
        if let Some(val) = find_field(country, "owned_planets") {
            if let Ok(arr) = val.read_array() {
                for item in arr.values() {
                    if let Ok(s) = item.read_str() {
                        if let Ok(id) = s.parse::<i64>() {
                            set.insert(id);
                        }
                    }
                }
            }
        }
        set
    };

    if owned_ids.is_empty() {
        return result;
    }

    // Find the planets.planet block
    let planets_val = match find_field(gamestate, "planets") {
        Some(v) => v,
        None => return result,
    };
    let planets_obj = match planets_val.read_object() {
        Ok(o) => o,
        Err(_) => return result,
    };
    let planet_val = match find_field(&planets_obj, "planet") {
        Some(v) => v,
        None => return result,
    };
    let planet_obj = match planet_val.read_object() {
        Ok(o) => o,
        Err(_) => return result,
    };

    for (key, _op, value) in planet_obj.fields() {
        let planet_id: i64 = match key.read_str().parse() {
            Ok(id) => id,
            Err(_) => continue,
        };
        if !owned_ids.contains(&planet_id) {
            continue;
        }
        let entry = match value.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        result.colonies.push(Colony {
            planet_id,
            name: read_display_name(&entry, "name"),
            planet_class: read_string(&entry, "planet_class"),
            planet_size: read_i64(&entry, "planet_size"),
            designation: read_string(&entry, "final_designation"),
            num_pops: read_i64(&entry, "num_sapient_pops"),
            stability: read_f64(&entry, "stability"),
            crime: read_f64(&entry, "crime"),
            amenities: read_f64(&entry, "amenities"),
            amenities_usage: read_f64(&entry, "amenities_usage"),
            free_housing: read_f64(&entry, "free_housing"),
            total_housing: read_f64(&entry, "total_housing"),
        });
    }

    // Sort by population descending
    result
        .colonies
        .sort_by(|a, b| b.num_pops.cmp(&a.num_pops));

    result
}
