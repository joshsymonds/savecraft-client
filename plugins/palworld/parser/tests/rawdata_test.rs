//! Real-fixture, byte-verified tests for the `rawdata` module's five
//! `RawData` codecs.
//!
//! This crate is `[[bin]]`-only (no `[lib]` target), and `rawdata` is
//! gated behind `#[cfg(test)]` in `main.rs` until a follow-on task wires
//! section builders to it (see `src/rawdata.rs`'s module doc) -- so there
//! is no `palworld_parser::` path to import from here. Instead this file
//! pulls in the exact same source `main.rs` does via `#[path]`, giving it
//! access to `rawdata`'s decoders and the existing `gvas::decode()`
//! pipeline (PlM1 container -> Kraken -> GVAS) needed to reach real
//! `RawData` bytes from `testdata/.../Level.sav`, without any Cargo.toml
//! changes. `container`/`decompress` are pulled in too because `gvas.rs`
//! itself references them via `crate::` paths, which resolve against
//! *this* crate once included.

#[path = "../src/container.rs"]
mod container;
#[path = "../src/decompress.rs"]
mod decompress;
#[path = "../src/gvas.rs"]
mod gvas;
#[path = "../src/rawdata.rs"]
mod rawdata;

use rawdata::{
    DynamicItemKind, PalValue, decode_character_container_slot, decode_character_save_parameter,
    decode_dynamic_item, decode_group_save_data, decode_item_container_permission,
};
use std::path::PathBuf;
use uesave::{ByteArray, FGuid, MapEntry, Properties, Property, Save, StructValue, ValueVec};

const WORLD_ID: &str = "1FCE97C34D214643B96A23A20A9E27D1";

fn fixture(name: &str) -> Vec<u8> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(WORLD_ID)
        .join(name);
    std::fs::read(&path).unwrap_or_else(|e| panic!("reading fixture {path:?}: {e}"))
}

fn level_save() -> Save {
    gvas::decode(&fixture("Level.sav")).expect("decode Level.sav")
}

fn find<'a>(props: &'a Properties, name: &str) -> &'a Property {
    let (_, value) = props
        .0
        .iter()
        .find(|(k, _)| k.1 == name)
        .unwrap_or_else(|| panic!("missing property {name}"));
    value
}

fn world_save_data(save: &Save) -> &Properties {
    match find(&save.root.properties, "worldSaveData") {
        Property::Struct(StructValue::Struct(props)) => props,
        other => panic!("worldSaveData is not a nested struct: {other:?}"),
    }
}

fn map_entries<'a>(props: &'a Properties, name: &str) -> &'a Vec<MapEntry> {
    match find(props, name) {
        Property::Map(entries) => entries,
        other => panic!("{name} is not a Map: {other:?}"),
    }
}

fn byte_array<'a>(props: &'a Properties, name: &str) -> &'a [u8] {
    match find(props, name) {
        Property::Array(ValueVec::Byte(ByteArray::Byte(bytes))) => bytes,
        other => panic!("{name} is not a Byte array: {other:?}"),
    }
}

fn nested(prop: &Property) -> &Properties {
    match prop {
        Property::Struct(StructValue::Struct(props)) => props,
        other => panic!("not a nested struct: {other:?}"),
    }
}

fn bytes_to_fguid(b: &[u8; 16]) -> FGuid {
    FGuid::new(
        u32::from_le_bytes(b[0..4].try_into().unwrap()),
        u32::from_le_bytes(b[4..8].try_into().unwrap()),
        u32::from_le_bytes(b[8..12].try_into().unwrap()),
        u32::from_le_bytes(b[12..16].try_into().unwrap()),
    )
}

fn pal_value<'a>(props: &'a [rawdata::PalProperty], name: &str) -> Option<&'a PalValue> {
    props.iter().find(|p| p.name == name).map(|p| &p.value)
}

// --- CharacterSaveParameterMap ------------------------------------------

#[test]
fn character_save_parameter_decodes_the_host_player_atmus_at_level_9() {
    let save = level_save();
    let entries = map_entries(world_save_data(&save), "CharacterSaveParameterMap");

    let atmus = entries
        .iter()
        .find_map(|e| {
            let raw = byte_array(nested(&e.value), "RawData");
            let decoded = decode_character_save_parameter(raw).expect("decode character");
            match pal_value(&decoded.object, "NickName") {
                Some(PalValue::Str(name)) if name == "Atmus" => Some(decoded),
                _ => None,
            }
        })
        .expect("an Atmus entry in CharacterSaveParameterMap");

    assert_eq!(
        pal_value(&atmus.object, "IsPlayer"),
        Some(&PalValue::Bool(true))
    );
    assert_eq!(
        pal_value(&atmus.object, "Level"),
        Some(&PalValue::ByteRaw(9))
    );

    // Cross-codec pin: Atmus's group id must be a real GroupSaveDataMap
    // key, of type Guild (the default single-player guild).
    let group_entries = map_entries(world_save_data(&save), "GroupSaveDataMap");
    let matching_group = group_entries.iter().find(|g| match &g.key {
        Property::Struct(StructValue::Guid(key)) => *key == bytes_to_fguid(&atmus.group_id),
        _ => false,
    });
    assert!(
        matching_group.is_some(),
        "Atmus's group_id should match a real GroupSaveDataMap key"
    );
    let group_type = nested(&matching_group.unwrap().value)
        .0
        .iter()
        .find(|(k, _)| k.1 == "GroupType");
    assert!(matches!(
        group_type,
        Some((_, Property::Enum(t))) if t == "EPalGroupType::Guild"
    ));
}

#[test]
fn character_save_parameter_decodes_an_owned_pal_species_and_level() {
    let save = level_save();
    let entries = map_entries(world_save_data(&save), "CharacterSaveParameterMap");

    // Entry 0 is a real owned pal (not the host player) in this fixture.
    let raw = byte_array(nested(&entries[0].value), "RawData");
    let decoded = decode_character_save_parameter(raw).expect("decode character");

    assert_eq!(
        pal_value(&decoded.object, "CharacterID"),
        Some(&PalValue::Str("ChickenPal".to_string()))
    );
    assert_eq!(
        pal_value(&decoded.object, "Level"),
        Some(&PalValue::ByteRaw(5))
    );
    assert_eq!(
        pal_value(&decoded.object, "OwnerPlayerUId"),
        Some(&PalValue::Guid([
            0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0
        ]))
    );
}

#[test]
fn character_save_parameter_all_37_entries_have_a_4_byte_trailing_tail() {
    // Pins the module doc's "consistent 24-byte trailer" claim (4 unknown
    // + 16 group_id + 4 more this build appends) across every real entry,
    // not just the two spot-checked above.
    let save = level_save();
    let entries = map_entries(world_save_data(&save), "CharacterSaveParameterMap");
    assert_eq!(entries.len(), 37);

    for e in entries {
        let raw = byte_array(nested(&e.value), "RawData");
        let decoded = decode_character_save_parameter(raw).expect("decode character");
        assert_eq!(
            decoded.trailing.len(),
            4,
            "every entry's post-group_id trailing tail should be 4 bytes"
        );
    }
}

#[test]
fn character_save_parameter_truncated_before_group_id_errors_not_panics() {
    let save = level_save();
    let entries = map_entries(world_save_data(&save), "CharacterSaveParameterMap");
    let raw = byte_array(nested(&entries[0].value), "RawData");

    // Cut off 10 bytes before the end -- lands inside the trailer, after
    // the property object has already parsed cleanly.
    let truncated = &raw[..raw.len() - 10];
    let err = decode_character_save_parameter(truncated).unwrap_err();
    assert_eq!(err.path, "CharacterSaveParameterMap.Value.RawData.trailer");
    assert!(err.message.contains("truncated"));
}

// --- CharacterContainerSaveData (per-slot RawData) ----------------------

fn character_container_slots(save: &Save, entry_index: usize) -> Vec<Vec<u8>> {
    let entries = map_entries(world_save_data(save), "CharacterContainerSaveData");
    match find(nested(&entries[entry_index].value), "Slots") {
        Property::Array(ValueVec::Struct(slots)) => slots
            .iter()
            .map(|s| match s {
                StructValue::Struct(props) => byte_array(props, "RawData").to_vec(),
                other => panic!("slot is not a nested struct: {other:?}"),
            })
            .collect(),
        other => panic!("Slots is not an array of structs: {other:?}"),
    }
}

#[test]
fn character_container_slot_decodes_occupant_and_cross_references_a_real_pal() {
    let save = level_save();
    let slots = character_container_slots(&save, 1);
    assert!(!slots.is_empty(), "expected occupied slots in this fixture");

    let decoded = decode_character_container_slot(&slots[0])
        .expect("decode slot")
        .expect("slot should be occupied");

    // Every occupied slot in this fixture belongs to the host player.
    assert_eq!(bytes_to_fguid(&decoded.player_uid), FGuid::new(0, 0, 0, 1));

    // The slot's instance_id must reference a real character in
    // CharacterSaveParameterMap.
    let character_entries = map_entries(world_save_data(&save), "CharacterSaveParameterMap");
    let referenced = character_entries.iter().any(|e| match &e.key {
        Property::Struct(StructValue::Struct(key_props)) => {
            matches!(
                key_props.0.iter().find(|(k, _)| k.1 == "InstanceId"),
                Some((_, Property::Struct(StructValue::Guid(g)))) if *g == bytes_to_fguid(&decoded.instance_id)
            )
        }
        _ => false,
    });
    assert!(
        referenced,
        "container slot's instance_id should match a real CharacterSaveParameterMap key"
    );
}

#[test]
fn character_container_slot_every_occupied_slot_has_a_6_byte_trailing_tail() {
    // Pins the module doc's "38 bytes each -- 5 more than [upstream's] 33"
    // claim: player_uid(16) + instance_id(16) = 32 decoded bytes, so the
    // remaining trailing tail (permission_tribe_id + this build's padding)
    // should be a consistent 6 bytes across every occupied slot.
    let save = level_save();
    let slots = character_container_slots(&save, 1);
    let occupied: Vec<_> = slots
        .iter()
        .filter_map(|s| decode_character_container_slot(s).expect("decode slot"))
        .collect();
    assert!(
        !occupied.is_empty(),
        "expected occupied slots in this fixture"
    );

    for slot in &occupied {
        assert_eq!(slot.trailing.len(), 6);
    }
}

#[test]
fn character_container_slot_truncated_instance_id_errors_not_panics() {
    let save = level_save();
    let slots = character_container_slots(&save, 1);
    let truncated = &slots[0][..20]; // full player_uid, partial instance_id

    let err = decode_character_container_slot(truncated).unwrap_err();
    assert_eq!(
        err.path,
        "CharacterContainerSaveData.Value.Slots.Slots.RawData.instance_id"
    );
    assert!(err.message.contains("truncated"));
}

// --- GroupSaveDataMap -----------------------------------------------------

#[test]
fn group_save_data_decodes_the_hosts_guild() {
    let save = level_save();
    let entries = map_entries(world_save_data(&save), "GroupSaveDataMap");

    let (key, guild) = entries
        .iter()
        .find_map(|e| {
            let group_type = nested(&e.value).0.iter().find(|(k, _)| k.1 == "GroupType");
            match (&e.key, group_type) {
                (Property::Struct(StructValue::Guid(key)), Some((_, Property::Enum(t))))
                    if t == "EPalGroupType::Guild" =>
                {
                    Some((*key, e))
                }
                _ => None,
            }
        })
        .expect("a Guild-type entry in GroupSaveDataMap");

    let raw = byte_array(nested(&guild.value), "RawData");
    let decoded = decode_group_save_data(raw).expect("decode group");

    assert_eq!(bytes_to_fguid(&decoded.group_id), key);
    assert_eq!(decoded.group_name, "00000000000000000000000000000001");
    assert_eq!(decoded.member_handle_ids.len(), 37);
    // Every member entry's first guid is the host player -- this is a
    // single-player save with one real player and every pal filed under
    // them.
    let host = FGuid::new(0, 0, 0, 1);
    assert!(
        decoded
            .member_handle_ids
            .iter()
            .all(|(guid, _)| bytes_to_fguid(guid) == host),
        "every guild member's owning player should be the host"
    );
}

#[test]
fn group_save_data_truncated_handle_ids_errors_not_panics() {
    let save = level_save();
    let entries = map_entries(world_save_data(&save), "GroupSaveDataMap");
    // Entry 1 has a real, non-empty roster (27 members in this fixture) --
    // cut off partway into it, after the declared count but before all the
    // guid pairs it promises. Derive the roster's start from a successful
    // decode (group_id + group_name + count precede it, and each member is
    // 32 bytes) rather than assuming a fixed offset -- group_name's wire
    // length varies per entry.
    let raw = byte_array(nested(&entries[1].value), "RawData");
    let decoded = decode_group_save_data(raw).expect("decode group");
    assert!(
        !decoded.member_handle_ids.is_empty(),
        "expected a non-empty roster in this fixture"
    );
    let roster_bytes = decoded.member_handle_ids.len() * 32;
    let roster_start = raw.len() - roster_bytes - decoded.trailing.len();
    let truncated = &raw[..roster_start + 10];

    let err = decode_group_save_data(truncated).unwrap_err();
    assert_eq!(
        err.path,
        "GroupSaveDataMap.Value.RawData.individual_character_handle_ids"
    );
    // Caught up front by the count/remaining-bytes check, not a per-element
    // truncation once inside the loop.
    assert!(err.message.contains("exceeds"));
}

// --- ItemContainerSaveData (permission RawData) --------------------------

fn item_container_raw_data_blobs(save: &Save) -> Vec<Vec<u8>> {
    let entries = map_entries(world_save_data(save), "ItemContainerSaveData");
    entries
        .iter()
        .map(|e| byte_array(nested(&e.value), "RawData").to_vec())
        .collect()
}

#[test]
fn item_container_permission_decodes_a_real_populated_container() {
    let save = level_save();
    let blobs = item_container_raw_data_blobs(&save);

    let populated = blobs
        .iter()
        .find(|raw| raw.iter().any(|&b| b != 0))
        .expect("a container with non-zero RawData in this fixture");

    let decoded = decode_item_container_permission(populated)
        .expect("decode container")
        .expect("non-empty bytes should decode to Some");

    assert_eq!(decoded.type_a, vec![9]);
    assert!(decoded.type_b.is_empty());
    assert!(decoded.item_static_ids.is_empty());
    // 3 array headers (4 + 1 element + 4 + 4 = 13 bytes) leave exactly 8
    // bytes of build-specific padding out of this container's 21-byte
    // RawData.
    assert_eq!(decoded.trailing, vec![0u8; 8]);
}

#[test]
fn item_container_permission_all_zero_blob_decodes_to_empty_arrays() {
    let save = level_save();
    let blobs = item_container_raw_data_blobs(&save);
    // Every all-zero container in this fixture still carries 20 zero
    // bytes (three empty-count arrays + padding), not a zero-length
    // blob -- confirm that decodes to Some with empty arrays, matching
    // upstream's guard being about byte length, not content.
    let all_zero = blobs
        .iter()
        .find(|raw| raw.iter().all(|&b| b == 0) && !raw.is_empty())
        .expect("an all-zero-content container in this fixture");
    let decoded = decode_item_container_permission(all_zero).unwrap().unwrap();
    assert!(decoded.type_a.is_empty());
    assert!(decoded.type_b.is_empty());
    assert!(decoded.item_static_ids.is_empty());
}

#[test]
fn item_container_permission_truncated_type_a_errors_not_panics() {
    let save = level_save();
    let blobs = item_container_raw_data_blobs(&save);
    let populated = blobs
        .iter()
        .find(|raw| raw.iter().any(|&b| b != 0))
        .expect("a populated container");

    let truncated = &populated[..2]; // claims a count, no element bytes
    let err = decode_item_container_permission(truncated).unwrap_err();
    assert_eq!(
        err.path,
        "ItemContainerSaveData.Value.RawData.permission.type_a"
    );
    assert!(err.message.contains("truncated"));
}

// --- DynamicItemSaveData ---------------------------------------------------

fn dynamic_item_raw_data_blobs(save: &Save) -> Vec<Vec<u8>> {
    match find(world_save_data(save), "DynamicItemSaveData") {
        Property::Array(ValueVec::Struct(items)) => items
            .iter()
            .map(|item| match item {
                StructValue::Struct(props) => byte_array(props, "RawData").to_vec(),
                other => panic!("dynamic item is not a nested struct: {other:?}"),
            })
            .collect(),
        other => panic!("DynamicItemSaveData is not an array of structs: {other:?}"),
    }
}

#[test]
fn dynamic_item_decodes_real_unhatched_eggs() {
    let save = level_save();
    let blobs = dynamic_item_raw_data_blobs(&save);

    let first = decode_dynamic_item(&blobs[0]).unwrap().unwrap();
    assert_eq!(first.id.static_id, "PalEgg_Fire_01");
    assert_eq!(first.unknown, [0u8; 4]);
    match first.kind {
        DynamicItemKind::Egg { character_id, .. } => assert_eq!(character_id, "Monkey_Fire"),
        other => panic!("expected Egg, got {other:?}"),
    }

    let third = decode_dynamic_item(&blobs[2]).unwrap().unwrap();
    assert_eq!(third.id.static_id, "PalEgg_Water_01");
    assert_eq!(third.unknown, [0u8; 4]);
    match third.kind {
        DynamicItemKind::Egg { character_id, .. } => assert_eq!(character_id, "Penguin"),
        other => panic!("expected Egg, got {other:?}"),
    }

    // Both eggs' trailing tail (unknown bytes + owner guid + this build's
    // padding, following the egg's empty property object) should be the
    // same length in this fixture.
    assert_eq!(first.trailing.len(), third.trailing.len());
    assert_eq!(first.trailing.len(), 28);
}

#[test]
fn dynamic_item_truncated_id_errors_not_panics() {
    let save = level_save();
    let blobs = dynamic_item_raw_data_blobs(&save);

    let truncated = &blobs[0][..10]; // not even a full created_world_id guid
    let err = decode_dynamic_item(truncated).unwrap_err();
    assert_eq!(err.path, "DynamicItemSaveData.RawData.id");
    assert!(err.message.contains("truncated"));
}

#[test]
fn dynamic_item_truncated_egg_on_a_real_palegg_blob_errors_not_falls_back_to_other() {
    let save = level_save();
    let blobs = dynamic_item_raw_data_blobs(&save);

    // blobs[0] is a real "PalEgg_Fire_01" egg (see
    // dynamic_item_decodes_real_unhatched_eggs); cut it short partway into
    // the egg-shaped payload, after `id` and the unknown field have
    // already parsed cleanly. Before this fix this returned
    // `Ok(DynamicItemKind::Other)` instead of propagating the failure.
    let truncated = &blobs[0][..60];
    let err = decode_dynamic_item(truncated).unwrap_err();
    assert_eq!(err.path, "DynamicItemSaveData.RawData.egg");
}
