use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_string, read_string_array};

/// An active edict.
#[derive(Debug, Serialize)]
pub struct Edict {
    pub edict: String,
    pub perpetual: bool,
}

/// The progression section: traditions, ascension perks, edicts.
#[derive(Debug, Serialize)]
pub struct Progression {
    pub traditions: Vec<String>,
    pub ascension_perks: Vec<String>,
    pub edicts: Vec<Edict>,
}

/// Extract the progression section from the player's country object.
pub fn extract(country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Progression {
    let mut progression = Progression {
        traditions: Vec::new(),
        ascension_perks: Vec::new(),
        edicts: Vec::new(),
    };

    if let Some(val) = find_field(country, "traditions") {
        progression.traditions = read_string_array(&val);
    }

    if let Some(val) = find_field(country, "ascension_perks") {
        progression.ascension_perks = read_string_array(&val);
    }

    if let Some(val) = find_field(country, "edicts") {
        if let Ok(arr) = val.read_array() {
            for item in arr.values() {
                if let Ok(obj) = item.read_object() {
                    if let Some(edict_name) = read_string(&obj, "edict") {
                        let perpetual = read_string(&obj, "perpetual")
                            .map(|s| s == "yes")
                            .unwrap_or(false);
                        progression.edicts.push(Edict {
                            edict: edict_name,
                            perpetual,
                        });
                    }
                }
            }
        }
    }

    progression
}
