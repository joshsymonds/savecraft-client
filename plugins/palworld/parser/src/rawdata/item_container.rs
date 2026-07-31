//! `ItemContainerSaveData` value `RawData`: which item types this
//! container is allowed to hold. This is a permission/allow-list blob,
//! *not* the container's actual contents -- those live in the container's
//! `Slots` property, decoded as ordinary typed GVAS properties (not
//! `RawData`) by uesave already.
//!
//! Ported from cheahjs/palworld-save-tools `rawdata/item_container.py`
//! (git main). Unlike `character`/`character_container`/`group`, upstream's
//! own decoder here already tolerates leftover bytes after the three
//! arrays (`trailing_unparsed_data`, no assertion) rather than requiring
//! exact EOF, and every entry decoded against the real fixture (264
//! containers, several with non-empty `type_a`/`type_b`/`item_static_ids`)
//! matched that shape with no divergence -- so no fixture-specific
//! surprises here.

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
}
