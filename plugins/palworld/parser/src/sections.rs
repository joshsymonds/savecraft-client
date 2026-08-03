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
use std::collections::{BTreeMap, HashMap, HashSet};
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
        Some(rawdata::PalValue::Enum { value, .. }) => Some(bare_ue_enum_value(value).to_string()),
        _ => None,
    }
}

/// UE's enum qualification is redundant once the output field identifies the type.
fn bare_ue_enum_value(value: &str) -> &str {
    value.rsplit_once("::").map_or(value, |(_, bare)| bare)
}

fn pal_bool(props: &[rawdata::PalProperty], name: &str) -> Option<bool> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::Bool(b)) => Some(*b),
        _ => None,
    }
}

fn pal_str_array(props: &[rawdata::PalProperty], name: &str) -> Vec<String> {
    match pal_find(props, name) {
        Some(rawdata::PalValue::ArrayStr(v)) => v
            .iter()
            .map(|value| bare_ue_enum_value(value).to_string())
            .collect(),
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

#[derive(Serialize, Clone)]
pub struct Position {
    pub x: f64,
    pub y: f64,
    pub z: f64,
}

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
    pub position: Option<Position>,
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
    #[serde(skip_serializing_if = "Option::is_none")]
    pub nickname: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sex: Option<String>,
    #[serde(rename = "instanceId")]
    pub instance_id: String,
    #[serde(rename = "passiveSkills")]
    pub passive_skills: Vec<String>,
    #[serde(rename = "equippedSkills")]
    pub equipped_skills: Vec<String>,
    #[serde(rename = "talentHp", skip_serializing_if = "Option::is_none")]
    pub talent_hp: Option<i32>,
    #[serde(rename = "talentShot", skip_serializing_if = "Option::is_none")]
    pub talent_shot: Option<i32>,
    #[serde(rename = "talentDefense", skip_serializing_if = "Option::is_none")]
    pub talent_defense: Option<i32>,
    #[serde(
        rename = "currentWorkSuitability",
        skip_serializing_if = "Option::is_none"
    )]
    pub current_work_suitability: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub exp: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub hp: Option<i64>,
    #[serde(rename = "friendshipPoint", skip_serializing_if = "Option::is_none")]
    pub friendship_point: Option<i32>,
    #[serde(rename = "ownerPlayerUId", skip_serializing_if = "Option::is_none")]
    pub owner_player_uid: Option<String>,
}

fn build_pal(instance_id: FGuid, object: &[rawdata::PalProperty]) -> Pal {
    let sex = pal_enum(object, "Gender");
    Pal {
        species_id: pal_str(object, "CharacterID").unwrap_or_default(),
        nickname: pal_str(object, "NickName"),
        level: pal_byte(object, "Level"),
        sex,
        instance_id: instance_id.to_string(),
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
    #[serde(rename = "playerUId", skip_serializing_if = "Option::is_none")]
    pub player_uid: Option<String>,
    pub level: Option<i32>,
}

#[derive(Serialize)]
pub struct Guild {
    #[serde(rename = "guildId")]
    pub guild_id: String,
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

#[derive(Serialize, Clone)]
pub struct Base {
    #[serde(rename = "baseId")]
    pub base_id: String,
    #[serde(rename = "guildId")]
    pub guild_id: Option<String>,
    pub name: String,
    pub position: Position,
    pub workers: Vec<String>,
    /// Base-linked `MapObjectSaveData` entries aggregated by verbatim
    /// `MapObjectId`. Deliberately NOT called `buildings`: the save gives no
    /// way to tell a player-built structure from a world object, and the
    /// base-linked set genuinely includes dropped-item pickups
    /// (`CommonDropItem3D`) and scenery (`DamagableRock…`). The cloud
    /// classifies and names these ids by joining `DT_BuildObjectDataTable`
    /// on `MapObjectId` (see `docs/v1.2-fields.md`).
    #[serde(rename = "mapObjects")]
    pub map_objects: Vec<BaseMapObject>,
    pub items: BaseItems,
}

#[derive(Serialize, Clone)]
pub struct BaseMapObject {
    pub id: String,
    pub count: usize,
    pub flags: BaseMapObjectFlags,
}

/// Per-condition tallies over the `count` objects of one `MapObjectId`.
///
/// Every value is one JSON type family -- an integer tally, or `null` when
/// the flag exists but this build can source no signal for it at all. A
/// consumer's schema is `number|null` for every key; a string sentinel in an
/// integer field is forbidden.
///
/// Sourceable tallies may overlap each other (REQ-9): one object can be both
/// powered and damaged. `unknown` is NOT a peer flag -- it counts the objects
/// for which NO condition signal of any kind was readable, so it is mutually
/// exclusive with every sourceable flag. The invariant, asserted by
/// `tests/pipeline_test.rs`, is: `unknown` plus the number of objects
/// carrying at least one sourceable signal equals `count`. `working` is the
/// only sourceable flag in this build, so that reduces to
/// `unknown + working == count`.
#[derive(Serialize, Clone)]
pub struct BaseMapObjectFlags {
    pub powered: Option<usize>,
    /// Objects whose `Workee` module GUID resolves to a `WorkSaveData`
    /// record with a readable `WorkableType` -- the only condition signal
    /// confirmed in these fixtures.
    pub working: Option<usize>,
    pub damaged: Option<usize>,
    #[serde(rename = "under_construction")]
    pub under_construction: Option<usize>,
    pub unknown: usize,
}

#[derive(Serialize, Clone)]
pub struct BaseItems {
    pub items: Vec<BaseItem>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub trimmed: Option<BaseItemsTrimmed>,
}

#[derive(Serialize, Clone)]
pub struct BaseItem {
    pub id: String,
    pub quantity: i64,
}

#[derive(Serialize, Clone)]
pub struct BaseItemsTrimmed {
    #[serde(rename = "types_omitted")]
    pub types_omitted: usize,
    #[serde(rename = "quantity_omitted")]
    pub quantity_omitted: i64,
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

struct World {
    characters: HashMap<FGuid, CharacterSaveParameter>,
    /// `(group_id, GroupType string, decoded roster)`.
    groups: Vec<(FGuid, String, rawdata::GroupSaveData)>,
    char_containers: HashMap<FGuid, Vec<CharacterContainerSlot>>,
    item_containers: HashMap<FGuid, Vec<ItemSlot>>,
    /// `(created_world_id, local_id_in_created_world)` -> the pal an egg
    /// will hatch into. Only egg-kind `DynamicItemSaveData` entries are
    /// ever looked up (see `build_inventory_item`), so non-egg entries
    /// aren't indexed at all.
    dynamic_items: HashMap<([u8; 16], [u8; 16]), String>,
    bases: HashMap<FGuid, rawdata::BaseCamp>,
    base_worker_containers: HashMap<FGuid, FGuid>,
    map_objects: Vec<MapObject>,
    workable_ids: HashSet<FGuid>,
}

struct MapObject {
    base_id: FGuid,
    id: String,
    item_container_ids: Vec<FGuid>,
    workee_id: Option<FGuid>,
}

impl World {
    fn find_character(&self, instance_id: FGuid) -> Option<&CharacterSaveParameter> {
        self.characters.get(&instance_id)
    }
}

/// Every `CharacterSaveParameterMap` instance id any section actually joins
/// against: each player's own character, every party/storage container
/// slot's occupant, and every Guild-type group's roster (the only group
/// type [`build_guilds`] displays -- roster members' names still need their
/// character entries, so their ids belong in this set too). Anything else
/// in `CharacterSaveParameterMap` (e.g. a non-Guild group's members) is
/// never surfaced by any section, so [`decode_characters`] skips decoding
/// its `RawData` entirely (see P7).
fn collect_needed_character_ids(
    players: &[Save],
    char_containers: &HashMap<FGuid, Vec<CharacterContainerSlot>>,
    groups: &[(FGuid, String, rawdata::GroupSaveData)],
) -> HashSet<FGuid> {
    let mut needed = HashSet::new();

    for player in players {
        if let Some(instance_id) = player_individual_instance_id(player) {
            needed.insert(instance_id);
        }
        for field in ["OtomoCharacterContainerId", "PalStorageContainerId"] {
            let Some(container_id) = player_container_id(player, field) else {
                continue;
            };
            let Some(slots) = char_containers.get(&container_id) else {
                continue;
            };
            needed.extend(slots.iter().map(|s| guid_bytes_to_fguid(&s.instance_id)));
        }
    }

    for (_, group_type, g) in groups {
        if group_type == "EPalGroupType::Guild" {
            needed.extend(
                g.member_handle_ids
                    .iter()
                    .map(|(_, instance_id)| guid_bytes_to_fguid(instance_id)),
            );
        }
    }

    needed
}

/// Formats a per-entry `RawData` decode failure's warning, naming `what`
/// (e.g. `"character {id}"`) and the failure. Phrases
/// `RawDataErrorKind::Unsupported` distinctly from genuine corruption
/// (`Malformed`/`Truncated`) -- an unrecognized property type means this
/// build's save format has drifted past what this plugin's codecs
/// recognize, not that the entry itself is broken -- and records its path
/// in `unsupported` so the caller can summarize or escalate it (see C2).
fn describe_rawdata_failure(
    what: &str,
    e: &rawdata::RawDataError,
    unsupported: &mut Vec<String>,
) -> String {
    if matches!(e.kind, rawdata::RawDataErrorKind::Unsupported(_)) {
        unsupported.push(e.path.clone());
        format!(
            "{what} RawData failed to decode: newer save format than this plugin supports ({e}); skipped"
        )
    } else {
        format!("{what} RawData failed to decode ({e}); skipped")
    }
}

fn decode_characters(
    wsd: &Properties,
    needed: &HashSet<FGuid>,
    warnings: &mut Vec<String>,
    unsupported: &mut Vec<String>,
) -> HashMap<FGuid, CharacterSaveParameter> {
    let Some(entries) = find_property(wsd, "CharacterSaveParameterMap").and_then(as_map) else {
        warnings.push(
            "CharacterSaveParameterMap missing from Level.sav; players/pals/guild sections will be incomplete"
                .to_string(),
        );
        return HashMap::new();
    };

    let mut out = HashMap::with_capacity(needed.len());
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
        if !needed.contains(&instance_id) {
            // Not referenced by any player, container slot, or guild
            // roster this world surfaces -- decoding its RawData would be
            // wasted work (see P7).
            continue;
        }
        let raw = as_nested_struct(&entry.value).and_then(|p| find_property(p, "RawData"));
        let Some(raw) = raw.and_then(as_byte_array) else {
            degraded.push(format!(
                "character {instance_id} is missing RawData; skipped"
            ));
            continue;
        };
        match rawdata::decode_character_save_parameter(raw) {
            Ok(params) => {
                out.insert(instance_id, params);
            }
            Err(e) => degraded.push(describe_rawdata_failure(
                &format!("character {instance_id}"),
                &e,
                unsupported,
            )),
        }
    }
    out
}

fn decode_groups(
    wsd: &Properties,
    warnings: &mut Vec<String>,
    unsupported: &mut Vec<String>,
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
            Err(e) => degraded.push(describe_rawdata_failure(
                &format!("group {group_id}"),
                &e,
                unsupported,
            )),
        }
    }
    out
}

/// Shared shape between `CharacterContainerSaveData` and
/// `ItemContainerSaveData`: a map of container id -> `{Slots: [...]}`,
/// each occupied slot's `RawData` decoded by a per-container-kind decoder.
/// `kind_label` (e.g. `"character"`/`"item"`) drives every derived message;
/// the article form (`"a"`/`"an"`) and the missing-map warning are both
/// derived from `map_name`/`kind_label` rather than passed separately.
fn decode_slotted_containers<T>(
    wsd: &Properties,
    warnings: &mut Vec<String>,
    unsupported: &mut Vec<String>,
    map_name: &str,
    kind_label: &str,
    decode_slot: impl Fn(&[u8]) -> Result<Option<T>, rawdata::RawDataError>,
) -> HashMap<FGuid, Vec<T>> {
    let article = if map_name.starts_with(['A', 'E', 'I', 'O', 'U']) {
        "an"
    } else {
        "a"
    };

    let Some(entries) = find_property(wsd, map_name).and_then(as_map) else {
        warnings.push(format!(
            "{map_name} missing from Level.sav; {kind_label}-related sections will be empty"
        ));
        return HashMap::new();
    };

    let mut out = HashMap::with_capacity(entries.len());
    let mut degraded = WarningCap::new(warnings, format!("{kind_label} container entries"));
    for entry in entries {
        let Some(id) = container_id_guid(&entry.key) else {
            degraded.push(format!(
                "{article} {map_name} entry has an unrecognized key shape; skipped"
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
                Err(e) => degraded.push(describe_rawdata_failure(
                    &format!("{kind_label} container {id} slot"),
                    &e,
                    unsupported,
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
    unsupported: &mut Vec<String>,
) -> HashMap<FGuid, Vec<CharacterContainerSlot>> {
    decode_slotted_containers(
        wsd,
        warnings,
        unsupported,
        "CharacterContainerSaveData",
        "character",
        rawdata::decode_character_container_slot,
    )
}

fn decode_item_containers(
    wsd: &Properties,
    warnings: &mut Vec<String>,
    unsupported: &mut Vec<String>,
) -> HashMap<FGuid, Vec<ItemSlot>> {
    decode_slotted_containers(
        wsd,
        warnings,
        unsupported,
        "ItemContainerSaveData",
        "item",
        rawdata::decode_item_slot,
    )
}

fn decode_dynamic_items(
    wsd: &Properties,
    warnings: &mut Vec<String>,
    unsupported: &mut Vec<String>,
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
            Err(e) => degraded.push(describe_rawdata_failure(
                "a DynamicItemSaveData entry's",
                &e,
                unsupported,
            )),
        }
    }
    out
}

fn decode_bases(wsd: &Properties, warnings: &mut Vec<String>) -> HashMap<FGuid, rawdata::BaseCamp> {
    let Some(entries) = find_property(wsd, "BaseCampSaveData").and_then(as_map) else {
        warnings.push(
            "BaseCampSaveData missing from Level.sav; bases section will be empty".to_string(),
        );
        return HashMap::new();
    };

    let mut out = HashMap::with_capacity(entries.len());
    let mut degraded = WarningCap::new(warnings, "base camps");
    for entry in entries {
        let Some(id) = as_guid(&entry.key) else {
            degraded.push(
                "a BaseCampSaveData entry has an unrecognized key shape; skipped".to_string(),
            );
            continue;
        };
        let Some(raw) = as_nested_struct(&entry.value)
            .and_then(|props| find_property(props, "RawData"))
            .and_then(as_byte_array)
        else {
            degraded.push(format!("base camp {id} is missing RawData; skipped"));
            continue;
        };
        match rawdata::decode_base_camp(raw) {
            Ok(base) if guid_bytes_to_fguid(&base.base_id) == id => {
                out.insert(id, base);
            }
            Ok(_) => degraded.push(format!(
                "base camp {id} RawData id does not match map key; skipped"
            )),
            Err(e) => degraded.push(format!(
                "base camp {id} RawData failed to decode ({e}); skipped"
            )),
        }
    }
    out
}

fn decode_base_worker_containers(wsd: &Properties) -> HashMap<FGuid, FGuid> {
    find_property(wsd, "BaseCampSaveData")
        .and_then(as_map)
        .into_iter()
        .flatten()
        .filter_map(|entry| {
            let id = as_guid(&entry.key)?;
            let raw = as_nested_struct(&entry.value)
                .and_then(|props| find_property(props, "WorkerDirector"))
                .and_then(as_nested_struct)
                .and_then(|props| find_property(props, "RawData"))
                .and_then(as_byte_array)?;
            raw.get(98..114)?
                .try_into()
                .ok()
                .map(|bytes: [u8; 16]| (id, guid_bytes_to_fguid(&bytes)))
        })
        .collect()
}

fn decode_map_objects(wsd: &Properties, warnings: &mut Vec<String>) -> Vec<MapObject> {
    let Some(Property::Array(ValueVec::Struct(objects))) = find_property(wsd, "MapObjectSaveData")
    else {
        warnings.push(
            "MapObjectSaveData missing from Level.sav; base map objects/items will be empty"
                .to_string(),
        );
        return Vec::new();
    };
    let mut out = Vec::new();
    let mut degraded = WarningCap::new(warnings, "map objects");
    for object in objects {
        let StructValue::Struct(props) = object else {
            continue;
        };
        let (Some(id), Some(model_raw)) = (
            find_property(props, "MapObjectId").and_then(|p| match p {
                Property::Name(name) => Some(name.clone()),
                _ => None,
            }),
            find_property(props, "Model")
                .and_then(as_nested_struct)
                .and_then(|p| find_property(p, "RawData"))
                .and_then(as_byte_array),
        ) else {
            continue;
        };
        let base_id = match rawdata::decode_base_id(model_raw) {
            Ok(id) => guid_bytes_to_fguid(&id),
            Err(e) => {
                degraded.push(format!(
                    "map object {id} model failed to decode ({e}); skipped"
                ));
                continue;
            }
        };
        let mut item_container_ids = Vec::new();
        let mut workee_id = None;
        if let Some(modules) = find_property(props, "ConcreteModel")
            .and_then(as_nested_struct)
            .and_then(|p| find_property(p, "ModuleMap"))
            .and_then(as_map)
        {
            for module in modules {
                let Some(kind) = as_enum(&module.key) else {
                    continue;
                };
                let Some(raw) = as_nested_struct(&module.value)
                    .and_then(|p| find_property(p, "RawData"))
                    .and_then(as_byte_array)
                else {
                    continue;
                };
                if kind.ends_with("::ItemContainer") {
                    if let Ok(id) = rawdata::decode_module_guid(
                        raw,
                        "MapObjectSaveData.ConcreteModel.ModuleMap.ItemContainer.RawData",
                    ) {
                        item_container_ids.push(guid_bytes_to_fguid(&id));
                    }
                } else if kind.ends_with("::Workee")
                    && let Ok(id) = rawdata::decode_module_guid(
                        raw,
                        "MapObjectSaveData.ConcreteModel.ModuleMap.Workee.RawData",
                    )
                {
                    workee_id = Some(guid_bytes_to_fguid(&id));
                }
            }
        }
        out.push(MapObject {
            base_id,
            id,
            item_container_ids,
            workee_id,
        });
    }
    out
}

fn decode_workable_ids(wsd: &Properties) -> HashSet<FGuid> {
    let Some(Property::Array(ValueVec::Struct(records))) = find_property(wsd, "WorkSaveData")
    else {
        return HashSet::new();
    };
    records
        .iter()
        .filter_map(|record| {
            let StructValue::Struct(props) = record else {
                return None;
            };
            as_enum(find_property(props, "WorkableType")?)?;
            let raw = find_property(props, "RawData").and_then(as_byte_array)?;
            rawdata::decode_module_guid(raw, "WorkSaveData.RawData")
                .ok()
                .map(|id| guid_bytes_to_fguid(&id))
        })
        .collect()
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

fn player_position(save_data: &Properties) -> Option<Position> {
    let transform = find_property(save_data, "LastTransform").and_then(as_nested_struct)?;
    match find_property(transform, "Translation")? {
        Property::Struct(StructValue::Vector(vector)) => Some(Position {
            x: vector.x.0,
            y: vector.y.0,
            z: vector.z.0,
        }),
        _ => None,
    }
}

/// A player's own `CharacterSaveParameterMap` instance id, via
/// `SaveData.IndividualId.InstanceId` -- the key every player's own
/// character entry (and, from there, their group/guild membership) joins
/// by.
fn player_individual_instance_id(player: &Save) -> Option<FGuid> {
    find_property(player_save_data(player)?, "IndividualId")
        .and_then(as_nested_struct)
        .and_then(|p| find_property(p, "InstanceId"))
        .and_then(as_guid)
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
            position: None,
        };
    };

    let character = player_individual_instance_id(player)
        .and_then(|instance_id| world.find_character(instance_id));
    if character.is_none() {
        warnings.push(
            "a player's own CharacterSaveParameterMap entry could not be joined; name/level degraded"
                .to_string(),
        );
    }

    let name = character.and_then(|c| pal_str(&c.object, "NickName"));
    let level = character.and_then(|c| pal_byte(&c.object, "Level"));

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
    let position = player_position(save_data);

    PlayerSection {
        player_uid: uid.map(|g| g.to_string()).unwrap_or_default(),
        name,
        level,
        technology_point,
        unlocked_technologies,
        paldeck_unlocked_count,
        tribe_capture_count,
        position,
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
                Some(c) => Some(build_pal(instance_id, &c.object)),
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
                .filter_map(|(character_guid, instance_id)| {
                    let instance_id = guid_bytes_to_fguid(instance_id);
                    match world.find_character(instance_id) {
                        Some(c) => {
                            let is_player = pal_bool(&c.object, "IsPlayer").unwrap_or(false);
                            Some(GuildMember {
                                name: pal_str(&c.object, "NickName"),
                                species_id: pal_str(&c.object, "CharacterID"),
                                is_player,
                                player_uid: is_player.then(|| {
                                    guid_bytes_to_fguid(character_guid).to_string()
                                }),
                                level: pal_byte(&c.object, "Level"),
                            })
                        }
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
                guild_id: guid_bytes_to_fguid(&g.group_id).to_string(),
                name: g.group_name.clone(),
                member_count: g.member_handle_ids.len(),
                members,
            }
        })
        .collect()
}

/// The host's own `CharacterSaveParameterMap` group id: the character
/// carrying `IsPlayer` (this is always a single-player world's host), or --
/// if no such character resolved -- the first `Players/*.sav`'s own
/// character via its `IndividualId` join.
fn resolve_host_group_id(world: &World, players: &[Save]) -> Option<FGuid> {
    if let Some(host) = world
        .characters
        .values()
        .find(|c| pal_bool(&c.object, "IsPlayer") == Some(true))
    {
        return Some(guid_bytes_to_fguid(&host.group_id));
    }
    players
        .iter()
        .find_map(|player| {
            player_individual_instance_id(player)
                .and_then(|instance_id| world.find_character(instance_id))
        })
        .map(|host| guid_bytes_to_fguid(&host.group_id))
}

/// The overview's `guildName`: the Guild-type group the host player
/// actually belongs to, not an arbitrary `guilds.first()` pick (a world can
/// have more than one Guild-type entry). Falls back to `guilds.first()`
/// (with this comment marking why) when no host resolves at all, so the
/// overview still names *a* guild rather than going empty.
fn resolve_guild_name(world: &World, players: &[Save], guilds: &[Guild]) -> Option<String> {
    if let Some(host_group_id) = resolve_host_group_id(world, players) {
        let host_guild = world.groups.iter().find(|(group_id, group_type, _)| {
            *group_id == host_group_id && group_type == "EPalGroupType::Guild"
        });
        if let Some((_, _, g)) = host_guild {
            return Some(g.group_name.clone());
        }
    }
    guilds.first().map(|g| g.name.clone())
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
    /// Paths of every `RawData` entry (character/group/container-slot/
    /// dynamic-item) whose decode failed with `RawDataErrorKind::Unsupported`
    /// -- a signal this save's format revision has drifted past what this
    /// plugin's codecs recognize, not genuine corruption (see C2).
    pub unsupported_paths: Vec<String>,
    /// `true` when every one of `worldSaveData`'s decoded collections
    /// (characters, groups, character/item container slots, dynamic items)
    /// came back with zero successfully-decoded entries *and* at least one
    /// of them failed with `RawDataErrorKind::Unsupported` -- i.e. the
    /// world is functionally wholly undecodable due to format drift, not a
    /// partial/mixed degrade. The caller (`lib.rs`) escalates this to a
    /// hard `unsupported_version` error instead of emitting a near-empty
    /// "successful" result.
    pub critical_unsupported: bool,
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
            unsupported_paths: Vec::new(),
            critical_unsupported: false,
        };
    };

    let mut unsupported_paths = Vec::new();
    let groups = decode_groups(wsd, &mut warnings, &mut unsupported_paths);
    let char_containers = decode_char_containers(wsd, &mut warnings, &mut unsupported_paths);
    let item_containers = decode_item_containers(wsd, &mut warnings, &mut unsupported_paths);
    let dynamic_items = decode_dynamic_items(wsd, &mut warnings, &mut unsupported_paths);
    let bases = decode_bases(wsd, &mut warnings);
    let base_worker_containers = decode_base_worker_containers(wsd);
    let map_objects = decode_map_objects(wsd, &mut warnings);
    let workable_ids = decode_workable_ids(wsd);

    // Decode only the characters some section actually needs (see P7) --
    // this depends on containers and groups already being decoded above.
    let needed_characters = collect_needed_character_ids(players, &char_containers, &groups);
    let characters = decode_characters(
        wsd,
        &needed_characters,
        &mut warnings,
        &mut unsupported_paths,
    );

    // The world is functionally wholly undecodable due to format drift
    // (rather than a partial/mixed degrade) when nothing at all decoded
    // successfully anywhere, yet at least one entry failed specifically
    // because its property type isn't recognized (see C2 / BuildResult's
    // `critical_unsupported` doc).
    let total_decoded = characters.len()
        + groups.len()
        + char_containers.values().map(Vec::len).sum::<usize>()
        + item_containers.values().map(Vec::len).sum::<usize>()
        + dynamic_items.len();
    let critical_unsupported = total_decoded == 0 && !unsupported_paths.is_empty();

    let world = World {
        characters,
        groups,
        char_containers,
        item_containers,
        dynamic_items,
        bases,
        base_worker_containers,
        map_objects,
        workable_ids,
    };

    let mut result = assemble_sections(overview, &world, players, warnings);
    result.unsupported_paths = unsupported_paths;
    result.critical_unsupported = critical_unsupported;
    result
}

/// The second half of [`build_all`]: joins an already-decoded [`World`]
/// (plus each player's own `Save`) into every section. Split out so tests
/// can exercise this exact join logic against a directly-constructed
/// `World` -- e.g. one with a thousand synthetic storage pals -- without
/// needing real `RawData` bytes for each one.
fn assemble_sections(
    mut overview: Overview,
    world: &World,
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
    let mut bases = build_bases(world, &mut warnings);
    enforce_bases_budget(&mut bases, &mut warnings);
    let inventory = players
        .iter()
        .map(|p| build_player_inventory(p, world, &mut warnings))
        .collect();

    overview.guild_name = resolve_guild_name(world, players, &guild);
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
        // Set by build_all after this call returns -- assemble_sections has
        // no visibility into the World's raw decode counts or the
        // accumulated unsupported-path list (see build_all).
        unsupported_paths: Vec::new(),
        critical_unsupported: false,
    }
}

fn build_bases(world: &World, warnings: &mut Vec<String>) -> Vec<Base> {
    let mut bases: Vec<Base> = world
        .bases
        .iter()
        .map(|(id, base)| build_base(*id, base, world))
        .collect();
    let unresolved_guilds = bases.iter().filter(|base| base.guild_id.is_none()).count();
    if unresolved_guilds > 0 {
        warnings.push(format!(
            "{unresolved_guilds} base camp guild reference(s) did not match a decoded group; guildId emitted as null"
        ));
    }
    bases.sort_by(|a, b| a.base_id.cmp(&b.base_id));
    bases
}

fn build_base(id: FGuid, base: &rawdata::BaseCamp, world: &World) -> Base {
    let objects: Vec<_> = world
        .map_objects
        .iter()
        .filter(|object| object.base_id == id)
        .collect();
    let workers = base_worker_ids(id, world);
    let mut by_id: BTreeMap<&str, (usize, usize)> = BTreeMap::new();
    let mut totals: BTreeMap<String, i64> = BTreeMap::new();
    for object in objects {
        let entry = by_id.entry(&object.id).or_default();
        entry.0 += 1;
        if object
            .workee_id
            .is_some_and(|work| world.workable_ids.contains(&work))
        {
            entry.1 += 1;
        }
        for container_id in &object.item_container_ids {
            if let Some(items) = world.item_containers.get(container_id) {
                for item in items {
                    *totals.entry(item.static_id.clone()).or_default() += i64::from(item.count);
                }
            }
        }
    }
    Base {
        base_id: id.to_string(),
        guild_id: world
            .groups
            .iter()
            .find(|(_, _, group)| group.group_id == base.guild_id)
            .map(|(_, _, group)| guid_bytes_to_fguid(&group.group_id).to_string()),
        name: base.name.clone(),
        position: Position {
            x: base.position[0],
            y: base.position[1],
            z: base.position[2],
        },
        workers,
        // `unknown` is every object this build could source no signal for,
        // i.e. the ones `working` did not claim -- never both (see
        // [`BaseMapObjectFlags`]).
        map_objects: by_id
            .into_iter()
            .map(|(id, (count, working))| BaseMapObject {
                id: id.to_string(),
                count,
                flags: BaseMapObjectFlags {
                    powered: None,
                    working: Some(working),
                    damaged: None,
                    under_construction: None,
                    unknown: count - working,
                },
            })
            .collect(),
        items: BaseItems {
            items: sorted_base_items(totals),
            trimmed: None,
        },
    }
}

fn base_worker_ids(id: FGuid, world: &World) -> Vec<String> {
    world
        .base_worker_containers
        .get(&id)
        .and_then(|container_id| world.char_containers.get(container_id))
        .map(|slots| {
            slots
                .iter()
                .map(|slot| guid_bytes_to_fguid(&slot.instance_id).to_string())
                .collect()
        })
        .unwrap_or_default()
}

fn sorted_base_items(totals: BTreeMap<String, i64>) -> Vec<BaseItem> {
    let mut items: Vec<_> = totals
        .into_iter()
        .map(|(id, quantity)| BaseItem { id, quantity })
        .collect();
    items.sort_by(|a, b| b.quantity.cmp(&a.quantity).then_with(|| a.id.cmp(&b.id)));
    items
}

const BASES_SECTION_LIMIT: usize = 81_920;

fn enforce_bases_budget(bases: &mut [Base], warnings: &mut Vec<String>) {
    while serde_json::to_vec(&BasesSection {
        bases: bases.to_vec(),
        count: bases.len(),
    })
    .is_ok_and(|json| json.len() > BASES_SECTION_LIMIT)
    {
        let Some((base_index, item_index)) =
            bases.iter().enumerate().find_map(|(base_index, base)| {
                base.items
                    .items
                    .iter()
                    .enumerate()
                    .next_back()
                    .map(|(item_index, _)| (base_index, item_index))
            })
        else {
            warnings.push(format!(
                "bases section exceeds {BASES_SECTION_LIMIT} bytes after item trimming"
            ));
            return;
        };
        let item = bases[base_index].items.items.remove(item_index);
        let trimmed = bases[base_index]
            .items
            .trimmed
            .get_or_insert(BaseItemsTrimmed {
                types_omitted: 0,
                quantity_omitted: 0,
            });
        trimmed.types_omitted += 1;
        trimmed.quantity_omitted += item.quantity;
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
            bases: HashMap::new(),
            base_worker_containers: HashMap::new(),
            map_objects: Vec::new(),
            workable_ids: HashSet::new(),
        }
    }

    #[test]
    fn unmatched_base_guild_reference_emits_null_and_is_counted_in_warning() {
        let mut world = empty_world();
        let base_id = guid_bytes_to_fguid(&synthetic_guid_bytes(7, 0));
        world.bases.insert(
            base_id,
            rawdata::BaseCamp {
                base_id: synthetic_guid_bytes(7, 0),
                name: "Unmatched guild base".to_string(),
                position: [0.0; 3],
                guild_id: synthetic_guid_bytes(99, 0),
                trailing: Vec::new(),
            },
        );
        let mut warnings = Vec::new();

        let bases = build_bases(&world, &mut warnings);

        assert!(bases[0].guild_id.is_none());
        assert_eq!(warnings.len(), 1);
        assert!(warnings[0].contains("1 base camp guild reference"));
        assert!(warnings[0].contains("guildId emitted as null"));
    }

    fn pal_prop(name: &str, value: rawdata::PalValue) -> rawdata::PalProperty {
        rawdata::PalProperty {
            name: name.to_string(),
            type_name: String::new(),
            value,
        }
    }

    fn character_entry(object: Vec<rawdata::PalProperty>) -> CharacterSaveParameter {
        CharacterSaveParameter {
            object,
            unknown: [0u8; 4],
            group_id: [0u8; 16],
            trailing: Vec::new(),
        }
    }

    #[test]
    fn build_pal_emits_bare_values_from_qualified_and_bare_inputs() {
        let pal = build_pal(
            FGuid::new(1, 2, 3, 4),
            &[
                pal_prop(
                    "Gender",
                    rawdata::PalValue::Enum {
                        enum_type: "EPalGenderType".to_string(),
                        value: "EPalGenderType::Female".to_string(),
                    },
                ),
                pal_prop(
                    "PassiveSkillList",
                    rawdata::PalValue::ArrayStr(vec!["Rare".to_string()]),
                ),
                pal_prop(
                    "EquipWaza",
                    rawdata::PalValue::ArrayStr(vec!["EPalWazaID::AirCanon".to_string()]),
                ),
                pal_prop(
                    "CurrentWorkSuitability",
                    rawdata::PalValue::Enum {
                        enum_type: "EPalWorkSuitability".to_string(),
                        value: "EPalWorkSuitability::Deforest".to_string(),
                    },
                ),
            ],
        );

        assert_eq!(pal.sex.as_deref(), Some("Female"));
        assert_eq!(pal.passive_skills, ["Rare"]);
        assert_eq!(pal.equipped_skills, ["AirCanon"]);
        assert_eq!(pal.current_work_suitability.as_deref(), Some("Deforest"));
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

    /// A `CharacterSaveParameterMap` entry: key = `{PlayerUId, InstanceId}`
    /// (matching [`character_key`]'s shape), value = `{RawData: <bytes>}`.
    fn character_map_entry(instance_id: FGuid, raw_data: Vec<u8>) -> MapEntry {
        let mut key_props = Properties::default();
        key_props.insert("PlayerUId", guid_prop(FGuid::new(0, 0, 0, 0)));
        key_props.insert("InstanceId", guid_prop(instance_id));

        let mut value_props = Properties::default();
        value_props.insert(
            "RawData",
            Property::Array(ValueVec::Byte(ByteArray::Byte(raw_data))),
        );

        MapEntry {
            key: Property::Struct(StructValue::Struct(key_props)),
            value: Property::Struct(StructValue::Struct(value_props)),
        }
    }

    /// A `RawData` property list's wire-format `FString`: an `i32` length
    /// (including the trailing NUL it counts) followed by the string's
    /// ASCII bytes and that NUL -- matches `rawdata::reader`'s own
    /// `test_support::ascii_fstring` (private to that crate), duplicated
    /// here since this module builds its own synthetic `RawData` bytes.
    fn ascii_fstring_bytes(s: &str) -> Vec<u8> {
        let mut bytes = Vec::new();
        let len = (s.len() + 1) as i32;
        bytes.extend_from_slice(&len.to_le_bytes());
        bytes.extend_from_slice(s.as_bytes());
        bytes.push(0);
        bytes
    }

    /// A `RawData` property list whose first (and only) property has a type
    /// name (`"MapProperty"`) the `rawdata::properties` decoder doesn't
    /// recognize -- decoding this fails with `RawDataErrorKind::Unsupported`
    /// before ever reaching any wrapper-shape check (e.g.
    /// `decode_character_save_parameter`'s `"SaveParameter"` struct), so it
    /// exercises the C2 escalation path from any of this module's rawdata
    /// decode sites.
    fn unsupported_property_type_raw_data() -> Vec<u8> {
        let mut data = Vec::new();
        data.extend_from_slice(&ascii_fstring_bytes("Foo")); // property name
        data.extend_from_slice(&ascii_fstring_bytes("MapProperty")); // unrecognized type
        data.extend_from_slice(&0u32.to_le_bytes()); // size
        data.extend_from_slice(&0u32.to_le_bytes()); // array index
        data
    }

    #[test]
    fn decode_characters_unsupported_property_type_degrades_with_a_newer_format_warning_naming_the_path()
     {
        let instance_id = guid_bytes_to_fguid(&synthetic_guid_bytes(22, 0));
        let map = vec![character_map_entry(
            instance_id,
            unsupported_property_type_raw_data(),
        )];
        let mut wsd = Properties::default();
        wsd.insert("CharacterSaveParameterMap", Property::Map(map));

        let mut needed = HashSet::new();
        needed.insert(instance_id);

        let mut warnings = Vec::new();
        let mut unsupported = Vec::new();
        let characters = decode_characters(&wsd, &needed, &mut warnings, &mut unsupported);

        assert!(characters.is_empty());
        assert_eq!(warnings.len(), 1, "warnings: {warnings:?}");
        let warning = &warnings[0];
        assert!(warning.contains("newer"), "expected 'newer' in: {warning}");
        assert!(
            warning.contains("unsupported"),
            "expected 'unsupported' in: {warning}"
        );
        assert!(
            warning.contains("Foo"),
            "expected the failing property's path to be named: {warning}"
        );
        assert_eq!(unsupported.len(), 1);
        assert!(unsupported[0].contains("Foo"));
    }

    #[test]
    fn build_all_escalates_to_critical_unsupported_when_nothing_decodes_due_to_format_drift() {
        // The only character in CharacterSaveParameterMap is referenced by
        // the sole player's IndividualId (so it's in the P7 "needed" set,
        // and its decode is actually attempted), but its RawData carries an
        // unrecognized property type. No other collection has any entries
        // at all -- so nothing in the world decodes successfully anywhere,
        // and the one failure is format drift, not corruption.
        let instance_id = guid_bytes_to_fguid(&synthetic_guid_bytes(23, 0));

        let mut wsd = Properties::default();
        wsd.insert(
            "CharacterSaveParameterMap",
            Property::Map(vec![character_map_entry(
                instance_id,
                unsupported_property_type_raw_data(),
            )]),
        );
        let mut root = Properties::default();
        root.insert("worldSaveData", Property::Struct(StructValue::Struct(wsd)));
        let level = synthetic_save(root);

        let mut save_data = Properties::default();
        save_data.insert("PlayerUId", guid_prop(instance_id));
        save_data.insert("IndividualId", individual_id_prop(instance_id));
        let player = player_save(save_data);

        let result = build_all(&level, None, std::slice::from_ref(&player));

        assert!(
            result.critical_unsupported,
            "expected critical_unsupported when the only referenced character fails with \
             Unsupported and nothing else in the world decodes: warnings={:?}",
            result.warnings
        );
        assert_eq!(result.unsupported_paths.len(), 1);
        assert!(result.unsupported_paths[0].contains("Foo"));
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

    // --- decode_characters only decodes needed entries (P7) --------------

    #[test]
    fn decode_characters_only_attempts_entries_in_the_needed_set() {
        // Both entries carry empty RawData, which fails to decode (see
        // rawdata::character's "empty bytes error instead of decoding to a
        // default" contract) -- so a decode *attempt* always shows up as a
        // warning naming that entry's instance id. If the unreferenced
        // entry were ever attempted, it would add a second warning.
        let needed_id = guid_bytes_to_fguid(&synthetic_guid_bytes(20, 0));
        let unreferenced_id = guid_bytes_to_fguid(&synthetic_guid_bytes(21, 0));

        let map = vec![
            character_map_entry(needed_id, Vec::new()),
            character_map_entry(unreferenced_id, Vec::new()),
        ];
        let mut wsd = Properties::default();
        wsd.insert("CharacterSaveParameterMap", Property::Map(map));

        let mut needed = HashSet::new();
        needed.insert(needed_id);

        let mut warnings = Vec::new();
        let mut unsupported = Vec::new();
        let characters = decode_characters(&wsd, &needed, &mut warnings, &mut unsupported);

        assert!(
            characters.is_empty(),
            "the needed entry's empty RawData should fail to decode, not be inserted"
        );
        assert_eq!(
            warnings.len(),
            1,
            "exactly one warning (the needed entry's decode failure) -- the unreferenced \
             entry must never be attempted: {warnings:?}"
        );
        assert!(
            warnings[0].contains(&needed_id.to_string()),
            "the one warning should name the needed entry: {warnings:?}"
        );
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
    fn resolve_guild_name_picks_the_hosts_guild_not_the_first_one() {
        let host_instance_bytes = synthetic_guid_bytes(30, 0);
        let host_instance_id = guid_bytes_to_fguid(&host_instance_bytes);
        let host_group_bytes = synthetic_guid_bytes(31, 0);
        let other_group_bytes = synthetic_guid_bytes(32, 0);

        let mut world = empty_world();
        world.characters.insert(
            host_instance_id,
            CharacterSaveParameter {
                object: vec![
                    pal_prop("NickName", rawdata::PalValue::Str("Atmus".to_string())),
                    pal_prop("IsPlayer", rawdata::PalValue::Bool(true)),
                ],
                unknown: [0u8; 4],
                group_id: host_group_bytes,
                trailing: Vec::new(),
            },
        );

        // The *other* guild is inserted first, so a plain `guilds.first()`
        // pick (the old, arbitrary behavior) would return its name instead
        // of the host's.
        world.groups.push((
            guid_bytes_to_fguid(&other_group_bytes),
            "EPalGroupType::Guild".to_string(),
            rawdata::GroupSaveData {
                group_id: other_group_bytes,
                group_name: "Other Guild".to_string(),
                member_handle_ids: Vec::new(),
                trailing: Vec::new(),
            },
        ));
        world.groups.push((
            guid_bytes_to_fguid(&host_group_bytes),
            "EPalGroupType::Guild".to_string(),
            rawdata::GroupSaveData {
                group_id: host_group_bytes,
                group_name: "Host Guild".to_string(),
                member_handle_ids: vec![([0u8; 16], host_instance_bytes)],
                trailing: Vec::new(),
            },
        ));

        let mut warnings = Vec::new();
        let guilds = build_guilds(&world, &mut warnings);
        assert_eq!(guilds.len(), 2, "sanity: both guilds should build");
        assert_eq!(
            guilds[0].name, "Other Guild",
            "sanity: guilds[0] is the non-host guild, proving a plain .first() pick would choose wrong"
        );

        let name = resolve_guild_name(&world, &[], &guilds);
        assert_eq!(name, Some("Host Guild".to_string()));
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
            section.position.is_none(),
            "a missing LastTransform must not fabricate a zero position"
        );
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

    /// One synthetic player's full contribution to the P6 multi-player
    /// scale test below: a distinct player `Save` plus its own 3 party
    /// pals, 1,000 storage pals (each at the same full detail as
    /// [`synthetic_pal_object`]), and six populated item containers.
    struct SyntheticPlayerWorld {
        player_id_bytes: [u8; 16],
        player_save: Save,
        characters: Vec<(FGuid, CharacterSaveParameter)>,
        char_containers: Vec<(FGuid, Vec<CharacterContainerSlot>)>,
        item_containers: Vec<(FGuid, Vec<ItemSlot>)>,
    }

    /// Builds one [`SyntheticPlayerWorld`], every synthetic guid tagged off
    /// `player_tag` (and small fixed offsets from it) so each player's ids
    /// stay fully distinct from every other player's and from every other
    /// test in this module.
    fn build_synthetic_player_world(player_tag: u8) -> SyntheticPlayerWorld {
        let player_id_bytes = synthetic_guid_bytes(player_tag, 0);
        let player_instance_id = guid_bytes_to_fguid(&player_id_bytes);

        let mut characters = vec![(
            player_instance_id,
            character_entry(vec![
                pal_prop(
                    "NickName",
                    rawdata::PalValue::Str(format!("Player{player_tag}")),
                ),
                pal_prop("Level", rawdata::PalValue::ByteRaw(9)),
                pal_prop("IsPlayer", rawdata::PalValue::Bool(true)),
            ]),
        )];

        let party_id_bytes: Vec<[u8; 16]> = (0..3)
            .map(|k| synthetic_guid_bytes(player_tag + 1, k))
            .collect();
        for (k, bytes) in party_id_bytes.iter().enumerate() {
            characters.push((
                guid_bytes_to_fguid(bytes),
                character_entry(synthetic_pal_object(k)),
            ));
        }

        let storage_id_bytes: Vec<[u8; 16]> = (0..1000)
            .map(|k| synthetic_guid_bytes(player_tag + 2, k))
            .collect();
        for (k, bytes) in storage_id_bytes.iter().enumerate() {
            characters.push((
                guid_bytes_to_fguid(bytes),
                character_entry(synthetic_pal_object(k)),
            ));
        }

        let party_container_id = guid_bytes_to_fguid(&synthetic_guid_bytes(player_tag + 3, 0));
        let storage_container_id = guid_bytes_to_fguid(&synthetic_guid_bytes(player_tag + 3, 1));
        let char_containers = vec![
            (
                party_container_id,
                party_id_bytes
                    .iter()
                    .map(|bytes| CharacterContainerSlot {
                        player_uid: player_id_bytes,
                        instance_id: *bytes,
                        trailing: Vec::new(),
                    })
                    .collect(),
            ),
            (
                storage_container_id,
                storage_id_bytes
                    .iter()
                    .map(|bytes| CharacterContainerSlot {
                        player_uid: player_id_bytes,
                        instance_id: *bytes,
                        trailing: Vec::new(),
                    })
                    .collect(),
            ),
        ];

        let item_container_ids: Vec<FGuid> = (0..INVENTORY_ROLES.len() as u32)
            .map(|k| guid_bytes_to_fguid(&synthetic_guid_bytes(player_tag + 4, k)))
            .collect();
        let item_containers: Vec<(FGuid, Vec<ItemSlot>)> = item_container_ids
            .iter()
            .map(|id| {
                (
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
                )
            })
            .collect();

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

        SyntheticPlayerWorld {
            player_id_bytes,
            player_save: player_save(save_data),
            characters,
            char_containers,
            item_containers,
        }
    }

    /// P6: measures the actual encoded envelope for 4 players x 1,000
    /// storage pals each (4,000 storage pals total, every one at the same
    /// full per-pal detail as the single-player scale test below) through
    /// the exact `assemble_sections` -> `build_sections_map` ->
    /// `ndjson::encode_result` chain `run()` uses.
    ///
    /// A capped/truncated pal list for oversized worlds was proposed and
    /// explicitly REJECTED (epic anti-pattern: no capped detail -- owner
    /// decision, recorded here verbatim rather than implemented). This test
    /// exists to document the measured envelope instead of asserting a
    /// hand-built one that could drift from what the daemon actually
    /// receives: on a real run (2026-07-31, this exact synthetic world),
    /// the encoded result line measured 1,808,779 bytes (~1.72 MiB) -- 86%
    /// of the daemon's 2 MiB ndjson line budget for only 4 players. That
    /// leaves little headroom: a 5th player at this density, or any save
    /// with denser per-pal data than this synthetic fixture, plausibly
    /// crosses the daemon's 2 MiB line-budget cliff. Should that happen on
    /// a real save, the daemon rejecting that oversized line is the
    /// intended, designed outcome -- nothing in this plugin should ever cap
    /// pal count or per-pal detail to force a result under budget.
    #[test]
    fn scale_full_result_with_4_players_and_4000_storage_pals_measures_the_encoded_size_cliff() {
        let players_data: Vec<SyntheticPlayerWorld> = (0..4u8)
            .map(|i| build_synthetic_player_world(60 + i * 10))
            .collect();

        let mut characters = HashMap::new();
        let mut char_containers = HashMap::new();
        let mut item_containers = HashMap::new();
        let mut players = Vec::new();
        let mut member_handle_ids = Vec::new();

        for p in players_data {
            member_handle_ids.push((p.player_id_bytes, p.player_id_bytes));
            characters.extend(p.characters);
            char_containers.extend(p.char_containers);
            item_containers.extend(p.item_containers);
            players.push(p.player_save);
        }

        let group_id = guid_bytes_to_fguid(&synthetic_guid_bytes(199, 0));
        let groups = vec![(
            group_id,
            "EPalGroupType::Guild".to_string(),
            rawdata::GroupSaveData {
                group_id: synthetic_guid_bytes(199, 0),
                group_name: "Multi-Player Guild".to_string(),
                member_handle_ids,
                trailing: Vec::new(),
            },
        )];

        let base_id = guid_bytes_to_fguid(&synthetic_guid_bytes(7, 0));
        let mut bases = HashMap::new();
        bases.insert(
            base_id,
            rawdata::BaseCamp {
                base_id: synthetic_guid_bytes(7, 0),
                name: "Synthetic base".to_string(),
                position: [0.0; 3],
                guild_id: synthetic_guid_bytes(199, 0),
                trailing: Vec::new(),
            },
        );
        let world = World {
            characters,
            groups,
            char_containers,
            item_containers,
            dynamic_items: HashMap::new(),
            bases,
            base_worker_containers: HashMap::new(),
            map_objects: Vec::new(),
            workable_ids: HashSet::new(),
        };

        let overview = Overview {
            world_name: Some("Palpagos Islands (multi-player scale test)".to_string()),
            host_player_name: Some("Player60".to_string()),
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
            player_count: 4,
        };

        let built = assemble_sections(overview, &world, &players, Vec::new());
        assert!(
            built.warnings.is_empty(),
            "expected a clean synthetic world with no degrade warnings: {:?}",
            built.warnings
        );
        assert_eq!(built.pals_party.len(), 4 * 3);
        assert_eq!(built.pals_storage.len(), 4 * 1000);
        assert_eq!(built.guild.len(), 1);
        assert_eq!(built.guild[0].member_count, 4);

        let sections_map = crate::build_sections_map(built);
        let identity = crate::ndjson::Identity {
            save_name: "Palpagos Islands (multi-player scale test)".to_string(),
            game_id: "palworld".to_string(),
            extra: Some(serde_json::json!({ "worldId": "MULTI-PLAYER-SCALE-TEST" })),
        };
        let encoded = crate::ndjson::encode_result(
            identity,
            "Palpagos Islands (multi-player scale test)".to_string(),
            sections_map,
        );

        // Documents the measured envelope (1,808,779 bytes / ~1.72 MiB on a
        // real run) rather than asserting an exact byte count that would be
        // brittle against incidental serialization changes: bounded to a
        // window around that measurement, still comfortably under the
        // daemon's 2 MiB budget for exactly 4 players at this density, but
        // consuming the large majority of it (see the doc comment above).
        let mib = encoded.len() as f64 / (1024.0 * 1024.0);
        assert!(
            (1_600_000..2 * 1024 * 1024).contains(&encoded.len()),
            "expected the measured envelope for this 4-player, 4,000-storage-pal world to stay \
             within its documented range (roughly 1.6 MiB to the daemon's 2 MiB budget), just \
             under budget but consuming most of it: measured {mib:.2} MiB ({} bytes)",
            encoded.len()
        );
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

        let base_id = guid_bytes_to_fguid(&synthetic_guid_bytes(7, 0));
        let mut bases = HashMap::new();
        bases.insert(
            base_id,
            rawdata::BaseCamp {
                base_id: synthetic_guid_bytes(7, 0),
                name: "Synthetic base".to_string(),
                position: [0.0; 3],
                guild_id: synthetic_guid_bytes(8, 0),
                trailing: Vec::new(),
            },
        );
        let world = World {
            characters,
            groups,
            char_containers,
            item_containers,
            dynamic_items: HashMap::new(),
            bases,
            base_worker_containers: HashMap::new(),
            map_objects: Vec::new(),
            workable_ids: HashSet::new(),
        };

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

        let built = assemble_sections(overview, &world, std::slice::from_ref(&player), Vec::new());
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
