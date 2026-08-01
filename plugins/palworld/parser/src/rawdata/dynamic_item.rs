//! `DynamicItemSaveData` entry `RawData`: a dynamically-generated item's
//! stable identity, plus (for eggs) which pal it will hatch into.
//!
//! Ported from cheahjs/palworld-save-tools `rawdata/dynamic_item.py` (git
//! main), which after the shared `id` fields tries an egg-shaped payload
//! first, then falls back to armor (exactly 4 remaining bytes: a
//! durability float) or weapon (durability + remaining bullets + passive
//! skill list) shapes, keeping unparsed bytes raw as a last resort.
//!
//! Every entry in this fixture (41 `DynamicItemSaveData` entries) is an
//! unhatched egg, so only the `id` fields and the egg shape are verified
//! against real bytes here. Armor/weapon are real, documented upstream
//! shapes, but this save has no example to check the field layout against
//! -- guessing at an unverified byte layout is exactly what the contract's
//! "never guess" rule rules out, so non-egg items decode as
//! [`DynamicItemKind::Other`], with their entire post-`id` payload kept as
//! opaque `trailing` bytes for a future task to decode once it has a real
//! armor/weapon fixture to verify against.
//!
//! One correction the fixture demands versus upstream: every entry has 4
//! extra bytes (always zero in this save) immediately after `id.static_id`
//! and before the type-specific payload, which upstream's `dynamic_item.py`
//! doesn't account for -- without skipping them, every entry's egg-shaped
//! payload misparses. Verified against two entries with different-length
//! `static_id` strings (ruling out an off-by-one in the fstring read
//! itself): both align perfectly, down to an identical trailing byte count,
//! once these 4 bytes are treated as a separate unknown field rather than
//! guessed at.

use super::properties::{PalProperty, read_properties_until_none};
#[cfg(test)]
use super::reader::test_support::ascii_fstring;
use super::reader::{RawDataError, Reader};

/// A dynamic item's stable identity: which world it was created in (for
/// multiplayer saves), its instance id within that world, and its static
/// item id (e.g. `PalEgg_Fire_01`).
#[derive(Debug, Clone, PartialEq)]
pub struct DynamicItemId {
    pub created_world_id: [u8; 16],
    pub local_id_in_created_world: [u8; 16],
    pub static_id: String,
}

/// The type-specific payload following a dynamic item's `id`. See module
/// docs for why only `Egg` is decoded further.
#[derive(Debug, Clone, PartialEq)]
pub enum DynamicItemKind {
    /// An unhatched egg: which pal it will hatch into, plus whatever
    /// properties were attached to it (empty in every entry this fixture
    /// has).
    Egg {
        character_id: String,
        object: Vec<PalProperty>,
    },
    /// Any other dynamic item (e.g. upstream's durability-tracked weapon
    /// or armor shapes). Its entire payload is kept in the outer
    /// [`DynamicItem::trailing`] field, undecoded.
    Other,
}

/// A decoded `DynamicItemSaveData` entry's `RawData`.
#[derive(Debug, Clone, PartialEq)]
pub struct DynamicItem {
    pub id: DynamicItemId,
    /// 4 bytes immediately after `id.static_id`, always zero in this
    /// fixture, that upstream's format doesn't account for -- see module
    /// docs.
    pub unknown: [u8; 4],
    pub kind: DynamicItemKind,
    /// For `Egg`: the unknown bytes + owner guid (and this build's extra
    /// padding) that follow the egg's property object. For `Other`: the
    /// item's entire undecoded payload.
    pub trailing: Vec<u8>,
}

/// Decodes one entry's `RawData`. Empty bytes are valid, mirroring
/// upstream's own `len(c_bytes) == 0 => None` guard.
pub fn decode_dynamic_item(raw: &[u8]) -> Result<Option<DynamicItem>, RawDataError> {
    if raw.is_empty() {
        return Ok(None);
    }

    let mut r = Reader::new(raw, "DynamicItemSaveData.RawData");
    r.push("id");
    let created_world_id = r.guid()?;
    let local_id_in_created_world = r.guid()?;
    let static_id = r.fstring()?;
    r.pop();
    let id = DynamicItemId {
        created_world_id,
        local_id_in_created_world,
        static_id,
    };

    r.push("unknown");
    let unknown: [u8; 4] = r.bytes(4)?.try_into().unwrap();
    r.pop();

    let after_unknown = r.position();
    r.push("egg");
    let egg = try_read_egg(&mut r);
    r.pop();

    let (kind, trailing) = match egg {
        Ok((character_id, object)) => {
            let trailing = r.rest().to_vec();
            (
                DynamicItemKind::Egg {
                    character_id,
                    object,
                },
                trailing,
            )
        }
        // An egg-shaped parse failure on a non-egg item is expected (its
        // payload just isn't egg-shaped) and falls back to `Other`. But an
        // id that's already declared itself an egg (`PalEgg*`) failing to
        // parse as one is a real decode error -- propagate it instead of
        // silently misclassifying a truncated or malformed egg as `Other`.
        Err(e) if id.static_id.starts_with("PalEgg") => return Err(e),
        Err(_) => {
            r.seek(after_unknown);
            (DynamicItemKind::Other, r.rest().to_vec())
        }
    };

    Ok(Some(DynamicItem {
        id,
        unknown,
        kind,
        trailing,
    }))
}

fn try_read_egg(r: &mut Reader) -> Result<(String, Vec<PalProperty>), RawDataError> {
    let character_id = r.fstring()?;
    let object = read_properties_until_none(r)?;
    Ok((character_id, object))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_bytes_decode_to_none() {
        assert_eq!(decode_dynamic_item(&[]).unwrap(), None);
    }

    #[test]
    fn decodes_egg_shaped_payload() {
        let mut data = vec![0u8; 32]; // created_world_id + local_id_in_created_world
        data.extend_from_slice(&ascii_fstring("PalEgg_Fire_01")); // static_id
        data.extend_from_slice(&[0u8; 4]); // unknown field
        data.extend_from_slice(&ascii_fstring("Monkey_Fire")); // character_id
        data.extend_from_slice(&ascii_fstring("None")); // empty object
        data.extend_from_slice(&[0u8; 20]); // build-specific trailer

        let decoded = decode_dynamic_item(&data).unwrap().unwrap();
        assert_eq!(decoded.id.static_id, "PalEgg_Fire_01");
        assert_eq!(decoded.unknown, [0u8; 4]);
        match decoded.kind {
            DynamicItemKind::Egg {
                character_id,
                object,
            } => {
                assert_eq!(character_id, "Monkey_Fire");
                assert!(object.is_empty());
            }
            other => panic!("expected Egg, got {other:?}"),
        }
        assert_eq!(decoded.trailing, vec![0u8; 20]);
    }

    #[test]
    fn truncated_egg_payload_on_a_palegg_id_errors_instead_of_falling_back_to_other() {
        let mut data = vec![0u8; 32]; // created_world_id + local_id_in_created_world
        data.extend_from_slice(&ascii_fstring("PalEgg_Fire_01")); // static_id
        data.extend_from_slice(&[0u8; 4]); // unknown field
        data.extend_from_slice(&[0xFFu8, 0xFF]); // truncated character_id fstring: needs 4 bytes, only 2 given

        let err = decode_dynamic_item(&data).unwrap_err();
        assert_eq!(err.path, "DynamicItemSaveData.RawData.egg");
        assert!(err.message.contains("truncated"));
    }

    #[test]
    fn non_egg_payload_falls_back_to_other_with_raw_trailing_bytes() {
        let mut data = vec![0u8; 32];
        data.extend_from_slice(&ascii_fstring("SomeWeapon_01"));
        data.extend_from_slice(&[0u8; 4]); // unknown field
        let payload = [1u8, 2, 3, 4, 5, 6, 7, 8];
        data.extend_from_slice(&payload);

        let decoded = decode_dynamic_item(&data).unwrap().unwrap();
        assert_eq!(decoded.kind, DynamicItemKind::Other);
        assert_eq!(decoded.trailing, payload);
    }

    #[test]
    fn truncated_id_errors_naming_the_path_no_panic() {
        let data = vec![0u8; 10]; // not even a full created_world_id guid
        let err = decode_dynamic_item(&data).unwrap_err();
        assert_eq!(err.path, "DynamicItemSaveData.RawData.id");
        assert!(err.message.contains("truncated"));
    }
}
