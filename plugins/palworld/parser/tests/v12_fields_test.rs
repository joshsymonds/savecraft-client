//! Fixture-pinned byte evidence for the v1.2 base-detail GO/NO-GO spike.
//!
//! No speculative RawData codec is used here: every confirmed prefix, offset,
//! cross-reference, and opaque boundary is asserted directly against both real
//! saves after the production PlM1/Kraken/GVAS decode.

use std::path::PathBuf;
use uesave::{ByteArray, FGuid, MapEntry, Properties, Property, StructValue, ValueVec};

const FIXTURES: [(&str, usize, usize, usize); 2] = [
    ("1FCE97C34D214643B96A23A20A9E27D1", 584, 80, 37),
    ("live-20260731", 568, 81, 36),
];
const BASE_ID: &str = "4e18f078-4717-eb0a-043c-01b96f527fff";
const WORKER_CONTAINER_ID: &str = "b7a83998-4821-1a8a-db54-42991e05b929";
const STOCKED_CONTAINER_ID: &str = "92ace5be-4b70-9668-c65e-dab29232d0d7";
const ASSIGNED_PALS: [&str; 3] = [
    "8fd0a461-4b36-96d9-592e-99bee9ca4953",
    "c63b3553-49f9-f3f5-1aee-e498828c41ad",
    "36d42d72-4589-a111-073f-50a0556e6866",
];
const WORKER_PALS: [&str; 5] = [
    "36d42d72-4589-a111-073f-50a0556e6866",
    "968f5f72-4400-9682-9adb-6ab8f5088316",
    "c63b3553-49f9-f3f5-1aee-e498828c41ad",
    "8fd0a461-4b36-96d9-592e-99bee9ca4953",
    "baebd795-452c-b1d3-9021-6a9793fc59e3",
];
const GUILD_ID: &str = "ef9302c3-4326-566b-926d-76a0f74ab46d";

fn fixture_path(fixture: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(fixture)
        .join("Level.sav")
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

fn raw(value: &Property) -> &[u8] {
    match value {
        Property::Array(ValueVec::Byte(ByteArray::Byte(bytes))) => bytes,
        other => panic!("expected byte array, got {other:?}"),
    }
}

fn guid(value: &Property) -> FGuid {
    match value {
        Property::Struct(StructValue::Guid(guid)) => *guid,
        other => panic!("expected GUID, got {other:?}"),
    }
}

fn guid_bytes(guid: FGuid) -> [u8; 16] {
    let compact = guid.to_string().replace('-', "");
    let mut bytes = [0; 16];
    for word in 0..4 {
        bytes[word * 4..word * 4 + 4].copy_from_slice(
            &u32::from_str_radix(&compact[word * 8..word * 8 + 8], 16)
                .unwrap()
                .to_le_bytes(),
        );
    }
    bytes
}

fn raw_guid(bytes: &[u8]) -> FGuid {
    FGuid::new(
        u32::from_le_bytes(bytes[0..4].try_into().unwrap()),
        u32::from_le_bytes(bytes[4..8].try_into().unwrap()),
        u32::from_le_bytes(bytes[8..12].try_into().unwrap()),
        u32::from_le_bytes(bytes[12..16].try_into().unwrap()),
    )
}

fn decoded_world(fixture: &str) -> uesave::Save {
    let bytes = std::fs::read(fixture_path(fixture)).unwrap();
    palworld_parser::gvas::decode(bytes).unwrap()
}

fn container_entry<'a>(world: &'a Properties, collection: &str, id: &str) -> &'a MapEntry {
    map(property(world, collection))
        .iter()
        .find(|entry| guid(property(properties(&entry.key), "ID")).to_string() == id)
        .unwrap_or_else(|| panic!("missing {collection} entry {id}"))
}

#[test]
fn base_prefixes_worker_container_and_opaque_boundaries_are_pinned() {
    for (fixture, work_collection_len, _, _) in FIXTURES {
        let save = decoded_world(fixture);
        let world = properties(property(&save.root.properties, "worldSaveData"));
        let bases = map(property(world, "BaseCampSaveData"));
        assert_eq!(bases.len(), 1, "fixture {fixture}");
        assert_eq!(guid(&bases[0].key).to_string(), BASE_ID, "fixture {fixture}");

        let base = properties(&bases[0].value);
        let base_raw = raw(property(base, "RawData"));
        let worker_raw = raw(property(properties(property(base, "WorkerDirector")), "RawData"));
        let work_raw = raw(property(properties(property(base, "WorkCollection")), "RawData"));
        let base_id = guid_bytes(guid(&bases[0].key));
        assert_eq!((base_raw.len(), worker_raw.len(), work_raw.len()), (257, 118, work_collection_len));
        assert_eq!(&base_raw[0..16], &base_id);
        assert_eq!(&worker_raw[0..16], &base_id);
        assert_eq!(&work_raw[0..16], &base_id);
        assert_eq!(u32::from_le_bytes(work_raw[16..20].try_into().unwrap()), if fixture == FIXTURES[0].0 { 35 } else { 34 });
        assert_eq!(raw_guid(&worker_raw[98..114]).to_string(), WORKER_CONTAINER_ID);
        assert_eq!(&worker_raw[114..118], &[0, 0, 0, 0]);

        let worker = properties(&container_entry(world, "CharacterContainerSaveData", WORKER_CONTAINER_ID).value);
        let occupied: Vec<_> = structs(property(worker, "Slots"))
            .iter()
            .filter_map(|slot| {
                let StructValue::Struct(slot) = slot else { panic!("slot is not a struct") };
                palworld_parser::rawdata::decode_character_container_slot(raw(property(slot, "RawData")))
                    .unwrap()
            })
            .collect();
        assert_eq!(occupied.len(), 5, "fixture {fixture}");
        let ids: Vec<_> = occupied.iter().map(|slot| raw_guid(&slot.instance_id).to_string()).collect();
        for assigned in ASSIGNED_PALS {
            assert!(ids.iter().any(|id| id == assigned), "fixture {fixture}, assigned pal {assigned}");
        }
    }
}

#[test]
fn base_object_item_container_and_build_process_bytes_are_pinned() {
    let expected_items = [
        (0, 26, "Wood"), (1, 2, "Stone"), (2, 2, "bone"),
        (3, 22, "CopperOre"), (4, 6, "BerrySeeds"), (5, 3, "Leather"),
        (7, 23, "Fiber"), (8, 1, "PalEgg_Water_01"),
    ];
    for (fixture, _, object_count, base_object_count) in FIXTURES {
        let save = decoded_world(fixture);
        let world = properties(property(&save.root.properties, "worldSaveData"));
        let base_id = guid_bytes(guid(&map(property(world, "BaseCampSaveData"))[0].key));
        let objects = structs(property(world, "MapObjectSaveData"));
        assert_eq!(objects.len(), object_count, "fixture {fixture}");

        let base_objects: Vec<_> = objects.iter().filter_map(|object| {
            let StructValue::Struct(object) = object else { panic!("object is not a struct") };
            let model = properties(property(object, "Model"));
            (raw(property(model, "RawData")).get(32..48) == Some(base_id.as_slice()))
                .then_some((object, model))
        }).collect();
        assert_eq!(base_objects.len(), base_object_count, "fixture {fixture}");
        for (_, model) in &base_objects {
            assert_eq!(raw(property(properties(property(model, "BuildProcess")), "RawData")), &[1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
        }

        let (chest, _) = base_objects.iter().find(|(object, _)| {
            matches!(property(object, "MapObjectId"), Property::Name(name) if name == "ItemChest")
        }).expect("base ItemChest");
        let modules = map(property(properties(property(chest, "ConcreteModel")), "ModuleMap"));
        let module = modules.iter().find(|module| {
            matches!(&module.key, Property::Enum(name) if name.ends_with("::ItemContainer"))
        }).unwrap();
        let module_raw = raw(property(properties(&module.value), "RawData"));
        assert_eq!(module_raw.len(), 33);
        assert_eq!(raw_guid(&module_raw[0..16]).to_string(), STOCKED_CONTAINER_ID);

        let container = properties(&container_entry(world, "ItemContainerSaveData", STOCKED_CONTAINER_ID).value);
        let items: Vec<_> = structs(property(container, "Slots")).iter().filter_map(|slot| {
            let StructValue::Struct(slot) = slot else { panic!("slot is not a struct") };
            palworld_parser::rawdata::decode_item_slot(raw(property(slot, "RawData"))).unwrap()
        }).map(|slot| (slot.slot_index, slot.count, slot.static_id)).collect();
        assert_eq!(items.len(), expected_items.len(), "fixture {fixture}");
        for (actual, expected) in items.iter().zip(expected_items) {
            assert_eq!((actual.0, actual.1, actual.2.as_str()), expected, "fixture {fixture}");
        }
    }
}

#[test]
fn work_assignment_prefixes_and_machine_state_are_pinned() {
    for (fixture, _, _, _) in FIXTURES {
        let save = decoded_world(fixture);
        let world = properties(property(&save.root.properties, "worldSaveData"));
        let work = structs(property(world, "WorkSaveData"));
        assert_eq!(work.len(), 8, "fixture {fixture}");
        let mut assigned = Vec::new();
        let mut monster_farm_id = None;
        for record in work {
            let StructValue::Struct(record) = record else { panic!("work is not a struct") };
            let record_raw = raw(property(record, "RawData"));
            let record_id = &record_raw[0..16];
            if matches!(property(record, "WorkableType"), Property::Enum(name) if name == "EPalWorkableType::MonsterFarm") {
                monster_farm_id = Some(record_id.to_vec());
            }
            for assignment in map(property(record, "WorkAssignMap")) {
                assert_eq!(assignment.key, Property::Int(0));
                let assignment_raw = raw(property(properties(&assignment.value), "RawData"));
                assert_eq!(assignment_raw.len(), 62);
                assert_eq!(&assignment_raw[0..16], record_id);
                assigned.push(raw_guid(&assignment_raw[37..53]).to_string());
                assert_eq!(&assignment_raw[53..62], &[2, 1, 0, 0, 0, 0, 0, 0, 0]);
            }
        }
        assert_eq!(assigned, ASSIGNED_PALS, "fixture {fixture}");

        let machine_id = monster_farm_id.expect("MonsterFarm work record");
        let linked = structs(property(world, "MapObjectSaveData")).iter().any(|object| {
            let StructValue::Struct(object) = object else { return false };
            if !matches!(property(object, "MapObjectId"), Property::Name(name) if name == "MonsterFarm") {
                return false;
            }
            map(property(properties(property(object, "ConcreteModel")), "ModuleMap")).iter().any(|module| {
                matches!(&module.key, Property::Enum(name) if name.ends_with("::Workee"))
                    && raw(property(properties(&module.value), "RawData")).get(0..16) == Some(machine_id.as_slice())
            })
        });
        assert!(linked, "fixture {fixture}");
    }
}

#[test]
fn base_position_and_name_are_cross_validated() {
    const POSITION: [f64; 3] = [
        -347922.1286995581,
        264690.7242905643,
        4006.8075977033227,
    ];
    const PLAYER_POSITIONS: [[f64; 3]; 2] = [
        [-346946.95917404373, 191651.84791294907, -211.4045001832311],
        [-351879.20256971574, 201841.4198500383, 1731.0433408787007],
    ];

    for ((fixture, _, _, _), player_position) in FIXTURES.into_iter().zip(PLAYER_POSITIONS) {
        let save = decoded_world(fixture);
        let world = properties(property(&save.root.properties, "worldSaveData"));
        let base = &map(property(world, "BaseCampSaveData"))[0];
        let base_raw = raw(property(properties(&base.value), "RawData"));
        assert_eq!(i32::from_le_bytes(base_raw[16..20].try_into().unwrap()), -18);
        let name_units: Vec<_> = base_raw[20..54]
            .chunks_exact(2)
            .map(|pair| u16::from_le_bytes(pair.try_into().unwrap()))
            .collect();
        assert_eq!(&base_raw[54..56], &[0, 0]);
        assert_eq!(
            String::from_utf16(&name_units).unwrap(),
            "新規生成拠点テンプレート名0(仮)"
        );

        let position = [89, 97, 105].map(|offset| {
            f64::from_le_bytes(base_raw[offset..offset + 8].try_into().unwrap())
        });
        assert_eq!(position, POSITION, "fixture {fixture}");
        let plausible_vector_offsets: Vec<_> = (0..=base_raw.len() - 24)
            .filter(|offset| {
                let values = [0, 8, 16].map(|relative| {
                    f64::from_le_bytes(
                        base_raw[offset + relative..offset + relative + 8]
                            .try_into()
                            .unwrap(),
                    )
                });
                values.iter().all(|value| value.is_finite() && value.abs() <= 1_000_000.0)
                    && values[0].abs() > 10_000.0
                    && values[1].abs() > 10_000.0
            })
            .collect();
        assert_eq!(plausible_vector_offsets, [89], "fixture {fixture}");

        let palbox_model = structs(property(world, "MapObjectSaveData"))
            .iter()
            .find_map(|object| {
                let StructValue::Struct(object) = object else { return None };
                let model = properties(property(object, "Model"));
                (matches!(property(object, "MapObjectId"), Property::Name(name) if name == "PalBoxV2")
                    && raw(property(model, "RawData")).get(32..48)
                        == Some(guid_bytes(guid(&base.key)).as_slice()))
                .then_some(raw(property(model, "RawData")))
            })
            .expect("base-linked PalBoxV2");
        assert_eq!(&base_raw[89..113], &palbox_model[104..128]);

        let player_distance = position
            .into_iter()
            .zip(player_position)
            .map(|(base, player)| (base - player).powi(2))
            .sum::<f64>()
            .sqrt();
        assert!(player_distance < 75_000.0, "fixture {fixture}: {player_distance}");
        let readout = [
            (position[1] - 158000.0) / 459.0,
            (position[0] - -123888.0) / -459.0,
        ];
        assert!(readout
            .into_iter()
            .all(|coordinate| (0.0..=1000.0).contains(&coordinate)));
    }

    assert!(include_str!("../docs/v1.2-fields.md").contains("### Base position and name"));
}

#[test]
fn base_to_guild_candidate_joins_are_pinned() {
    for (fixture, _, _, _) in FIXTURES {
        let save = decoded_world(fixture);
        let world = properties(property(&save.root.properties, "worldSaveData"));
        let base = &map(property(world, "BaseCampSaveData"))[0];
        let base_raw = raw(property(properties(&base.value), "RawData"));

        let guilds: Vec<_> = map(property(world, "GroupSaveDataMap"))
            .iter()
            .filter(|entry| {
                matches!(
                    property(properties(&entry.value), "GroupType"),
                    Property::Enum(group_type) if group_type == "EPalGroupType::Guild"
                )
            })
            .map(|entry| {
                let group_raw = raw(property(properties(&entry.value), "RawData"));
                palworld_parser::rawdata::decode_group_save_data(group_raw).unwrap()
            })
            .collect();
        assert_eq!(guilds.len(), 1, "fixture {fixture}");
        assert_eq!(raw_guid(&guilds[0].group_id).to_string(), GUILD_ID);
        assert_eq!(
            &base_raw[56..89],
            &[
                0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
                0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4a, 0x9d, 0x59, 0x77, 0xc1,
                0xe4, 0xe2, 0xbf, 0x74, 0x3e, 0x5a, 0xc5, 0xb4, 0xd3, 0xe9, 0x3f,
            ],
            "fixture {fixture}"
        );

        let direct_offsets: Vec<_> = (0..=base_raw.len() - 16)
            .filter(|offset| {
                guilds
                    .iter()
                    .any(|guild| base_raw[*offset..*offset + 16] == guild.group_id)
            })
            .collect();
        assert_eq!(direct_offsets, [141], "fixture {fixture}");
        assert_eq!(&base_raw[141..157], &guilds[0].group_id, "fixture {fixture}");

        let worker_raw = raw(property(
            properties(property(properties(&base.value), "WorkerDirector")),
            "RawData",
        ));
        let worker_container_id = raw_guid(&worker_raw[98..114]).to_string();
        let worker = properties(
            &container_entry(world, "CharacterContainerSaveData", &worker_container_id).value,
        );
        let worker_ids: Vec<_> = structs(property(worker, "Slots"))
            .iter()
            .filter_map(|slot| {
                let StructValue::Struct(slot) = slot else {
                    panic!("slot is not a struct")
                };
                palworld_parser::rawdata::decode_character_container_slot(raw(property(
                    slot, "RawData",
                )))
                .unwrap()
            })
            .map(|slot| slot.instance_id)
            .collect();
        assert_eq!(worker_ids.len(), 5, "fixture {fixture}");
        assert_eq!(
            worker_ids
                .iter()
                .map(|id| raw_guid(id).to_string())
                .collect::<Vec<_>>(),
            WORKER_PALS,
            "fixture {fixture}"
        );
        let membership_counts: Vec<_> = worker_ids
            .iter()
            .map(|worker_id| {
                guilds
                    .iter()
                    .filter(|guild| {
                        guild
                            .member_handle_ids
                            .iter()
                            .any(|(_, instance_id)| instance_id == worker_id)
                    })
                    .count()
            })
            .collect();
        assert_eq!(membership_counts, [1; 5], "fixture {fixture}");
    }

    assert!(include_str!("../docs/v1.2-fields.md").contains("### Base-to-guild attribution"));
}
