use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;
use std::collections::HashMap;

use super::gamestate::{
    find_field, read_f64, read_i64, read_repeated_strings, read_string, read_string_array,
};
use super::meta::Meta;

/// The overview section: high-level empire identity and game state.
#[derive(Debug, Serialize)]
pub struct Overview {
    pub empire_name: Option<String>,
    pub player_tag: Option<String>,
    pub date: Option<String>,
    pub game_version: Option<String>,
    pub required_dlcs: Vec<String>,
    pub meta_fleets: Option<i64>,
    pub meta_planets: Option<i64>,

    // Gamestate-derived fields
    pub ethics: Vec<String>,
    pub authority: Option<String>,
    pub government_type: Option<String>,
    pub civics: Vec<String>,
    pub origin: Option<String>,
    pub personality: Option<String>,
    pub military_power: Option<f64>,
    pub economy_power: Option<f64>,
    pub tech_power: Option<f64>,
    pub victory_rank: Option<i64>,
    pub victory_score: Option<f64>,
    pub fleet_size: Option<i64>,
    pub used_naval_capacity: Option<i64>,
    pub empire_size: Option<i64>,
    pub num_pops: Option<i64>,
    pub resources: HashMap<String, f64>,
}

/// Extract the overview section from parsed meta and the player's country object.
pub fn extract(meta: &Meta, player_country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Overview {
    let mut overview = Overview {
        empire_name: meta.name.clone(),
        player_tag: meta.player.clone(),
        date: meta.date.clone(),
        game_version: meta.version.clone(),
        required_dlcs: meta.required_dlcs.clone(),
        meta_fleets: meta.meta_fleets,
        meta_planets: meta.meta_planets,
        ethics: Vec::new(),
        authority: None,
        government_type: None,
        civics: Vec::new(),
        origin: None,
        personality: None,
        military_power: None,
        economy_power: None,
        tech_power: None,
        victory_rank: None,
        victory_score: None,
        fleet_size: None,
        used_naval_capacity: None,
        empire_size: None,
        num_pops: None,
        resources: HashMap::new(),
    };

    // Extract ethics from ethos block
    if let Some(ethos_val) = find_field(player_country, "ethos") {
        if let Ok(ethos_obj) = ethos_val.read_object() {
            overview.ethics = read_repeated_strings(&ethos_obj, "ethic");
        }
    }

    // Extract government block
    if let Some(gov_val) = find_field(player_country, "government") {
        if let Ok(gov_obj) = gov_val.read_object() {
            overview.government_type = read_string(&gov_obj, "type");
            overview.authority = read_string(&gov_obj, "authority");
            overview.origin = read_string(&gov_obj, "origin");
            if let Some(civics_val) = find_field(&gov_obj, "civics") {
                overview.civics = read_string_array(&civics_val);
            }
        }
    }

    // Extract scalar fields
    overview.personality = read_string(player_country, "personality");
    overview.military_power = read_f64(player_country, "military_power");
    overview.economy_power = read_f64(player_country, "economy_power");
    overview.tech_power = read_f64(player_country, "tech_power");
    overview.victory_rank = read_i64(player_country, "victory_rank");
    overview.victory_score = read_f64(player_country, "victory_score");
    overview.fleet_size = read_i64(player_country, "fleet_size");
    overview.used_naval_capacity = read_i64(player_country, "used_naval_capacity");
    overview.empire_size = read_i64(player_country, "empire_size");
    overview.num_pops = read_i64(player_country, "num_sapient_pops");

    // Extract resource stockpiles from modules.standard_economy_module.resources
    if let Some(modules_val) = find_field(player_country, "modules") {
        if let Ok(modules_obj) = modules_val.read_object() {
            if let Some(econ_val) = find_field(&modules_obj, "standard_economy_module") {
                if let Ok(econ_obj) = econ_val.read_object() {
                    if let Some(res_val) = find_field(&econ_obj, "resources") {
                        if let Ok(res_obj) = res_val.read_object() {
                            for (key, _op, value) in res_obj.fields() {
                                let key_str = key.read_str();
                                if let Ok(val_str) = value.read_str() {
                                    if let Ok(num) = val_str.parse::<f64>() {
                                        overview
                                            .resources
                                            .insert(key_str.into_owned(), num);
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    overview
}
