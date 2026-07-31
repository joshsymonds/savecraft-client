//! Decodes a single Palworld save member: PlM1 container -> Kraken -> GVAS.

use crate::container::{self, ContainerError};
use crate::decompress;
use uesave::{Save, SaveReader, StructType, Types};

#[derive(Debug)]
pub enum GvasError {
    Container(ContainerError),
    Decompress(String),
    Parse { offset: usize, message: String },
}

impl std::fmt::Display for GvasError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            GvasError::Container(e) => write!(f, "{e}"),
            GvasError::Decompress(msg) => write!(f, "{msg}"),
            GvasError::Parse { offset, message } => {
                write!(f, "at offset {offset}: {message}")
            }
        }
    }
}

/// Palworld property-type hints uesave needs to resolve otherwise-ambiguous
/// Map/Set struct types in `worldSaveData`. Ported from
/// cheahjs/palworld-save-tools `PALWORLD_TYPE_HINTS` (`paltypes.py`), with
/// leading dots stripped and each hint's `"StructProperty"` string mapped to
/// uesave's `Struct` (a plain nested property list) and `"Guid"` mapped
/// straight across. Two of the ported entries have a doubled path component
/// in palworld-save-tools' own convention (e.g.
/// `MapObjectSaveData.MapObjectSaveData`, `DungeonSaveData.DungeonSaveData`)
/// that does not appear in uesave's paths — collapsed to a single component
/// here, verified against the real Level.sav fixture (parses to a clean EOF
/// with zero leftover bytes using this exact table).
const TYPE_HINTS: &[(&str, &str)] = &[
    ("worldSaveData.CharacterContainerSaveData.Key", "Struct"),
    ("worldSaveData.CharacterSaveParameterMap.Key", "Struct"),
    ("worldSaveData.CharacterSaveParameterMap.Value", "Struct"),
    ("worldSaveData.FoliageGridSaveDataMap.Key", "Struct"),
    (
        "worldSaveData.FoliageGridSaveDataMap.Value.ModelMap.Value",
        "Struct",
    ),
    (
        "worldSaveData.FoliageGridSaveDataMap.Value.ModelMap.Value.InstanceDataMap.Key",
        "Struct",
    ),
    (
        "worldSaveData.FoliageGridSaveDataMap.Value.ModelMap.Value.InstanceDataMap.Value",
        "Struct",
    ),
    ("worldSaveData.FoliageGridSaveDataMap.Value", "Struct"),
    ("worldSaveData.ItemContainerSaveData.Key", "Struct"),
    (
        "worldSaveData.MapObjectSaveData.ConcreteModel.ModuleMap.Value",
        "Struct",
    ),
    (
        "worldSaveData.MapObjectSaveData.Model.EffectMap.Value",
        "Struct",
    ),
    (
        "worldSaveData.MapObjectSpawnerInStageSaveData.Key",
        "Struct",
    ),
    (
        "worldSaveData.MapObjectSpawnerInStageSaveData.Value",
        "Struct",
    ),
    (
        "worldSaveData.MapObjectSpawnerInStageSaveData.Value.SpawnerDataMapByLevelObjectInstanceId.Key",
        "Guid",
    ),
    (
        "worldSaveData.MapObjectSpawnerInStageSaveData.Value.SpawnerDataMapByLevelObjectInstanceId.Value",
        "Struct",
    ),
    (
        "worldSaveData.MapObjectSpawnerInStageSaveData.Value.SpawnerDataMapByLevelObjectInstanceId.Value.ItemMap.Value",
        "Struct",
    ),
    ("worldSaveData.WorkSaveData.WorkAssignMap.Value", "Struct"),
    ("worldSaveData.BaseCampSaveData.Key", "Guid"),
    ("worldSaveData.BaseCampSaveData.Value", "Struct"),
    (
        "worldSaveData.BaseCampSaveData.Value.ModuleMap.Value",
        "Struct",
    ),
    ("worldSaveData.ItemContainerSaveData.Value", "Struct"),
    ("worldSaveData.CharacterContainerSaveData.Value", "Struct"),
    ("worldSaveData.GroupSaveDataMap.Key", "Guid"),
    ("worldSaveData.GroupSaveDataMap.Value", "Struct"),
    (
        "worldSaveData.EnemyCampSaveData.EnemyCampStatusMap.Value",
        "Struct",
    ),
    (
        "worldSaveData.DungeonSaveData.MapObjectSaveData.Model.EffectMap.Value",
        "Struct",
    ),
    (
        "worldSaveData.DungeonSaveData.MapObjectSaveData.ConcreteModel.ModuleMap.Value",
        "Struct",
    ),
    ("worldSaveData.InvaderSaveData.Key", "Guid"),
    ("worldSaveData.InvaderSaveData.Value", "Struct"),
    ("worldSaveData.OilrigSaveData.OilrigMap.Value", "Struct"),
    ("worldSaveData.SupplySaveData.SupplyInfos.Key", "Guid"),
    ("worldSaveData.SupplySaveData.SupplyInfos.Value", "Struct"),
    // Current-build additions not present in the upstream Python table.
    ("worldSaveData.GuildExtraSaveDataMap.Key", "Guid"),
    ("worldSaveData.GuildExtraSaveDataMap.Value", "Struct"),
    (
        "worldSaveData.DungeonSaveData.RewardSaveDataMap.Key",
        "Guid",
    ),
    (
        "worldSaveData.DungeonSaveData.RewardSaveDataMap.Value",
        "Struct",
    ),
    (
        "worldSaveData.EnemyCampSaveData.EnemyCampStatusMap.Value.TreasureBoxInfoMapBySpawnerName.Value",
        "Struct",
    ),
];

fn build_types() -> Types {
    let mut types = Types::new();
    for (path, hint) in TYPE_HINTS {
        types.add((*path).to_string(), StructType::from(*hint));
    }
    types
}

/// Decode one Palworld save member's raw bytes (as read from the tar) into
/// a parsed GVAS [`Save`]: PlM1 container -> Kraken decompression -> uesave.
/// Takes `raw` by value (rather than borrowing it) so it can be dropped as
/// soon as decompression has produced `gvas_bytes` from it, instead of
/// staying alive for the whole call -- at that point `raw` (the compressed
/// member bytes, up to `tarball::MAX_MEMBER_SIZE`) is no longer needed,
/// while uesave's parse of `gvas_bytes` into its `Save` tree necessarily
/// keeps that decompressed buffer alive for the parse's duration (that's
/// the unavoidable floor -- see P1+P2).
pub fn decode(raw: Vec<u8>) -> Result<Save, GvasError> {
    let header = container::parse_header(&raw).map_err(GvasError::Container)?;
    let payload = container::payload(&raw, &header);

    let gvas_bytes =
        decompress::decompress(payload, header.uncompressed_len).map_err(GvasError::Decompress)?;
    drop(raw);

    SaveReader::new()
        .types(build_types())
        // Palworld's save format evolves between patches faster than this
        // parser's TYPE_HINTS table can be kept in sync. `error_to_raw`
        // downgrades an unparseable property to a `Property::Raw` blob
        // instead of failing the whole decode, so a save with one unknown
        // property still yields a usable result for everything else.
        .error_to_raw(true)
        .read(std::io::Cursor::new(gvas_bytes))
        .map_err(|e| GvasError::Parse {
            offset: e.offset,
            message: e.error.to_string(),
        })
}
