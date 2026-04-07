use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_i64, read_repeated_strings, read_string};

/// A species in the game.
#[derive(Debug, Serialize)]
pub struct Species {
    pub species_id: String,
    pub class: Option<String>,
    pub traits: Vec<String>,
    pub home_planet: Option<i64>,
}

/// The species section.
#[derive(Debug, Serialize)]
pub struct SpeciesSection {
    pub species: Vec<Species>,
    pub founder_species_id: Option<i64>,
}

/// Extract the species section from the top-level species_db,
/// filtered to species referenced by the player's founder_species_ref.
pub fn extract(
    gamestate: &ObjectReader<'_, '_, Windows1252Encoding>,
    country: &ObjectReader<'_, '_, Windows1252Encoding>,
) -> SpeciesSection {
    let founder_species_id = read_i64(country, "founder_species_ref");

    let mut result = SpeciesSection {
        species: Vec::new(),
        founder_species_id,
    };

    let species_val = match find_field(gamestate, "species_db") {
        Some(v) => v,
        None => return result,
    };
    let species_obj = match species_val.read_object() {
        Ok(o) => o,
        Err(_) => return result,
    };

    for (key, _op, value) in species_obj.fields() {
        let entry_obj = match value.read_object() {
            Ok(o) => o,
            Err(_) => continue,
        };

        // Extract traits from the nested traits={} block
        let traits = if let Some(traits_val) = find_field(&entry_obj, "traits") {
            if let Ok(traits_obj) = traits_val.read_object() {
                read_repeated_strings(&traits_obj, "trait")
            } else {
                Vec::new()
            }
        } else {
            Vec::new()
        };

        // Skip entries with no meaningful traits (template entries like species 0)
        if traits.is_empty() {
            continue;
        }

        result.species.push(Species {
            species_id: key.read_str().into_owned(),
            class: read_string(&entry_obj, "class"),
            traits,
            home_planet: read_i64(&entry_obj, "home_planet"),
        });
    }

    result
}
