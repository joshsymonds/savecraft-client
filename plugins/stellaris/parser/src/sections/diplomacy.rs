use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{find_field, read_f64, read_i64, read_string};

/// A diplomatic relation with another country.
#[derive(Debug, Serialize)]
pub struct Relation {
    pub country: i64,
    pub opinion: f64,
    pub hostile: bool,
    pub trust: Option<f64>,
    pub has_communications: bool,
    pub closed_borders: bool,
}

/// The diplomacy section: relations, casus belli.
#[derive(Debug, Serialize)]
pub struct Diplomacy {
    pub relations: Vec<Relation>,
    pub casus_belli: Vec<CasusBelli>,
}

/// An available casus belli against another country.
#[derive(Debug, Serialize)]
pub struct CasusBelli {
    pub cb_type: String,
    pub target_country: i64,
}

/// Extract the diplomacy section from the player's country object.
pub fn extract(country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Diplomacy {
    let mut diplomacy = Diplomacy {
        relations: Vec::new(),
        casus_belli: Vec::new(),
    };

    // Extract relations from relations_manager
    if let Some(rm_val) = find_field(country, "relations_manager") {
        if let Ok(rm_obj) = rm_val.read_object() {
            // Relations are repeated `relation={}` blocks
            for (key, _op, value) in rm_obj.fields() {
                if key.read_str() != "relation" {
                    continue;
                }
                if let Ok(rel_obj) = value.read_object() {
                    let country_id = match read_i64(&rel_obj, "country") {
                        Some(id) => id,
                        None => continue,
                    };
                    let opinion = read_f64(&rel_obj, "relation_current").unwrap_or(0.0);
                    let hostile = read_string(&rel_obj, "hostile")
                        .map(|s| s == "yes")
                        .unwrap_or(false);
                    let trust = read_f64(&rel_obj, "trust");
                    let has_communications = read_string(&rel_obj, "communications")
                        .map(|s| s == "yes")
                        .unwrap_or(false);
                    let closed_borders = read_string(&rel_obj, "closed_borders")
                        .map(|s| s == "yes")
                        .unwrap_or(false);

                    diplomacy.relations.push(Relation {
                        country: country_id,
                        opinion,
                        hostile,
                        trust,
                        has_communications,
                        closed_borders,
                    });
                }
            }
        }
    }

    // Sort relations by opinion (most hostile first — most interesting to AI)
    diplomacy
        .relations
        .sort_by(|a, b| a.opinion.partial_cmp(&b.opinion).unwrap_or(std::cmp::Ordering::Equal));

    // Extract casus belli from standard_diplomacy_module
    if let Some(mod_val) = find_field(country, "modules") {
        if let Ok(mod_obj) = mod_val.read_object() {
            if let Some(diplo_val) = find_field(&mod_obj, "standard_diplomacy_module") {
                if let Ok(diplo_obj) = diplo_val.read_object() {
                    if let Some(cb_val) = find_field(&diplo_obj, "casus_belli") {
                        if let Ok(cb_arr) = cb_val.read_array() {
                            for item in cb_arr.values() {
                                if let Ok(cb_obj) = item.read_object() {
                                    let cb_type = match read_string(&cb_obj, "type") {
                                        Some(t) => t,
                                        None => continue,
                                    };
                                    let target = match read_i64(&cb_obj, "country") {
                                        Some(id) => id,
                                        None => continue,
                                    };
                                    diplomacy.casus_belli.push(CasusBelli {
                                        cb_type,
                                        target_country: target,
                                    });
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    diplomacy
}
