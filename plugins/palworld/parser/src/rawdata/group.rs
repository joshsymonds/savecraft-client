//! `GroupSaveDataMap` value `RawData`: a group's (guild's) own id, its
//! display name, and the roster of characters (players + pals) that
//! belong to it. This prefix is common to every `EPalGroupType` variant.
//!
//! Ported from cheahjs/palworld-save-tools `rawdata/group.py` (git main).
//! Upstream additionally decodes group-type-specific fields after the
//! roster (`org_type`/`base_ids` for Guild/IndependentGuild/Organization
//! groups, plus more for Guild/IndependentGuild specifically). Attempting
//! that against this fixture produced array counts in the tens of
//! millions -- clearly not real lengths for a payload only a few dozen
//! bytes long -- so this build's encoding of those fields doesn't match
//! upstream's assumptions closely enough to trust a guess (see the
//! contract's "never guess" rule). The common prefix below is verified
//! byte-for-byte against the real fixture, including an
//! `EPalGroupType::Guild` entry whose `group_id` here matches its
//! `GroupSaveDataMap` key exactly and whose roster size matches the
//! save's player + pal count; everything after the roster is kept as
//! opaque `trailing` bytes rather than decoded further.

use super::reader::{RawDataError, Reader};

/// A decoded `GroupSaveDataMap` value's `RawData`.
#[derive(Debug, Clone, PartialEq)]
pub struct GroupSaveData {
    pub group_id: [u8; 16],
    pub group_name: String,
    /// `(character_guid, instance_id)` pairs, one per member.
    pub member_handle_ids: Vec<([u8; 16], [u8; 16])>,
    /// Group-type-specific fields (org/guild membership details per
    /// upstream) this build's encoding doesn't match closely enough to
    /// decode with confidence -- see module docs.
    pub trailing: Vec<u8>,
}

pub fn decode_group_save_data(raw: &[u8]) -> Result<GroupSaveData, RawDataError> {
    let mut r = Reader::new(raw, "GroupSaveDataMap.Value.RawData");

    r.push("group_id");
    let group_id = r.guid()?;
    r.pop();

    r.push("group_name");
    let group_name = r.fstring()?;
    r.pop();

    r.push("individual_character_handle_ids");
    // Each member is a (character_guid, instance_id) pair: 32 bytes minimum.
    let count = r.count(32)?;
    let mut member_handle_ids = Vec::with_capacity(count);
    for _ in 0..count {
        let guid = r.guid()?;
        let instance_id = r.guid()?;
        member_handle_ids.push((guid, instance_id));
    }
    r.pop();

    let trailing = r.rest().to_vec();

    Ok(GroupSaveData {
        group_id,
        group_name,
        member_handle_ids,
        trailing,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_empty_roster() {
        let mut data = vec![0u8; 16]; // group_id
        data.extend_from_slice(&0i32.to_le_bytes()); // group_name = ""
        data.extend_from_slice(&0u32.to_le_bytes()); // handle count = 0

        let decoded = decode_group_save_data(&data).unwrap();
        assert_eq!(decoded.group_id, [0u8; 16]);
        assert_eq!(decoded.group_name, "");
        assert!(decoded.member_handle_ids.is_empty());
        assert!(decoded.trailing.is_empty());
    }

    #[test]
    fn truncated_handle_ids_errors_naming_the_path_no_panic() {
        let mut data = vec![0u8; 16]; // group_id
        data.extend_from_slice(&0i32.to_le_bytes()); // group_name = ""
        data.extend_from_slice(&1u32.to_le_bytes()); // claims 1 member
        data.extend_from_slice(&[0u8; 10]); // but only 10 of the 32 bytes needed

        let err = decode_group_save_data(&data).unwrap_err();
        assert_eq!(
            err.path,
            "GroupSaveDataMap.Value.RawData.individual_character_handle_ids"
        );
        // Caught up front by the count/remaining-bytes check rather than a
        // per-element `take()` truncation once inside the loop.
        assert!(err.message.contains("exceeds"));
    }

    #[test]
    fn hostile_handle_count_errors_instead_of_overflowing_capacity() {
        // Claims ~134M members against a payload with only 8 bytes left --
        // must error, not overflow/panic computing
        // `Vec::with_capacity(count * 32)` on a 32-bit target.
        let mut data = vec![0u8; 16]; // group_id
        data.extend_from_slice(&0i32.to_le_bytes()); // group_name = ""
        data.extend_from_slice(&0x0800_0000u32.to_le_bytes()); // hostile count
        data.extend_from_slice(&[0u8; 8]);

        let err = decode_group_save_data(&data).unwrap_err();
        assert_eq!(
            err.path,
            "GroupSaveDataMap.Value.RawData.individual_character_handle_ids"
        );
        assert!(err.message.contains("exceeds"));
    }
}
