//! Fixture-pinned proof of the four fields needed by parser v1.1.0.
//!
//! These helpers deliberately use only the crate's existing public GVAS and
//! RawData decoders. They mirror the joins in `sections.rs` without changing
//! production output, so this test remains a format-location specification.

use palworld_parser::rawdata::{PalProperty, PalValue};
use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use uesave::{ByteArray, FGuid, MapEntry, Properties, Property, StructValue, ValueVec};

const FIXTURES: [(&str, [f64; 3]); 2] = [
    (
        "1FCE97C34D214643B96A23A20A9E27D1",
        [-346_946.959_174_043_73, 191_651.847_912_949_07, -211.404_500_183_231_1],
    ),
    (
        "live-20260731",
        [-351_879.202_569_715_74, 201_841.419_850_038_3, 1_731.043_340_878_700_7],
    ),
];
const PLAYER_FILE: &str = "Players/00000000000000000000000000000001.sav";
const HOST_UID: &str = "00000000-0000-0000-0000-000000000001";

fn fixture_path(fixture: &str, relative: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(fixture)
        .join(relative)
}

fn decode(path: &Path) -> uesave::Save {
    let bytes = std::fs::read(path).unwrap_or_else(|e| panic!("reading {path:?}: {e}"));
    palworld_parser::gvas::decode(bytes)
        .unwrap_or_else(|e| panic!("decoding {path:?}: {e}"))
}

fn property<'a>(properties: &'a Properties, name: &str) -> &'a Property {
    &properties
        .0
        .iter()
        .find(|(key, _)| key.1 == name)
        .unwrap_or_else(|| panic!("missing property {name}"))
        .1
}

fn properties(value: &Property) -> &Properties {
    match value {
        Property::Struct(StructValue::Struct(properties)) => properties,
        other => panic!("expected nested properties, got {other:?}"),
    }
}

fn guid(value: &Property) -> FGuid {
    match value {
        Property::Struct(StructValue::Guid(guid)) => *guid,
        other => panic!("expected GUID, got {other:?}"),
    }
}

fn wrapped_guid(value: &Property, field: &str) -> FGuid {
    guid(property(properties(value), field))
}

fn map(value: &Property) -> &[MapEntry] {
    match value {
        Property::Map(entries) => entries,
        other => panic!("expected map, got {other:?}"),
    }
}

fn structs(value: &Property) -> &[StructValue] {
    match value {
        Property::Array(ValueVec::Struct(values)) => values,
        other => panic!("expected struct array, got {other:?}"),
    }
}

fn bytes(value: &Property) -> &[u8] {
    match value {
        Property::Array(ValueVec::Byte(ByteArray::Byte(bytes))) => bytes,
        other => panic!("expected byte array, got {other:?}"),
    }
}

fn guid_from_raw(bytes: &[u8; 16]) -> FGuid {
    FGuid::new(
        u32::from_le_bytes(bytes[0..4].try_into().unwrap()),
        u32::from_le_bytes(bytes[4..8].try_into().unwrap()),
        u32::from_le_bytes(bytes[8..12].try_into().unwrap()),
        u32::from_le_bytes(bytes[12..16].try_into().unwrap()),
    )
}

fn pal_field<'a>(object: &'a [PalProperty], name: &str) -> &'a PalValue {
    &object
        .iter()
        .find(|property| property.name == name)
        .unwrap_or_else(|| panic!("missing pal field {name}"))
        .value
}

fn container_slots(world: &Properties, container_id: FGuid) -> Vec<(FGuid, FGuid)> {
    let entry = map(property(world, "CharacterContainerSaveData"))
        .iter()
        .find(|entry| wrapped_guid(&entry.key, "ID") == container_id)
        .unwrap_or_else(|| panic!("missing character container {container_id}"));
    let value = properties(&entry.value);

    structs(property(value, "Slots"))
        .iter()
        .filter_map(|slot| {
            let StructValue::Struct(slot) = slot else {
                panic!("expected slot struct, got {slot:?}");
            };
            palworld_parser::rawdata::decode_character_container_slot(bytes(property(
                slot, "RawData",
            )))
            .expect("slot RawData decodes")
            .map(|slot| {
                (
                    guid_from_raw(&slot.player_uid),
                    guid_from_raw(&slot.instance_id),
                )
            })
        })
        .collect()
}

fn character_map(world: &Properties) -> HashMap<FGuid, Vec<PalProperty>> {
    map(property(world, "CharacterSaveParameterMap"))
        .iter()
        .map(|entry| {
            let key = properties(&entry.key);
            let instance_id = guid(property(key, "InstanceId"));
            let value = properties(&entry.value);
            let character = palworld_parser::rawdata::decode_character_save_parameter(bytes(
                property(value, "RawData"),
            ))
            .expect("character RawData decodes");
            (instance_id, character.object)
        })
        .collect()
}

#[test]
fn player_last_transform_translation_is_a_fixture_pinned_ue_vector() {
    for (fixture, expected) in FIXTURES {
        let player = decode(&fixture_path(fixture, PLAYER_FILE));
        let save_data = properties(property(&player.root.properties, "SaveData"));
        assert_eq!(guid(property(save_data, "PlayerUId")).to_string(), HOST_UID);
        let transform = properties(property(save_data, "LastTransform"));
        let actual = match property(transform, "Translation") {
            Property::Struct(StructValue::Vector(vector)) => [vector.x.0, vector.y.0, vector.z.0],
            other => panic!("expected position vector, got {other:?}"),
        };
        assert_eq!(actual, expected, "fixture {fixture}");
        assert!(actual[0].abs() < 1_000_000.0 && actual[1].abs() < 1_000_000.0);
    }
}

#[test]
fn pal_gender_instance_id_and_owner_decode_and_join_for_party_and_storage() {
    for (fixture, _) in FIXTURES {
        let player = decode(&fixture_path(fixture, PLAYER_FILE));
        let save_data = properties(property(&player.root.properties, "SaveData"));
        let host_uid = guid(property(save_data, "PlayerUId"));
        let party_id = wrapped_guid(property(save_data, "OtomoCharacterContainerId"), "ID");
        let storage_id = wrapped_guid(property(save_data, "PalStorageContainerId"), "ID");

        let level = decode(&fixture_path(fixture, "Level.sav"));
        let world = properties(property(&level.root.properties, "worldSaveData"));
        let characters = character_map(world);
        let party = container_slots(world, party_id);
        let storage = container_slots(world, storage_id);
        let all_slots: Vec<_> = party.iter().chain(&storage).copied().collect();

        let unique_ids: HashSet<_> = all_slots.iter().map(|(_, id)| *id).collect();
        assert_eq!(unique_ids.len(), all_slots.len(), "fixture {fixture}");
        assert_eq!((party.len(), storage.len()), (3, 28), "fixture {fixture}");
        assert_eq!(
            party[0].1.to_string(),
            "719954a8-47f3-d347-6393-1084f7eb5122",
            "the same stable first-party instance id is present in both snapshots"
        );

        for (slot_owner, instance_id) in &all_slots {
            assert_eq!(*slot_owner, host_uid, "fixture {fixture}, pal {instance_id}");
            assert!(characters.contains_key(instance_id));
        }

        let joined: Vec<_> = all_slots
            .iter()
            .map(|(_, instance_id)| &characters[instance_id])
            .collect();
        let genders: HashSet<_> = joined
            .iter()
            .map(|pal| match pal_field(pal, "Gender") {
                PalValue::Enum { enum_type, value } => (enum_type.as_str(), value.as_str()),
                other => panic!("expected gender enum, got {other:?}"),
            })
            .collect();
        assert!(genders.contains(&("EPalGenderType", "EPalGenderType::Female")));
        assert!(genders.contains(&("EPalGenderType", "EPalGenderType::Male")));
        let species_and_sex: HashSet<_> = joined
            .iter()
            .map(|pal| {
                let species = match pal_field(pal, "CharacterID") {
                    PalValue::Str(species) => species.as_str(),
                    other => panic!("expected species name, got {other:?}"),
                };
                let sex = match pal_field(pal, "Gender") {
                    PalValue::Enum { value, .. } => value.as_str(),
                    other => panic!("expected gender enum, got {other:?}"),
                };
                (species, sex)
            })
            .collect();
        assert!(species_and_sex.contains(&("ChickenPal", "EPalGenderType::Female")));
        assert!(species_and_sex.contains(&("PinkCat", "EPalGenderType::Male")));

        let party_pal = &characters[&party[0].1];
        let owner = match pal_field(party_pal, "OwnerPlayerUId") {
            PalValue::Guid(owner) => guid_from_raw(owner),
            other => panic!("expected owner GUID, got {other:?}"),
        };
        assert_eq!(owner, host_uid, "fixture {fixture}");
    }
}
