//! `ItemContainerSaveData` value `RawData` (the container-level permission
//! blob) and `ItemContainerSaveData.Value.Slots.Slots` value `RawData` (the
//! per-slot item content).
//!
//! The container-level blob is a permission/allow-list: which item types
//! this container may hold, *not* its actual contents. Ported from
//! cheahjs/palworld-save-tools `rawdata/item_container.py` (git main).
//! Unlike `character`/`character_container`/`group`, upstream's own decoder
//! here already tolerates leftover bytes after the three arrays
//! (`trailing_unparsed_data`, no assertion) rather than requiring exact
//! EOF, and every entry decoded against the real fixture (264 containers,
//! several with non-empty `type_a`/`type_b`/`item_static_ids`) matched that
//! shape with no divergence -- so no fixture-specific surprises here.
//!
//! The per-slot blob *is* the container's actual contents: which item
//! occupies a slot, how many, and (for dynamically-generated items like
//! eggs) which `DynamicItemSaveData` entry it references. Verified against
//! every populated slot across all 264 containers in the real fixture:
//! `Slots` is a *sparse* array (its length can be less than the container's
//! own `SlotNum` capacity -- e.g. a 42-slot container with only 23 `Slots`
//! entries) rather than one entry per capacity slot, so `slot_index` (the
//! field this decodes first) is which of the container's `SlotNum` slots
//! this entry occupies, not a position within `Slots` itself. Cross-codec
//! pin: an egg's `dynamic_item_id` here matches a real
//! `DynamicItemSaveData` entry's `id.created_world_id`/
//! `id.local_id_in_created_world` exactly (see `dynamic_item.rs`).

#[cfg(test)]
use super::reader::test_support::ascii_fstring;
use super::reader::{RawDataError, Reader};

/// A decoded `ItemContainerSaveData` value's `RawData`.
#[derive(Debug, Clone, PartialEq)]
pub struct ItemContainerPermission {
    pub type_a: Vec<u8>,
    pub type_b: Vec<u8>,
    pub item_static_ids: Vec<String>,
    /// Bytes left over after the three arrays -- always zero-padding in
    /// this fixture, kept opaque rather than assumed meaningless.
    pub trailing: Vec<u8>,
}

/// Decodes a container's `RawData`. Empty bytes are valid, mirroring
/// upstream's own `len(c_bytes) == 0 => None` guard.
pub fn decode_item_container_permission(
    raw: &[u8],
) -> Result<Option<ItemContainerPermission>, RawDataError> {
    if raw.is_empty() {
        return Ok(None);
    }

    let mut r = Reader::new(raw, "ItemContainerSaveData.Value.RawData");

    r.push("permission.type_a");
    let type_a = read_byte_array(&mut r)?;
    r.pop();

    r.push("permission.type_b");
    let type_b = read_byte_array(&mut r)?;
    r.pop();

    r.push("permission.item_static_ids");
    // Each element is an FString: at least its i32 length prefix.
    let count = r.count(4)?;
    let mut item_static_ids = Vec::with_capacity(count);
    for _ in 0..count {
        item_static_ids.push(r.fstring()?);
    }
    r.pop();

    let trailing = r.rest().to_vec();

    Ok(Some(ItemContainerPermission {
        type_a,
        type_b,
        item_static_ids,
        trailing,
    }))
}

fn read_byte_array(r: &mut Reader) -> Result<Vec<u8>, RawDataError> {
    let count = r.count(1)?;
    let mut values = Vec::with_capacity(count);
    for _ in 0..count {
        values.push(r.u8()?);
    }
    Ok(values)
}

/// A dynamically-generated item's stable identity, as referenced from an
/// [`ItemSlot`] -- the same `(created_world_id, local_id_in_created_world)`
/// pair a real `DynamicItemSaveData` entry's `id` carries (see
/// `dynamic_item.rs`), but without that entry's own `static_id` (the slot
/// already has its own copy of it).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ItemSlotDynamicId {
    pub created_world_id: [u8; 16],
    pub local_id_in_created_world: [u8; 16],
}

/// A decoded, occupied item container slot's `RawData`: which item, how
/// many, and (for dynamically-generated items) which `DynamicItemSaveData`
/// entry it references.
#[derive(Debug, Clone, PartialEq)]
pub struct ItemSlot {
    /// Which of the container's `SlotNum` slots this entry occupies --
    /// `Slots` itself is a sparse array, so this is not the same as this
    /// entry's position within it. See module docs.
    pub slot_index: i32,
    pub count: i32,
    pub static_id: String,
    /// `Some` when this item is a dynamically-generated instance (e.g. an
    /// unhatched egg) referencing a real `DynamicItemSaveData` entry;
    /// `None` when both guids are zero, this fixture's convention (shared
    /// with every other zero-guid-means-absent field in this crate) for a
    /// plain static item with no dynamic identity.
    pub dynamic_item_id: Option<ItemSlotDynamicId>,
    /// Bytes after the dynamic id pair, kept opaque rather than guessed at.
    /// 336 of this fixture's 339 occupied slots trail with 20 bytes
    /// regardless of `static_id` length; the other 3 -- durability-tracked
    /// equipment (`ClothArmor`, `Shield_01`, `Glider_Old`), all with a
    /// plain zero-guid `dynamic_item_id` -- trail with 25. No other length
    /// occurs in this fixture (see `tests/rawdata_test.rs`'s
    /// `item_slot_decodes_every_slot_across_every_container_with_a_two_shape_trailer_and_is_sparse`).
    pub trailing: Vec<u8>,
}

/// Decodes a populated container's per-slot `RawData`. Empty bytes decode
/// to `None`, mirroring every other "maybe absent" `RawData` field in this
/// crate -- the real fixture's `Slots` arrays never contain an empty-bytes
/// entry (unoccupied slots are simply missing from the sparse array, see
/// module docs), so this guard is defensive/for-consistency rather than
/// something the fixture itself exercises.
pub fn decode_item_slot(raw: &[u8]) -> Result<Option<ItemSlot>, RawDataError> {
    if raw.is_empty() {
        return Ok(None);
    }

    let mut r = Reader::new(raw, "ItemContainerSaveData.Value.Slots.Slots.RawData");

    r.push("slot_index");
    let slot_index = r.i32()?;
    r.pop();

    r.push("count");
    let count = r.i32()?;
    r.pop();

    r.push("static_id");
    let static_id = r.fstring()?;
    r.pop();

    r.push("dynamic_id");
    let created_world_id = r.guid()?;
    let local_id_in_created_world = r.guid()?;
    r.pop();
    let dynamic_item_id = if created_world_id == [0u8; 16] && local_id_in_created_world == [0u8; 16]
    {
        None
    } else {
        Some(ItemSlotDynamicId {
            created_world_id,
            local_id_in_created_world,
        })
    };

    let trailing = r.rest().to_vec();

    Ok(Some(ItemSlot {
        slot_index,
        count,
        static_id,
        dynamic_item_id,
        trailing,
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_bytes_decode_to_none() {
        assert_eq!(decode_item_container_permission(&[]).unwrap(), None);
    }

    #[test]
    fn decodes_populated_permission_arrays() {
        let mut data = Vec::new();
        data.extend_from_slice(&1u32.to_le_bytes());
        data.push(9); // type_a = [9]
        data.extend_from_slice(&0u32.to_le_bytes()); // type_b = []
        data.extend_from_slice(&0u32.to_le_bytes()); // item_static_ids = []

        let decoded = decode_item_container_permission(&data).unwrap().unwrap();
        assert_eq!(decoded.type_a, vec![9]);
        assert!(decoded.type_b.is_empty());
        assert!(decoded.item_static_ids.is_empty());
    }

    #[test]
    fn truncated_type_a_errors_naming_the_path_no_panic() {
        let mut data = Vec::new();
        data.extend_from_slice(&5u32.to_le_bytes()); // claims 5 bytes
        data.push(1); // only 1 supplied

        let err = decode_item_container_permission(&data).unwrap_err();
        assert_eq!(
            err.path,
            "ItemContainerSaveData.Value.RawData.permission.type_a"
        );
        // Caught up front by the count/remaining-bytes check rather than a
        // per-element `take()` truncation once inside the loop.
        assert!(err.message.contains("exceeds"));
    }

    #[test]
    fn hostile_type_a_count_errors_instead_of_overflowing_capacity() {
        // Claims ~4B bytes against a payload with only 4 bytes left --
        // must error, not overflow/panic computing `Vec::with_capacity`.
        let mut data = Vec::new();
        data.extend_from_slice(&u32::MAX.to_le_bytes());
        data.extend_from_slice(&[0u8; 4]);

        let err = decode_item_container_permission(&data).unwrap_err();
        assert_eq!(
            err.path,
            "ItemContainerSaveData.Value.RawData.permission.type_a"
        );
        assert!(err.message.contains("exceeds"));
    }

    fn item_slot_bytes(
        slot_index: i32,
        count: i32,
        static_id: &str,
        trailing_len: usize,
    ) -> Vec<u8> {
        let mut data = Vec::new();
        data.extend_from_slice(&slot_index.to_le_bytes());
        data.extend_from_slice(&count.to_le_bytes());
        data.extend_from_slice(&ascii_fstring(static_id));
        data.extend_from_slice(&[0u8; 32]); // dynamic_id: zero guids
        data.extend_from_slice(&vec![0u8; trailing_len]);
        data
    }

    #[test]
    fn item_slot_empty_bytes_decode_to_none() {
        assert_eq!(decode_item_slot(&[]).unwrap(), None);
    }

    #[test]
    fn item_slot_decodes_a_static_item_with_no_dynamic_id() {
        let data = item_slot_bytes(0, 749, "Money", 20);
        let decoded = decode_item_slot(&data).unwrap().unwrap();
        assert_eq!(decoded.slot_index, 0);
        assert_eq!(decoded.count, 749);
        assert_eq!(decoded.static_id, "Money");
        assert_eq!(decoded.dynamic_item_id, None);
        assert_eq!(decoded.trailing, vec![0u8; 20]);
    }

    #[test]
    fn item_slot_decodes_an_egg_with_a_nonzero_dynamic_id() {
        let mut data = Vec::new();
        data.extend_from_slice(&0i32.to_le_bytes()); // slot_index
        data.extend_from_slice(&1i32.to_le_bytes()); // count
        data.extend_from_slice(&ascii_fstring("PalEgg_Fire_01"));
        data.extend_from_slice(&[0u8; 16]); // created_world_id: zero
        data.extend_from_slice(&[7u8; 16]); // local_id_in_created_world: nonzero
        data.extend_from_slice(&[0u8; 20]); // trailing

        let decoded = decode_item_slot(&data).unwrap().unwrap();
        assert_eq!(
            decoded.dynamic_item_id,
            Some(ItemSlotDynamicId {
                created_world_id: [0u8; 16],
                local_id_in_created_world: [7u8; 16],
            })
        );
    }

    #[test]
    fn item_slot_truncated_static_id_errors_naming_the_path_no_panic() {
        let mut data = Vec::new();
        data.extend_from_slice(&0i32.to_le_bytes());
        data.extend_from_slice(&1i32.to_le_bytes());
        data.extend_from_slice(&[0xFFu8, 0xFF]); // truncated fstring length

        let err = decode_item_slot(&data).unwrap_err();
        assert_eq!(
            err.path,
            "ItemContainerSaveData.Value.Slots.Slots.RawData.static_id"
        );
        assert!(err.message.contains("truncated"));
    }

    #[test]
    fn item_slot_truncated_dynamic_id_errors_naming_the_path_no_panic() {
        let mut data = Vec::new();
        data.extend_from_slice(&0i32.to_le_bytes());
        data.extend_from_slice(&1i32.to_le_bytes());
        data.extend_from_slice(&ascii_fstring("Wood"));
        data.extend_from_slice(&[0u8; 10]); // only 10 of the 32 dynamic_id bytes

        let err = decode_item_slot(&data).unwrap_err();
        assert_eq!(
            err.path,
            "ItemContainerSaveData.Value.Slots.Slots.RawData.dynamic_id"
        );
        assert!(err.message.contains("truncated"));
    }
}
