//! Builds the `overview` section and derives the save's identity from the
//! decoded LevelMeta and Level GVAS trees.
//!
//! This scaffold only reads plain typed properties that uesave already
//! decodes generically (strings, ints, a DateTime struct) — no Palworld
//! "RawData" blob codecs. Those, and the remaining sections, are later-wave
//! work built on top of this pipeline.

use serde::Serialize;
use uesave::{Properties, Property, Save, StructValue, VersionInfo};

#[derive(Serialize, Default)]
pub struct Overview {
    #[serde(rename = "worldName")]
    pub world_name: Option<String>,
    #[serde(rename = "hostPlayerName")]
    pub host_player_name: Option<String>,
    #[serde(rename = "hostPlayerLevel")]
    pub host_player_level: Option<i32>,
    #[serde(rename = "inGameDay")]
    pub in_game_day: Option<i32>,
    #[serde(rename = "levelMetaVersion")]
    pub level_meta_version: Option<i32>,
    /// Raw .NET `DateTime` ticks (100ns units since 0001-01-01) from
    /// LevelMeta's `Timestamp` property, left unconverted for a later wave.
    #[serde(rename = "levelMetaTimestampTicks")]
    pub level_meta_timestamp_ticks: Option<u64>,
    #[serde(rename = "engineVersion")]
    pub engine_version: String,
    #[serde(rename = "saveGameVersion")]
    pub save_game_version: u32,
    #[serde(rename = "packageVersionUe4")]
    pub package_version_ue4: u32,
    #[serde(rename = "packageVersionUe5")]
    pub package_version_ue5: u32,
}

/// The subset of LevelMeta's properties this scaffold understands.
#[derive(Default)]
pub struct LevelMetaFields {
    pub world_name: Option<String>,
    pub host_player_name: Option<String>,
    pub host_player_level: Option<i32>,
    pub in_game_day: Option<i32>,
    pub version: Option<i32>,
    pub timestamp_ticks: Option<u64>,
}

fn find_property<'a>(props: &'a Properties, name: &str) -> Option<&'a Property> {
    props
        .0
        .iter()
        .find(|(key, _)| key.1 == name)
        .map(|(_, v)| v)
}

fn as_str(prop: &Property) -> Option<String> {
    match prop {
        Property::Str(s) => Some(s.clone()),
        _ => None,
    }
}

fn as_int(prop: &Property) -> Option<i32> {
    match prop {
        Property::Int(v) => Some(*v),
        _ => None,
    }
}

fn as_nested_struct(prop: &Property) -> Option<&Properties> {
    match prop {
        Property::Struct(StructValue::Struct(props)) => Some(props),
        _ => None,
    }
}

/// Extract the fields this scaffold's overview needs from a decoded
/// LevelMeta save. LevelMeta parses cleanly with no Palworld type hints at
/// all, so a missing field here means the field wasn't present in this
/// save's LevelMeta, not that decoding failed.
pub fn level_meta_fields(save: &Save) -> LevelMetaFields {
    let root = &save.root.properties;

    let version = find_property(root, "Version").and_then(as_int);
    let timestamp_ticks = find_property(root, "Timestamp").and_then(|p| match p {
        Property::Struct(StructValue::DateTime(ticks)) => Some(*ticks),
        _ => None,
    });

    let save_data = find_property(root, "SaveData").and_then(as_nested_struct);
    let (world_name, host_player_name, host_player_level, in_game_day) = match save_data {
        Some(props) => (
            find_property(props, "WorldName").and_then(as_str),
            find_property(props, "HostPlayerName").and_then(as_str),
            find_property(props, "HostPlayerLevel").and_then(as_int),
            find_property(props, "InGameDay").and_then(as_int),
        ),
        None => (None, None, None, None),
    };

    LevelMetaFields {
        world_name,
        host_player_name,
        host_player_level,
        in_game_day,
        version,
        timestamp_ticks,
    }
}

/// Build the `overview` section from Level's header and (optionally)
/// LevelMeta's fields. `level_meta` is `None` when LevelMeta was missing or
/// failed to decode — the caller has already degraded identity and emitted
/// a status warning for that case.
pub fn build_overview(level_meta: Option<&LevelMetaFields>, level: &Save) -> Overview {
    let header = &level.header;
    Overview {
        world_name: level_meta.and_then(|m| m.world_name.clone()),
        host_player_name: level_meta.and_then(|m| m.host_player_name.clone()),
        host_player_level: level_meta.and_then(|m| m.host_player_level),
        in_game_day: level_meta.and_then(|m| m.in_game_day),
        level_meta_version: level_meta.and_then(|m| m.version),
        level_meta_timestamp_ticks: level_meta.and_then(|m| m.timestamp_ticks),
        engine_version: header.engine_version.clone(),
        save_game_version: header.save_game_version,
        package_version_ue4: header.package_file_version_ue4(),
        package_version_ue5: header.package_file_version_ue5(),
    }
}
