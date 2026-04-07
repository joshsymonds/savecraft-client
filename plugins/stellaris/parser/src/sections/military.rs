use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;

use super::gamestate::{read_f64, read_i64};

/// The military section: fleet power, fleet size, naval capacity.
#[derive(Debug, Serialize)]
pub struct Military {
    pub military_power: Option<f64>,
    pub fleet_size: Option<i64>,
    pub used_naval_capacity: Option<i64>,
    pub empire_size: Option<i64>,
}

/// Extract the military section from the player's country object.
///
/// For now this extracts the summary stats from the country block.
/// Deep fleet/ship composition parsing will be added when we implement
/// cross-referencing with the top-level fleet block.
pub fn extract(country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Military {
    Military {
        military_power: read_f64(country, "military_power"),
        fleet_size: read_i64(country, "fleet_size"),
        used_naval_capacity: read_i64(country, "used_naval_capacity"),
        empire_size: read_i64(country, "empire_size"),
    }
}
