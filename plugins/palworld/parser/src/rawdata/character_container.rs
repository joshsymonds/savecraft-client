//! `CharacterContainerSaveData` value `Slots.Slots.RawData`: which
//! character occupies one slot of a character container (e.g. a base
//! camp's pal box), and which player's container it's filed under.
//!
//! Ported from cheahjs/palworld-save-tools `rawdata/character_container.py`
//! (git main), which reads exactly `player_uid(16) + instance_id(16) +
//! permission_tribe_id(1 byte)` = 33 bytes and asserts EOF. This fixture's
//! occupied slots are 38 bytes each -- 5 more than that -- so everything
//! from `permission_tribe_id` onward (1 meaningful byte plus whatever
//! padding this build appends) is kept as opaque `trailing` bytes rather
//! than guessed at. `player_uid` was verified to match the save's host
//! player id across every occupied slot in the fixture; `instance_id`
//! varies per slot, matching individual pals' `InstanceId` in
//! `CharacterSaveParameterMap`.
//!
//! Note: this is *not* the top-level `CharacterContainerSaveData.Value.RawData`
//! field (which upstream's own `paltypes.py` notes "isn't actually
//! serialised into at all" -- confirmed empty on every entry in this
//! fixture) -- it's the per-slot field nested under `Slots`.

use super::reader::{RawDataError, Reader};

/// A decoded, occupied container slot's `RawData`.
#[derive(Debug, Clone, PartialEq)]
pub struct CharacterContainerSlot {
    pub player_uid: [u8; 16],
    pub instance_id: [u8; 16],
    /// `permission_tribe_id` (1 byte) plus whatever padding this build
    /// appends, left undecoded -- see module docs.
    pub trailing: Vec<u8>,
}

/// Decodes a slot's `RawData`. Empty bytes are a valid, common case (most
/// container slots are unoccupied), mirroring upstream's own
/// `len(c_bytes) == 0 => None` guard.
pub fn decode_character_container_slot(
    raw: &[u8],
) -> Result<Option<CharacterContainerSlot>, RawDataError> {
    if raw.is_empty() {
        return Ok(None);
    }

    let mut r = Reader::new(raw, "CharacterContainerSaveData.Value.Slots.Slots.RawData");
    r.push("player_uid");
    let player_uid = r.guid()?;
    r.pop();
    r.push("instance_id");
    let instance_id = r.guid()?;
    r.pop();
    let trailing = r.rest().to_vec();

    Ok(Some(CharacterContainerSlot {
        player_uid,
        instance_id,
        trailing,
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_bytes_decode_to_none() {
        assert_eq!(decode_character_container_slot(&[]).unwrap(), None);
    }

    #[test]
    fn truncated_instance_id_errors_naming_the_path_no_panic() {
        // A full player_uid but only 4 of the 16 instance_id bytes.
        let mut data = vec![0u8; 16];
        data.extend_from_slice(&[1, 2, 3, 4]);

        let err = decode_character_container_slot(&data).unwrap_err();
        assert_eq!(
            err.path,
            "CharacterContainerSaveData.Value.Slots.Slots.RawData.instance_id"
        );
        assert!(err.message.contains("truncated"));
    }
}
