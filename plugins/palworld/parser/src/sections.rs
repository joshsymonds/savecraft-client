//! Builds every ndjson section from the decoded LevelMeta/Level GVAS trees
//! (and, for player-scoped sections, each decoded Player.sav), joining
//! `rawdata`'s `RawData` codecs against uesave's own typed property tree
//! where the two need to meet (container ids, character keys, group
//! rosters, egg cross-references).
//!
//! Every join degrades gracefully: a missing or undecodable piece appends a
//! human-readable line to [`BuildResult::warnings`] (the caller emits these
//! as ndjson `status` lines) and the affected section comes back
//! incomplete rather than the whole result failing.

use crate::rawdata::{self, CharacterContainerSlot, CharacterSaveParameter, ItemSlot};
use serde::Serialize;
use std::collections::HashMap;
use uesave::{
    ByteArray, FGuid, MapEntry, Properties, Property, Save, StructValue, ValueVec, VersionInfo,
};

/// Caps a decode/join loop's per-entry degrade warnings at the first 3
/// messages, folding any further occurrences into one summary line -- a
/// single pathological save (e.g. hundreds of dangling container slots)
/// must not flood the daemon's status log with near-identical warnings.
/// Flushes its summary line (if any) automatically on drop, so a call site
/// can't forget to emit it.
struct WarningCap<'a> {
    warnings: &'a mut Vec<String>,
    category: String,
    shown: usize,
    suppressed: usize,
}

impl<'a> WarningCap<'a> {
    const SHOWN_LIMIT: usize = 3;

    fn new(warnings: &'a mut Vec<String>, category: impl Into<String>) -> Self {
        Self {
            warnings,
            category: category.into(),
            shown: 0,
            suppressed: 0,
        }
    }

    fn push(&mut self, message: String) {
        if self.shown < Self::SHOWN_LIMIT {
            self.warnings.push(message);
            self.shown += 1;
        } else {
            self.suppressed += 1;
        }
    }
}

impl Drop for WarningCap<'_> {
    fn drop(&mut self) {
        if self.suppressed > 0 {
            self.warnings.push(format!(
                "...and {} more {} failed to decode",
                self.suppressed, self.category
            ));
        }
    }
}

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
    #[serde(rename = "guildName")]
    pub guild_name: Option<String>,
    #[serde(rename = "baseCount")]
    pub base_count: usize,
    #[serde(rename = "playerCount")]
    pub player_count: usize,
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

fn as_map(prop: &Property) -> Option<&Vec<MapEntry>> {
    match prop {
        Property::Map(entries) => Some(entries),
        _ => None,
    }
}

fn as_byte_array(prop: &Property) -> Option<&[u8]> {
    match prop {
        Property::Array(ValueVec::Byte(ByteArray::Byte(bytes))) => Some(bytes),
        _ => None,
    }
}

fn as_guid(prop: &Property) -> Option<FGuid> {
    match prop {
        Property::Struct(StructValue::Guid(g)) => Some(*g),
        _ => None,
    }
}

fn as_enum(prop: &Property) -> Option<&str> {
    match prop {
        Property::Enum(s) => Some(s.as_str()),
        _ => None,
    }
}

fn as_bool(prop: &Property) -> Option<bool> {
    match prop {
        Property::Bool(b) => Some(*b),
        _ => None,
    }
}

fn as_name_array(prop: &Property) -> Option<&[String]> {
    match prop {
        Property::Array(ValueVec::Name(v)) => Some(v),
        _ => None,
    }
}

/// Converts a `{"ID": Struct(Guid(...))}` wrapper -- the shape every
/// container-id reference in this save uses, whether it's a map key
/// (`CharacterContainerSaveData`, `ItemContainerSaveData`) or a plain field
/// on a player (`OtomoCharacterContainerId`, `InventoryInfo.*ContainerId`,
/// ...) -- into the guid it wraps.
fn container_id_guid(prop: &Property) -> Option<FGuid> {
    let outer = as_nested_struct(prop)?;
    as_guid(find_property(outer, "ID")?)
}

/// Converts a `CharacterSaveParameterMap` entry's key
/// (`{PlayerUId, InstanceId, DebugName}`) into the `(player_uid,
/// instance_id)` pair used to join a character's other references.
fn character_key(prop: &Property) -> Option<(FGuid, FGuid)> {
    let outer = as_nested_struct(prop)?;
    let player_uid = as_guid(find_property(outer, "PlayerUId")?)?;
    let instance_id = as_guid(find_property(outer, "InstanceId")?)?;
    Some((player_uid, instance_id))
}

/// [`rawdata`] decodes a Palworld character/pal's own `[u8; 16]` guid
/// fields (e.g. `OwnerPlayerUId`) without any dependency on uesave's
/// `FGuid` -- this converts one into an [`FGuid`] the same way this
/// crate's own tests do, so every guid in this plugin's output (container
/// ids, player uids, owner ids alike) renders through the one canonical
/// `FGuid::to_string()` format.
fn guid_bytes_to_fguid(b: &[u8; 16]) -> FGuid {
    FGuid::new(
        u32::from_le_bytes(b[0..4].try_into().unwrap()),
        u32::from_le_bytes(b[4..8].try_into().unwrap()),
        u32::from_le_bytes(b[8..12].try_into().unwrap()),
        u32::from_le_bytes(b[12..16].try_into().unwrap()),
    )
}

fn pal_find<'a>(props: &'a [rawdata::PalProperty], name: &str) -> Option<&'a rawdata::PalValue> {
    props.iter().find(|p| p.name == name).map(|p| &p.value)
}

fn pal_str(props: &[rawdata::PalProperty], name: &str) -> Option<String> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Str(s)) => Some(s.clone()),
        _ => None,
    }
}

/// Palworld's `Level` and the `Talent_*` IV stats are `ByteProperty`
/// (`PalValue::ByteRaw`), not `IntProperty` -- see `FriendshipPoint` etc.
/// below for the `IntProperty` counterpart.
fn pal_byte(props: &[rawdata::PalProperty], name: &str) -> Option<i32> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::ByteRaw(b)) => Some(*b as i32),
        _ => None,
    }
}

fn pal_int(props: &[rawdata::PalProperty], name: &str) -> Option<i32> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Int(v)) => Some(*v),
        _ => None,
    }
}

fn pal_i64(props: &[rawdata::PalProperty], name: &str) -> Option<i64> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Int64(v)) => Some(*v),
        _ => None,
    }
}

/// `Hp`/`ShieldHP` are a nested one-field struct (`{"Value": Int64Property}`)
/// rather than a plain `Int64Property`.
fn pal_nested_i64(props: &[rawdata::PalProperty], name: &str) -> Option<i64> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Struct(inner)) => pal_i64(inner, "Value"),
        _ => None,
    }
}

fn pal_enum(props: &[rawdata::PalProperty], name: &str) -> Option<String> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Enum { value, .. }) => Some(value.clone()),
        _ => None,
    }
}

fn pal_bool(props: &[rawdata::PalProperty], name: &str) -> Option<bool> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Bool(b)) => Some(*b),
        _ => None,
    }
}

fn pal_str_array(props: &[rawdata::PalProperty], name: &str) -> Vec<String> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::ArrayStr(v)) => v.clone(),
        _ => Vec::new(),
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
/// a status warning for that case. Guild/base/player counts are filled in
/// by [`build_all`] once the rest of the world has been decoded.
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
        guild_name: None,
        base_count: 0,
        player_count: 0,
    }
}

// --- Players ---------------------------------------------------------------

#[derive(Serialize)]
pub struct PlayerSection {
    #[serde(rename = "playerUId")]
    pub player_uid: String,
    pub name: Option<String>,
    pub level: Option<i32>,
    #[serde(rename = "technologyPoint")]
    pub technology_point: Option<i32>,
    #[serde(rename = "unlockedTechnologies")]
    pub unlocked_technologies: Vec<String>,
    /// Count of `RecordData.PaldeckUnlockFlag` entries set to `true`.
    #[serde(rename = "paldeckUnlockedCount")]
    pub paldeck_unlocked_count: Option<usize>,
    #[serde(rename = "tribeCaptureCount")]
    pub tribe_capture_count: Option<i32>,
}

/// The `players` section's top-level shape -- the daemon drops any section
/// whose JSON value isn't an object, so the list is nested under a
/// descriptive key rather than emitted bare (see `docs/plugins.md`).
#[derive(Serialize)]
pub struct PlayersSection {
    pub players: Vec<PlayerSection>,
}

// --- Pals (shared by pals_party and pals_storage) ---------------------------

#[derive(Serialize)]
pub struct Pal {
    #[serde(rename = "speciesId")]
    pub species_id: String,
    pub nickname: Option<String>,
    pub level: Option<i32>,
    pub gender: Option<String>,
    #[serde(rename = "passiveSkills")]
    pub passive_skills: Vec<String>,
    #[serde(rename = "equippedSkills")]
    pub equipped_skills: Vec<String>,
    #[serde(rename = "talentHp")]
    pub talent_hp: Option<i32>,
    #[serde(rename = "talentShot")]
    pub talent_shot: Option<i32>,
    #[serde(rename = "talentDefense")]
    pub talent_defense: Option<i32>,
    #[serde(rename = "currentWorkSuitability")]
    pub current_work_suitability: Option<String>,
    pub exp: Option<i64>,
    pub hp: Option<i64>,
    #[serde(rename = "friendshipPoint")]
    pub friendship_point: Option<i32>,
    #[serde(rename = "ownerPlayerUId")]
    pub owner_player_uid: Option<String>,
}

fn build_pal(object: &[rawdata::PalProperty]) -> Pal {
    Pal {
        species_id: pal_str(object, "CharacterID").unwrap_or_default(),
        nickname: pal_str(object, "NickName"),
        level: pal_byte(object, "Level"),
        gender: pal_enum(object, "Gender"),
        passive_skills: pal_str_array(object, "PassiveSkillList"),
        equipped_skills: pal_str_array(object, "EquipWaza"),
        talent_hp: pal_byte(object, "Talent_HP"),
        talent_shot: pal_byte(object, "Talent_Shot"),
        talent_defense: pal_byte(object, "Talent_Defense"),
        current_work_suitability: pal_enum(object, "CurrentWorkSuitability"),
        exp: pal_i64(object, "Exp"),
        hp: pal_nested_i64(object, "Hp"),
        friendship_point: pal_int(object, "FriendshipPoint"),
        owner_player_uid: pal_find(object, "OwnerPlayerUId").and_then(|v| match v {
            rawdata::PalValue::Guid(g) => Some(guid_bytes_to_fguid(g).to_string()),
            _ => None,
        }),
    }
}

/// The `pals_party`/`pals_storage` sections' shared top-level shape -- see
/// [`PlayersSection`] for why the list is nested rather than bare.
#[derive(Serialize)]
pub struct PalsSection {
    pub pals: Vec<Pal>,
}

// --- Guild -------------------------------------------------------------------

#[derive(Serialize)]
pub struct GuildMember {
    pub name: Option<String>,
    #[serde(rename = "speciesId")]
    pub species_id: Option<String>,
    #[serde(rename = "isPlayer")]
    pub is_player: bool,
    pub level: Option<i32>,
}

#[derive(Serialize)]
pub struct Guild {
    pub name: String,
    /// The guild's true roster size (`GroupSaveData.member_handle_ids.len()`),
    /// which can exceed `members.len()` when a member's
    /// `CharacterSaveParameterMap` entry is missing -- such a member is
    /// omitted from `members` (with a warning) rather than fabricated, so
    /// this count stays honest even when the roster is degraded.
    #[serde(rename = "memberCount")]
    pub member_count: usize,
    pub members: Vec<GuildMember>,
}

/// The `guild` section's top-level shape -- see [`PlayersSection`] for why
/// the list is nested rather than bare.
#[derive(Serialize)]
pub struct GuildsSection {
    pub guilds: Vec<Guild>,
}

// --- Bases -------------------------------------------------------------------

#[derive(Serialize)]
pub struct Base {
    #[serde(rename = "baseId")]
    pub base_id: String,
}

/// The `bases` section's top-level shape -- see [`PlayersSection`] for why
/// the list is nested rather than bare. `count` mirrors `bases.len()`,
/// named explicitly so a consumer doesn't have to count the array itself.
#[derive(Serialize)]
pub struct BasesSection {
    pub bases: Vec<Base>,
    pub count: usize,
}

// --- Inventory ---------------------------------------------------------------

#[derive(Serialize)]
pub struct InventoryItem {
    #[serde(rename = "slotIndex")]
    pub slot_index: i32,
    #[serde(rename = "staticId")]
    pub static_id: String,
    pub count: i32,
    /// `Some(character_id)` when this item is an unhatched egg (joined
    /// against `DynamicItemSaveData`), naming what it will hatch into.
    #[serde(rename = "eggHatchesInto")]
    pub egg_hatches_into: Option<String>,
}

#[derive(Serialize)]
pub struct InventoryContainer {
    pub role: String,
    pub items: Vec<InventoryItem>,
}

#[derive(Serialize)]
pub struct PlayerInventory {
    #[serde(rename = "playerUId")]
    pub player_uid: String,
    pub containers: Vec<InventoryContainer>,
}

/// The `inventory` section's top-level shape -- see [`PlayersSection`] for
/// why the list is nested rather than bare. Named `inventories` (not
/// `items`/`eggs`) because the actual data is per-player, each with its own
/// role-labeled containers -- not a flat item list.
#[derive(Serialize)]
pub struct InventorySection {
    pub inventories: Vec<PlayerInventory>,
}

/// `InventoryInfo` field name -> a stable, readable role label for the
/// `inventory` section. Order here is the order containers are emitted in.
const INVENTORY_ROLES: &[(&str, &str)] = &[
    ("CommonContainerId", "common"),
    ("DropSlotContainerId", "dropSlot"),
    ("EssentialContainerId", "essential"),
    ("WeaponLoadOutContainerId", "weaponLoadOut"),
    ("PlayerEquipArmorContainerId", "equippedArmor"),
    ("FoodEquipContainerId", "equippedFood"),
];

// --- World: every RawData collection decoded once, joined by the section
// builders below. ------------------------------------------------------------

struct CharacterEntry {
    params: CharacterSaveParameter,
}

struct World {
    characters: HashMap<FGuid, CharacterEntry>,
    /// `(group_id, GroupType string, decoded roster)`.
    groups: Vec<(FGuid, String, rawdata::GroupSaveData)>,
    char_containers: HashMap<FGuid, Vec<CharacterContainerSlot>>,
    item_containers: HashMap<FGuid, Vec<ItemSlot>>,
    /// `(created_world_id, local_id_in_created_world)` -> the pal an egg
    /// will hatch into. Only egg-kind `DynamicItemSaveData` entries are
    /// ever looked up (see `build_inventory_item`), so non-egg entries
    /// aren't indexed at all.
    dynamic_items: HashMap<([u8; 16], [u8; 16]), String>,
}

impl World {
    fn find_character(&self, instance_id: FGuid) -> Option<&CharacterEntry> {
        self.characters.get(&instance_id)
    }
}

fn decode_characters(
    wsd: &Properties,
    warnings: &mut Vec<String>,
) -> HashMap<FGuid, CharacterEntry> {
    let Some(entries) = find_property(wsd, "CharacterSaveParameterMap").and_then(as_map) else {
        warnings.push(
            "CharacterSaveParameterMap missing from Level.sav; players/pals/guild sections will be incomplete"
                .to_string(),
        );
        return HashMap::new();
    };

    let mut out = HashMap::with_capacity(entries.len());
    let mut degraded = WarningCap::new(warnings, "characters");
    for entry in entries {
        // `player_uid` (the owning player) isn't needed here: `instance_id`
        // alone is what every other section joins characters by (container
        // slots, guild rosters, a player's own IndividualId).
        let Some((_player_uid, instance_id)) = character_key(&entry.key) else {
            degraded.push(
                "a CharacterSaveParameterMap entry has an unrecognized key shape; skipped"
                    .to_string(),
            );
            continue;
        };
        let raw = as_nested_struct(&entry.value).and_then(|p| find_property(p, "RawData"));
        let Some(raw) = raw.and_then(as_byte_array) else {
            degraded.push(format!(
                "character {instance_id} is missing RawData; skipped"
            ));
            continue;
        };
        match rawdata::decode_character_save_parameter(raw) {
            Ok(params) => {
                out.insert(instance_id, CharacterEntry { params });
            }
            Err(e) => degraded.push(format!(
                "character {instance_id} RawData failed to decode ({e}); skipped"
            )),
        }
    }
    out
}

fn decode_groups(
    wsd: &Properties,
    warnings: &mut Vec<String>,
) -> Vec<(FGuid, String, rawdata::GroupSaveData)> {
    let Some(entries) = find_property(wsd, "GroupSaveDataMap").and_then(as_map) else {
        warnings.push(
            "GroupSaveDataMap missing from Level.sav; guild section will be empty".to_string(),
        );
        return Vec::new();
    };

    let mut out = Vec::with_capacity(entries.len());
    let mut degraded = WarningCap::new(warnings, "groups");
    for entry in entries {
        let Some(group_id) = as_guid(&entry.key) else {
            degraded.push(
                "a GroupSaveDataMap entry has an unrecognized key shape; skipped".to_string(),
            );
            continue;
        };
        let Some(props) = as_nested_struct(&entry.value) else {
            degraded.push(format!(
                "group {group_id} has an unrecognized value shape; skipped"
            ));
            continue;
        };
        let group_type = find_property(props, "GroupType")
            .and_then(as_enum)
            .unwrap_or_default()
            .to_string();
        let Some(raw) = find_property(props, "RawData").and_then(as_byte_array) else {
            degraded.push(format!("group {group_id} is missing RawData; skipped"));
            continue;
        };
        match rawdata::decode_group_save_data(raw) {
            Ok(g) => out.push((group_id, group_type, g)),
            Err(e) => degraded.push(format!(
                "group {group_id} RawData failed to decode ({e}); skipped"
            )),
        }
    }
    out
}

/// Shared shape between `CharacterContainerSaveData` and
/// `ItemContainerSaveData`: a map of container id -> `{Slots: [...]}`,
/// each occupied slot's `RawData` decoded by a per-container-kind decoder.
fn decode_slotted_containers<T>(
    wsd: &Properties,
    warnings: &mut Vec<String>,
    map_name: &str,
    entry_article_and_name: &str,
    kind_label: &str,
    missing_map_warning: &str,
    decode_slot: impl Fn(&[u8]) -> Result<Option<T>, rawdata::RawDataError>,
) -> HashMap<FGuid, Vec<T>> {
    let Some(entries) = find_property(wsd, map_name).and_then(as_map) else {
        warnings.push(missing_map_warning.to_string());
        return HashMap::new();
    };

    let mut out = HashMap::with_capacity(entries.len());
    let mut degraded = WarningCap::new(warnings, format!("{kind_label} container entries"));
    for entry in entries {
        let Some(id) = container_id_guid(&entry.key) else {
            degraded.push(format!(
                "{entry_article_and_name} entry has an unrecognized key shape; skipped"
            ));
            continue;
        };
        let Some(props) = as_nested_struct(&entry.value) else {
            degraded.push(format!(
                "{kind_label} container {id} has an unrecognized value shape; skipped"
            ));
            continue;
        };
        let Some(slots) = find_property(props, "Slots") else {
            out.insert(id, Vec::new());
            continue;
        };
        let Property::Array(ValueVec::Struct(slot_structs)) = slots else {
            degraded.push(format!(
                "{kind_label} container {id} has a non-array Slots field; skipped"
            ));
            continue;
        };

        let mut occupied = Vec::new();
        for slot in slot_structs {
            let StructValue::Struct(slot_props) = slot else {
                degraded.push(format!(
                    "{kind_label} container {id} has a non-struct slot; skipped"
                ));
                continue;
            };
            let Some(raw) = find_property(slot_props, "RawData").and_then(as_byte_array) else {
                continue;
            };
            match decode_slot(raw) {
                Ok(Some(item)) => occupied.push(item),
                Ok(None) => {}
                Err(e) => degraded.push(format!(
                    "{kind_label} container {id} slot RawData failed to decode ({e}); skipped"
                )),
            }
        }
        out.insert(id, occupied);
    }
    out
}

fn decode_char_containers(
    wsd: &Properties,
    warnings: &mut Vec<String>,
) -> HashMap<FGuid, Vec<CharacterContainerSlot>> {
    decode_slotted_containers(
        wsd,
        warnings,
        "CharacterContainerSaveData",
        "a CharacterContainerSaveData",
        "character",
        "CharacterContainerSaveData missing from Level.sav; pals_party/pals_storage sections will be empty",
        rawdata::decode_character_container_slot,
    )
}

fn decode_item_containers(
    wsd: &Properties,
    warnings: &mut Vec<String>,
) -> HashMap<FGuid, Vec<ItemSlot>> {
    decode_slotted_containers(
        wsd,
        warnings,
        "ItemContainerSaveData",
        "an ItemContainerSaveData",
        "item",
        "ItemContainerSaveData missing from Level.sav; inventory section will be empty",
        rawdata::decode_item_slot,
    )
}

fn decode_dynamic_items(
    wsd: &Properties,
    warnings: &mut Vec<String>,
) -> HashMap<([u8; 16], [u8; 16]), String> {
    let Some(Property::Array(ValueVec::Struct(items))) = find_property(wsd, "DynamicItemSaveData")
    else {
        warnings.push(
            "DynamicItemSaveData missing from Level.sav; egg cross-references in the inventory section will be unavailable"
                .to_string(),
        );
        return HashMap::new();
    };

    let mut out = HashMap::with_capacity(items.len());
    let mut degraded = WarningCap::new(warnings, "dynamic items");
    for item in items {
        let StructValue::Struct(props) = item else {
            degraded
                .push("a DynamicItemSaveData entry has an unrecognized shape; skipped".to_string());
            continue;
        };
        let Some(raw) = find_property(props, "RawData").and_then(as_byte_array) else {
            continue;
        };
        match rawdata::decode_dynamic_item(raw) {
            Ok(Some(decoded)) => {
                if let rawdata::DynamicItemKind::Egg { character_id, .. } = decoded.kind {
                    out.insert(
                        (
                            decoded.id.created_world_id,
                            decoded.id.local_id_in_created_world,
                        ),
                        character_id,
                    );
                }
            }
            Ok(None) => {}
            Err(e) => degraded.push(format!(
                "a DynamicItemSaveData entry's RawData failed to decode ({e}); skipped"
            )),
        }
    }
    out
}

fn decode_base_ids(wsd: &Properties, warnings: &mut Vec<String>) -> Vec<FGuid> {
    let Some(entries) = find_property(wsd, "BaseCampSaveData").and_then(as_map) else {
        warnings.push(
            "BaseCampSaveData missing from Level.sav; bases section will be empty".to_string(),
        );
        return Vec::new();
    };

    let mut out = Vec::with_capacity(entries.len());
    let mut degraded = WarningCap::new(warnings, "base camps");
    for entry in entries {
        match as_guid(&entry.key) {
            Some(id) => out.push(id),
            None => degraded.push(
                "a BaseCampSaveData entry has an unrecognized key shape; skipped".to_string(),
            ),
        }
    }
    out
}

fn player_save_data(player: &Save) -> Option<&Properties> {
    find_property(&player.root.properties, "SaveData").and_then(as_nested_struct)
}

fn player_uid(player: &Save) -> Option<FGuid> {
    as_guid(find_property(player_save_data(player)?, "PlayerUId")?)
}

fn player_container_id(player: &Save, field: &str) -> Option<FGuid> {
    container_id_guid(find_property(player_save_data(player)?, field)?)
}

fn build_player_section(player: &Save, world: &World, warnings: &mut Vec<String>) -> PlayerSection {
    let uid = player_uid(player);
    let Some(save_data) = player_save_data(player) else {
        warnings.push("a player save is missing SaveData; degraded".to_string());
        return PlayerSection {
            player_uid: uid.map(|g| g.to_string()).unwrap_or_default(),
            name: None,
            level: None,
            technology_point: None,
            unlocked_technologies: Vec::new(),
            paldeck_unlocked_count: None,
            tribe_capture_count: None,
        };
    };

    let individual_instance_id = find_property(save_data, "IndividualId")
        .and_then(as_nested_struct)
        .and_then(|p| find_property(p, "InstanceId"))
        .and_then(as_guid);

    let character =
        individual_instance_id.and_then(|instance_id| world.find_character(instance_id));
    if character.is_none() {
        warnings.push(
            "a player's own CharacterSaveParameterMap entry could not be joined; name/level degraded"
                .to_string(),
        );
    }

    let name = character.and_then(|c| pal_str(&c.params.object, "NickName"));
    let level = character.and_then(|c| pal_byte(&c.params.object, "Level"));

    let technology_point = find_property(save_data, "TechnologyPoint").and_then(as_int);
    let unlocked_technologies = find_property(save_data, "UnlockedRecipeTechnologyNames")
        .and_then(as_name_array)
        .map(|v| v.to_vec())
        .unwrap_or_default();

    let record_data = find_property(save_data, "RecordData").and_then(as_nested_struct);
    let paldeck_unlocked_count = record_data
        .and_then(|p| find_property(p, "PaldeckUnlockFlag"))
        .and_then(as_map)
        .map(|entries| {
            entries
                .iter()
                .filter(|e| as_bool(&e.value) == Some(true))
                .count()
        });
    let tribe_capture_count = record_data
        .and_then(|p| find_property(p, "TribeCaptureCount"))
        .and_then(as_int);

    PlayerSection {
        player_uid: uid.map(|g| g.to_string()).unwrap_or_default(),
        name,
        level,
        technology_point,
        unlocked_technologies,
        paldeck_unlocked_count,
        tribe_capture_count,
    }
}

fn build_pals_from_container(
    container_id: Option<FGuid>,
    world: &World,
    warnings: &mut Vec<String>,
    label: &str,
) -> Vec<Pal> {
    let Some(container_id) = container_id else {
        warnings.push(format!(
            "{label} container id not found on a player; that player's section is empty"
        ));
        return Vec::new();
    };
    let Some(slots) = world.char_containers.get(&container_id) else {
        warnings.push(format!(
            "{label} container ({container_id}) not found in CharacterContainerSaveData; section degraded"
        ));
        return Vec::new();
    };

    let mut degraded = WarningCap::new(warnings, format!("{label} pals"));
    slots
        .iter()
        .filter_map(|slot| {
            let instance_id = guid_bytes_to_fguid(&slot.instance_id);
            match world.find_character(instance_id) {
                Some(c) => Some(build_pal(&c.params.object)),
                None => {
                    degraded.push(format!(
                        "{label} slot references instance id {instance_id} with no matching CharacterSaveParameterMap entry; skipped"
                    ));
                    None
                }
            }
        })
        .collect()
}

fn build_guilds(world: &World, warnings: &mut Vec<String>) -> Vec<Guild> {
    world
        .groups
        .iter()
        .filter(|(_, group_type, _)| group_type == "EPalGroupType::Guild")
        .map(|(group_id, _, g)| {
            let mut degraded = WarningCap::new(&mut *warnings, "guild members");
            // A dangling member (no matching CharacterSaveParameterMap
            // entry) is omitted from the roster rather than fabricated as
            // an all-`None` placeholder -- `member_count` below still
            // reflects the save's true roster size, so it can legitimately
            // exceed `members.len()` when this happens.
            let members: Vec<GuildMember> = g
                .member_handle_ids
                .iter()
                .filter_map(|(_, instance_id)| {
                    let instance_id = guid_bytes_to_fguid(instance_id);
                    match world.find_character(instance_id) {
                        Some(c) => Some(GuildMember {
                            name: pal_str(&c.params.object, "NickName"),
                            species_id: pal_str(&c.params.object, "CharacterID"),
                            is_player: pal_bool(&c.params.object, "IsPlayer").unwrap_or(false),
                            level: pal_byte(&c.params.object, "Level"),
                        }),
                        None => {
                            degraded.push(format!(
                                "guild {group_id} member {instance_id} has no matching CharacterSaveParameterMap entry; omitted from roster"
                            ));
                            None
                        }
                    }
                })
                .collect();
            Guild {
                name: g.group_name.clone(),
                member_count: g.member_handle_ids.len(),
                members,
            }
        })
        .collect()
}

fn build_inventory_item(slot: &ItemSlot, world: &World) -> InventoryItem {
    let egg_hatches_into = slot.dynamic_item_id.as_ref().and_then(|dynamic_id| {
        world
            .dynamic_items
            .get(&(
                dynamic_id.created_world_id,
                dynamic_id.local_id_in_created_world,
            ))
            .cloned()
    });

    InventoryItem {
        slot_index: slot.slot_index,
        static_id: slot.static_id.clone(),
        count: slot.count,
        egg_hatches_into,
    }
}

fn build_player_inventory(
    player: &Save,
    world: &World,
    warnings: &mut Vec<String>,
) -> PlayerInventory {
    let uid = player_uid(player);
    let inventory_info = player_save_data(player)
        .and_then(|sd| find_property(sd, "InventoryInfo"))
        .and_then(as_nested_struct);

    let mut containers = Vec::new();
    match inventory_info {
        Some(inventory_info) => {
            let mut degraded = WarningCap::new(warnings, "inventory containers");
            for (field, role) in INVENTORY_ROLES {
                let Some(container_id) =
                    find_property(inventory_info, field).and_then(container_id_guid)
                else {
                    continue;
                };
                match world.item_containers.get(&container_id) {
                    Some(slots) => {
                        let items = slots.iter().map(|slot| build_inventory_item(slot, world)).collect();
                        containers.push(InventoryContainer {
                            role: (*role).to_string(),
                            items,
                        });
                    }
                    None => degraded.push(format!(
                        "{role} item container ({container_id}) not found in ItemContainerSaveData; skipped"
                    )),
                }
            }
        }
        None => warnings
            .push("a player save is missing InventoryInfo; inventory section degraded".to_string()),
    }

    PlayerInventory {
        player_uid: uid.map(|g| g.to_string()).unwrap_or_default(),
        containers,
    }
}

/// Every section this plugin emits, plus the human-readable degrade
/// warnings collected while building them (the caller emits these as
/// ndjson `status` lines).
pub struct BuildResult {
    pub overview: Overview,
    pub players: Vec<PlayerSection>,
    pub pals_party: Vec<Pal>,
    pub pals_storage: Vec<Pal>,
    pub guild: Vec<Guild>,
    pub bases: Vec<Base>,
    pub inventory: Vec<PlayerInventory>,
    pub warnings: Vec<String>,
}

/// Builds every section from a decoded Level.sav, optionally-decoded
/// LevelMeta fields, and every successfully-decoded Player.sav. Never
/// fails outright: a missing or malformed piece of the world degrades the
/// affected section (with a warning) rather than the whole result.
pub fn build_all(
    level: &Save,
    level_meta: Option<&LevelMetaFields>,
    players: &[Save],
) -> BuildResult {
    let mut warnings = Vec::new();
    let mut overview = build_overview(level_meta, level);
    overview.player_count = players.len();

    let wsd = find_property(&level.root.properties, "worldSaveData").and_then(as_nested_struct);
    let Some(wsd) = wsd else {
        warnings.push(
            "worldSaveData missing from Level.sav; players/pals/guild/bases/inventory sections degraded"
                .to_string(),
        );
        return BuildResult {
            overview,
            players: Vec::new(),
            pals_party: Vec::new(),
            pals_storage: Vec::new(),
            guild: Vec::new(),
            bases: Vec::new(),
            inventory: Vec::new(),
            warnings,
        };
    };

    let characters = decode_characters(wsd, &mut warnings);
    let groups = decode_groups(wsd, &mut warnings);
    let char_containers = decode_char_containers(wsd, &mut warnings);
    let item_containers = decode_item_containers(wsd, &mut warnings);
    let dynamic_items = decode_dynamic_items(wsd, &mut warnings);
    let base_ids = decode_base_ids(wsd, &mut warnings);

    let world = World {
        characters,
        groups,
        char_containers,
        item_containers,
        dynamic_items,
    };

    assemble_sections(overview, &world, &base_ids, players, warnings)
}

/// The second half of [`build_all`]: joins an already-decoded [`World`]
/// (plus each player's own `Save`) into every section. Split out so tests
/// can exercise this exact join logic against a directly-constructed
/// `World` -- e.g. one with a thousand synthetic storage pals -- without
/// needing real `RawData` bytes for each one.
fn assemble_sections(
    mut overview: Overview,
    world: &World,
    base_ids: &[FGuid],
    players: &[Save],
    mut warnings: Vec<String>,
) -> BuildResult {
    let players_section: Vec<PlayerSection> = players
        .iter()
        .map(|p| build_player_section(p, world, &mut warnings))
        .collect();

    let pals_party = players
        .iter()
        .flat_map(|p| {
            let container_id = player_container_id(p, "OtomoCharacterContainerId");
            build_pals_from_container(container_id, world, &mut warnings, "party")
        })
        .collect();

    let pals_storage = players
        .iter()
        .flat_map(|p| {
            let container_id = player_container_id(p, "PalStorageContainerId");
            build_pals_from_container(container_id, world, &mut warnings, "storage")
        })
        .collect();

    let guild = build_guilds(world, &mut warnings);
    let bases: Vec<Base> = base_ids
        .iter()
        .map(|id| Base {
            base_id: id.to_string(),
        })
        .collect();
    let inventory = players
        .iter()
        .map(|p| build_player_inventory(p, world, &mut warnings))
        .collect();

    overview.guild_name = guild.first().map(|g| g.name.clone());
    overview.base_count = bases.len();

    BuildResult {
        overview,
        players: players_section,
        pals_party,
        pals_storage,
        guild,
        bases,
        inventory,
        warnings,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use uesave::{Header, PropertySchemas, Root};

    fn empty_world() -> World {
        World {
            characters: HashMap::new(),
            groups: Vec::new(),
            char_containers: HashMap::new(),
            item_containers: HashMap::new(),
            dynamic_items: HashMap::new(),
        }
    }

    fn pal_prop(name: &str, value: rawdata::PalValue) -> rawdata::PalProperty {
        rawdata::PalProperty {
            name: name.to_string(),
            type_name: String::new(),
            value,
        }
    }

    fn character_entry(object: Vec<rawdata::PalProperty>) -> CharacterEntry {
        CharacterEntry {
            params: CharacterSaveParameter {
                object,
                unknown: [0u8; 4],
                group_id: [0u8; 16],
                trailing: Vec::new(),
            },
        }
    }

    /// A distinguishable `[u8; 16]` guid: `tag` picks the category (player,
    /// party pal, storage pal, ...), `index` disambiguates within it.
    fn synthetic_guid_bytes(tag: u8, index: u32) -> [u8; 16] {
        let mut bytes = [0u8; 16];
        bytes[0] = tag;
        bytes[12..16].copy_from_slice(&index.to_le_bytes());
        bytes
    }

    /// Builds a minimal but real `uesave::Save` around a hand-constructed
    /// property tree -- lets tests exercise the real `build_player_section`/
    /// `build_player_inventory` join logic without needing actual `RawData`
    /// bytes or a real GVAS-decoded fixture. `Header`'s own fields are all
    /// `pub`, but `PackageVersion`'s aren't, so it's built via its derived
    /// `Deserialize` impl rather than a struct literal.
    fn synthetic_save(root_properties: Properties) -> Save {
        let header: Header = serde_json::from_value(serde_json::json!({
            "magic": u32::from_le_bytes(*b"GVAS"),
            "save_game_version": 3,
            "package_version": {"ue4": 522, "ue5": 1008},
            "engine_version_major": 5,
            "engine_version_minor": 1,
            "engine_version_patch": 1,
            "engine_version_build": 0,
            "engine_version": "++UE5+Release-5.1",
            "custom_version": null,
        }))
        .expect("construct a synthetic uesave Header via its derived Deserialize impl");
        Save {
            header,
            schemas: PropertySchemas::default(),
            root: Root {
                save_game_type: "Test".to_string(),
                properties: root_properties,
            },
            extra: Vec::new(),
        }
    }

    /// A synthetic `Players/*.sav`-equivalent `Save`: its root holds exactly
    /// one `SaveData` struct property, matching every real player save.
    fn player_save(save_data: Properties) -> Save {
        let mut root = Properties::default();
        root.insert("SaveData", Property::Struct(StructValue::Struct(save_data)));
        synthetic_save(root)
    }

    fn guid_prop(g: FGuid) -> Property {
        Property::Struct(StructValue::Guid(g))
    }

    /// The `{"InstanceId": Guid(...)}` shape `IndividualId` carries on a
    /// player save.
    fn individual_id_prop(instance_id: FGuid) -> Property {
        let mut inner = Properties::default();
        inner.insert("InstanceId", guid_prop(instance_id));
        Property::Struct(StructValue::Struct(inner))
    }

    /// The `{"ID": Guid(...)}` wrapper every container-id reference in a
    /// real save uses (see [`container_id_guid`]).
    fn container_ref(id: FGuid) -> Property {
        let mut inner = Properties::default();
        inner.insert("ID", guid_prop(id));
        Property::Struct(StructValue::Struct(inner))
    }

    // The real fixture's only egg (`PalEgg_Water_01`, in container index 69 --
    // see rawdata_test.rs) lives in a base storage box, not any of this
    // fixture's single player's own six tracked inventory containers, so the
    // happy-path pipeline test never exercises an egg actually reaching
    // build_inventory_item's dynamic-item join. These two tests cover that
    // join directly against constructed (not fixture) data instead.

    #[test]
    fn build_inventory_item_resolves_an_egg_via_its_dynamic_item_id() {
        let slot = ItemSlot {
            slot_index: 0,
            count: 1,
            static_id: "PalEgg_Fire_01".to_string(),
            dynamic_item_id: Some(rawdata::ItemSlotDynamicId {
                created_world_id: [0u8; 16],
                local_id_in_created_world: [7u8; 16],
            }),
            trailing: Vec::new(),
        };
        let mut world = empty_world();
        // Same created_world_id, different local_id_in_created_world --
        // proves the join keys on the full (created_world_id,
        // local_id_in_created_world) pair, not created_world_id alone.
        world
            .dynamic_items
            .insert(([0u8; 16], [8u8; 16]), "Penguin".to_string());
        world
            .dynamic_items
            .insert(([0u8; 16], [7u8; 16]), "Monkey_Fire".to_string());

        let item = build_inventory_item(&slot, &world);
        assert_eq!(item.egg_hatches_into, Some("Monkey_Fire".to_string()));
    }

    #[test]
    fn build_inventory_item_static_items_never_get_an_egg_hatches_into() {
        let slot = ItemSlot {
            slot_index: 0,
            count: 749,
            static_id: "Money".to_string(),
            dynamic_item_id: None,
            trailing: Vec::new(),
        };
        let world = empty_world();

        let item = build_inventory_item(&slot, &world);
        assert_eq!(item.egg_hatches_into, None);
    }

    // --- WarningCap ----------------------------------------------------

    #[test]
    fn warning_cap_shows_first_three_and_summarizes_the_rest() {
        let mut warnings = Vec::new();
        {
            let mut cap = WarningCap::new(&mut warnings, "widgets");
            for i in 0..5 {
                cap.push(format!("widget {i} failed"));
            }
        }
        assert_eq!(
            warnings,
            vec![
                "widget 0 failed".to_string(),
                "widget 1 failed".to_string(),
                "widget 2 failed".to_string(),
                "...and 2 more widgets failed to decode".to_string(),
            ]
        );
    }

    #[test]
    fn warning_cap_emits_no_summary_line_when_nothing_was_suppressed() {
        let mut warnings = Vec::new();
        {
            let mut cap = WarningCap::new(&mut warnings, "widgets");
            cap.push("widget 0 failed".to_string());
        }
        assert_eq!(warnings, vec!["widget 0 failed".to_string()]);
    }

    // --- Degrade branches (GAP 2) ---------------------------------------

    #[test]
    fn build_all_missing_world_save_data_degrades_to_six_empty_sections_and_one_warning() {
        let level = synthetic_save(Properties::default());

        let result = build_all(&level, None, &[]);

        assert!(result.players.is_empty());
        assert!(result.pals_party.is_empty());
        assert!(result.pals_storage.is_empty());
        assert!(result.guild.is_empty());
        assert!(result.bases.is_empty());
        assert!(result.inventory.is_empty());
        assert_eq!(
            result.warnings,
            vec![
                "worldSaveData missing from Level.sav; players/pals/guild/bases/inventory sections degraded"
                    .to_string()
            ]
        );
    }

    #[test]
    fn build_pals_from_container_dangling_instance_id_is_empty_with_a_warning_naming_the_guid() {
        let container_id = guid_bytes_to_fguid(&synthetic_guid_bytes(9, 0));
        let dangling_instance_bytes = synthetic_guid_bytes(10, 0);
        let dangling_instance_id = guid_bytes_to_fguid(&dangling_instance_bytes);

        let mut world = empty_world();
        world.char_containers.insert(
            container_id,
            vec![CharacterContainerSlot {
                player_uid: [0u8; 16],
                instance_id: dangling_instance_bytes,
                trailing: Vec::new(),
            }],
        );

        let mut warnings = Vec::new();
        let pals = build_pals_from_container(Some(container_id), &world, &mut warnings, "party");

        assert!(pals.is_empty());
        assert!(
            warnings
                .iter()
                .any(|w| w.contains(&dangling_instance_id.to_string())),
            "expected a warning naming the dangling instance id: {warnings:?}"
        );
    }

    #[test]
    fn build_pals_from_container_container_not_found_is_empty_with_a_warning() {
        let container_id = guid_bytes_to_fguid(&synthetic_guid_bytes(11, 0));
        let world = empty_world(); // no entry for container_id at all

        let mut warnings = Vec::new();
        let pals = build_pals_from_container(Some(container_id), &world, &mut warnings, "storage");

        assert!(pals.is_empty());
        assert!(
            warnings
                .iter()
                .any(|w| w.contains("not found in CharacterContainerSaveData")),
            "expected a container-not-found warning: {warnings:?}"
        );
    }

    #[test]
    fn build_guilds_dangling_member_is_omitted_from_roster_but_counted_in_member_count() {
        let resolvable_bytes = synthetic_guid_bytes(12, 0);
        let resolvable_id = guid_bytes_to_fguid(&resolvable_bytes);
        let dangling_bytes = synthetic_guid_bytes(13, 0);
        let dangling_id = guid_bytes_to_fguid(&dangling_bytes);

        let mut world = empty_world();
        world.characters.insert(
            resolvable_id,
            character_entry(vec![pal_prop(
                "NickName",
                rawdata::PalValue::Str("Chicken".to_string()),
            )]),
        );
        world.groups.push((
            guid_bytes_to_fguid(&synthetic_guid_bytes(14, 0)),
            "EPalGroupType::Guild".to_string(),
            rawdata::GroupSaveData {
                group_id: synthetic_guid_bytes(14, 0),
                group_name: "Test Guild".to_string(),
                member_handle_ids: vec![([0u8; 16], resolvable_bytes), ([0u8; 16], dangling_bytes)],
                trailing: Vec::new(),
            },
        ));

        let mut warnings = Vec::new();
        let guilds = build_guilds(&world, &mut warnings);

        assert_eq!(guilds.len(), 1);
        assert_eq!(
            guilds[0].member_count, 2,
            "memberCount should reflect the guild's true (raw) roster size"
        );
        assert_eq!(
            guilds[0].members.len(),
            1,
            "the dangling member should be omitted from the roster, not fabricated as a placeholder"
        );
        assert_eq!(guilds[0].members[0].name, Some("Chicken".to_string()));
        assert!(
            warnings
                .iter()
                .any(|w| w.contains(&dangling_id.to_string()) && w.contains("omitted from roster")),
            "expected a warning naming the dangling guild member: {warnings:?}"
        );
    }

    #[test]
    fn build_player_section_character_join_failure_degrades_name_and_level_with_a_warning() {
        let instance_id = guid_bytes_to_fguid(&synthetic_guid_bytes(15, 0));
        let mut save_data = Properties::default();
        save_data.insert("PlayerUId", guid_prop(instance_id));
        save_data.insert("IndividualId", individual_id_prop(instance_id));
        let player = player_save(save_data);
        let world = empty_world(); // no character with a matching instance id

        let mut warnings = Vec::new();
        let section = build_player_section(&player, &world, &mut warnings);

        assert_eq!(section.name, None);
        assert_eq!(section.level, None);
        assert!(
            warnings.iter().any(|w| w.contains("could not be joined")),
            "expected a warning about the failed character join: {warnings:?}"
        );
    }

    #[test]
    fn build_player_inventory_missing_inventory_info_degrades_to_empty_containers_with_a_warning() {
        let mut save_data = Properties::default();
        save_data.insert(
            "PlayerUId",
            guid_prop(guid_bytes_to_fguid(&synthetic_guid_bytes(16, 0))),
        );
        let player = player_save(save_data);
        let world = empty_world();

        let mut warnings = Vec::new();
        let inventory = build_player_inventory(&player, &world, &mut warnings);

        assert!(inventory.containers.is_empty());
        assert!(
            warnings.iter().any(|w| w.contains("missing InventoryInfo")),
            "expected a missing-InventoryInfo warning: {warnings:?}"
        );
    }

    /// A full-detail synthetic pal property object -- every field populated
    /// with a realistically-sized value (not stubbed to `None`), since the
    /// point of the scale test below is proving 1,000 pals *at full detail*
    /// stay under the 2 MiB cap, not 1,000 sparse stubs. Decoded through the
    /// real `build_pal` the same as any fixture-derived pal would be.
    fn synthetic_pal_object(i: usize) -> Vec<rawdata::PalProperty> {
        use rawdata::PalValue;
        vec![
            pal_prop(
                "CharacterID",
                PalValue::Str(format!("SyntheticSpecies_{:04}", i % 50)),
            ),
            pal_prop("NickName", PalValue::Str(format!("Buddy the {i}th"))),
            pal_prop("Level", PalValue::ByteRaw(((i % 60) + 1) as u8)),
            pal_prop(
                "Gender",
                PalValue::Enum {
                    enum_type: "EPalGenderType".to_string(),
                    value: if i.is_multiple_of(2) {
                        "EPalGenderType::Male"
                    } else {
                        "EPalGenderType::Female"
                    }
                    .to_string(),
                },
            ),
            pal_prop(
                "PassiveSkillList",
                PalValue::ArrayStr(vec![
                    format!("PAL_passive_skill_{}", i % 20),
                    "ElementBoost_Fire_1_PAL".to_string(),
                ]),
            ),
            pal_prop(
                "EquipWaza",
                PalValue::ArrayStr(vec![format!("EPalWazaID::Unique_Species_{}_Move", i % 20)]),
            ),
            pal_prop("Talent_HP", PalValue::ByteRaw((i % 100) as u8)),
            pal_prop("Talent_Shot", PalValue::ByteRaw((i % 100) as u8)),
            pal_prop("Talent_Defense", PalValue::ByteRaw((i % 100) as u8)),
            pal_prop(
                "CurrentWorkSuitability",
                PalValue::Enum {
                    enum_type: "EPalWorkSuitability".to_string(),
                    value: "EPalWorkSuitability::Deforest".to_string(),
                },
            ),
            pal_prop("Exp", PalValue::Int64(i as i64 * 137)),
            pal_prop(
                "Hp",
                PalValue::Struct(vec![pal_prop(
                    "Value",
                    PalValue::Int64(i as i64 * 1000 + 500_000),
                )]),
            ),
            pal_prop("FriendshipPoint", PalValue::Int(500)),
            pal_prop(
                "OwnerPlayerUId",
                PalValue::Guid([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0]),
            ),
            pal_prop("IsPlayer", PalValue::Bool(false)),
        ]
    }

    /// Builds a synthetic `World` (1,000 storage pals, 3 party pals, a
    /// 37-member guild, six populated inventory containers, one base) and
    /// its single player's `Save`, then drives them through the real
    /// `assemble_sections` -> `build_sections_map` -> `ndjson::encode_result`
    /// chain -- the exact path `run()` uses -- so this test would catch GAP
    /// 1's bare-array regression (a wrapped-object assertion below) as well
    /// as a real size blowup, instead of asserting against a hand-built
    /// envelope that could drift from what the daemon actually receives.
    #[test]
    fn scale_full_result_with_1000_storage_pals_stays_under_2_mib_through_the_real_assembly_and_emit_path()
     {
        let player_id_bytes = synthetic_guid_bytes(1, 0);
        let player_instance_id = guid_bytes_to_fguid(&player_id_bytes);

        let mut characters = HashMap::new();
        characters.insert(
            player_instance_id,
            character_entry(vec![
                pal_prop("NickName", rawdata::PalValue::Str("Atmus".to_string())),
                pal_prop("Level", rawdata::PalValue::ByteRaw(9)),
                pal_prop("IsPlayer", rawdata::PalValue::Bool(true)),
            ]),
        );

        let party_id_bytes: Vec<[u8; 16]> = (0..3).map(|k| synthetic_guid_bytes(2, k)).collect();
        for (k, bytes) in party_id_bytes.iter().enumerate() {
            let id = guid_bytes_to_fguid(bytes);
            characters.insert(id, character_entry(synthetic_pal_object(k)));
        }

        let storage_id_bytes: Vec<[u8; 16]> =
            (0..1000).map(|k| synthetic_guid_bytes(3, k)).collect();
        for (k, bytes) in storage_id_bytes.iter().enumerate() {
            let id = guid_bytes_to_fguid(bytes);
            characters.insert(id, character_entry(synthetic_pal_object(k)));
        }

        let party_container_id = guid_bytes_to_fguid(&synthetic_guid_bytes(4, 0));
        let storage_container_id = guid_bytes_to_fguid(&synthetic_guid_bytes(4, 1));
        let mut char_containers = HashMap::new();
        char_containers.insert(
            party_container_id,
            party_id_bytes
                .iter()
                .map(|bytes| CharacterContainerSlot {
                    player_uid: player_id_bytes,
                    instance_id: *bytes,
                    trailing: Vec::new(),
                })
                .collect(),
        );
        char_containers.insert(
            storage_container_id,
            storage_id_bytes
                .iter()
                .map(|bytes| CharacterContainerSlot {
                    player_uid: player_id_bytes,
                    instance_id: *bytes,
                    trailing: Vec::new(),
                })
                .collect(),
        );

        let item_container_ids: Vec<FGuid> = (0..INVENTORY_ROLES.len() as u32)
            .map(|k| guid_bytes_to_fguid(&synthetic_guid_bytes(5, k)))
            .collect();
        let mut item_containers = HashMap::new();
        for id in &item_container_ids {
            item_containers.insert(
                *id,
                (0..20)
                    .map(|i| ItemSlot {
                        slot_index: i,
                        count: i + 1,
                        static_id: format!("Item_{i:03}"),
                        dynamic_item_id: None,
                        trailing: Vec::new(),
                    })
                    .collect(),
            );
        }

        let group_id = guid_bytes_to_fguid(&synthetic_guid_bytes(8, 0));
        let mut member_handle_ids = vec![(player_id_bytes, player_id_bytes)];
        for bytes in storage_id_bytes.iter().take(36) {
            member_handle_ids.push((player_id_bytes, *bytes));
        }
        let groups = vec![(
            group_id,
            "EPalGroupType::Guild".to_string(),
            rawdata::GroupSaveData {
                group_id: synthetic_guid_bytes(8, 0),
                group_name: "Unnamed Guild".to_string(),
                member_handle_ids,
                trailing: Vec::new(),
            },
        )];

        let world = World {
            characters,
            groups,
            char_containers,
            item_containers,
            dynamic_items: HashMap::new(),
        };

        let base_ids = vec![guid_bytes_to_fguid(&synthetic_guid_bytes(7, 0))];

        let mut save_data = Properties::default();
        save_data.insert("PlayerUId", guid_prop(player_instance_id));
        save_data.insert("IndividualId", individual_id_prop(player_instance_id));
        save_data.insert("TechnologyPoint", Property::Int(14));
        save_data.insert(
            "UnlockedRecipeTechnologyNames",
            Property::Array(ValueVec::Name(
                (0..27).map(|i| format!("Technology_{i}")).collect(),
            )),
        );
        let mut record_data = Properties::default();
        let mut paldeck = Vec::new();
        for i in 0..10 {
            paldeck.push(MapEntry {
                key: Property::Int(i),
                value: Property::Bool(true),
            });
        }
        for i in 10..15 {
            paldeck.push(MapEntry {
                key: Property::Int(i),
                value: Property::Bool(false),
            });
        }
        record_data.insert("PaldeckUnlockFlag", Property::Map(paldeck));
        record_data.insert("TribeCaptureCount", Property::Int(10));
        save_data.insert(
            "RecordData",
            Property::Struct(StructValue::Struct(record_data)),
        );
        save_data.insert(
            "OtomoCharacterContainerId",
            container_ref(party_container_id),
        );
        save_data.insert("PalStorageContainerId", container_ref(storage_container_id));
        let mut inventory_info = Properties::default();
        for (i, (field, _role)) in INVENTORY_ROLES.iter().enumerate() {
            inventory_info.insert(*field, container_ref(item_container_ids[i]));
        }
        save_data.insert(
            "InventoryInfo",
            Property::Struct(StructValue::Struct(inventory_info)),
        );
        let player = player_save(save_data);

        let overview = Overview {
            world_name: Some("Palpagos Islands".to_string()),
            host_player_name: Some("Atmus".to_string()),
            host_player_level: Some(9),
            in_game_day: Some(4),
            level_meta_version: Some(100),
            level_meta_timestamp_ticks: Some(639210506566940000),
            engine_version: "++UE5+Release-5.1".to_string(),
            save_game_version: 3,
            package_version_ue4: 522,
            package_version_ue5: 1008,
            guild_name: None,
            base_count: 0,
            player_count: 1,
        };

        let built = assemble_sections(
            overview,
            &world,
            &base_ids,
            std::slice::from_ref(&player),
            Vec::new(),
        );
        assert!(
            built.warnings.is_empty(),
            "expected a clean synthetic world with no degrade warnings: {:?}",
            built.warnings
        );
        assert_eq!(built.pals_party.len(), 3);
        assert_eq!(built.pals_storage.len(), 1000);
        assert_eq!(built.guild.len(), 1);
        assert_eq!(built.guild[0].member_count, 37);
        assert_eq!(built.bases.len(), 1);

        let sections_map = crate::build_sections_map(built);
        let identity = crate::ndjson::Identity {
            save_name: "Palpagos Islands".to_string(),
            game_id: "palworld".to_string(),
            extra: Some(serde_json::json!({ "worldId": "1FCE97C34D214643B96A23A20A9E27D1" })),
        };
        let encoded =
            crate::ndjson::encode_result(identity, "Palpagos Islands".to_string(), sections_map);

        assert!(
            encoded.len() < 2 * 1024 * 1024,
            "encoded result line was {} bytes, expected under 2 MiB",
            encoded.len()
        );

        // Prove the six wrapped-object shapes (GAP 1) are what actually
        // reaches the wire, not just what the section builders return
        // internally -- this is the assertion that would have caught the
        // bare-array regression the daemon silently drops.
        let value: serde_json::Value = serde_json::from_slice(&encoded).unwrap();
        let sections = &value["sections"];
        assert_eq!(
            sections["players"]["data"]["players"]
                .as_array()
                .unwrap()
                .len(),
            1
        );
        assert_eq!(
            sections["pals_party"]["data"]["pals"]
                .as_array()
                .unwrap()
                .len(),
            3
        );
        assert_eq!(
            sections["pals_storage"]["data"]["pals"]
                .as_array()
                .unwrap()
                .len(),
            1000
        );
        assert_eq!(
            sections["guild"]["data"]["guilds"]
                .as_array()
                .unwrap()
                .len(),
            1
        );
        assert_eq!(
            sections["bases"]["data"]["bases"].as_array().unwrap().len(),
            1
        );
        assert_eq!(sections["bases"]["data"]["count"], 1);
        assert_eq!(
            sections["inventory"]["data"]["inventories"]
                .as_array()
                .unwrap()
                .len(),
            1
        );
    }
}
