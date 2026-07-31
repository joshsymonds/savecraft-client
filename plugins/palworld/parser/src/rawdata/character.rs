//! `CharacterSaveParameterMap` value `RawData`: a character's (player or
//! pal) own stat block plus which group (guild) it belongs to.
//!
//! Ported from cheahjs/palworld-save-tools `rawdata/character.py` (git
//! main), with one correction the fixture itself demands: the published
//! PyPI release's decoder reads a 4-byte unknown field followed directly
//! by the 16-byte group guid (20 bytes total) and asserts EOF immediately
//! after -- which raises "EOF not reached" against every entry in this
//! save (build 24467282). Decoding this fixture's `CharacterSaveParameterMap`
//! (37 entries, covering the host player and every owned pal) shows a
//! consistent 24-byte trailer instead: the same 4 unknown bytes, the same
//! 16-byte group guid (verified to match a real `GroupSaveDataMap` key
//! exactly), plus 4 more bytes this build appends. Rather than hard-coding
//! either length, this decodes the two fields whose position and meaning
//! are unambiguous and keeps everything after the guid as opaque
//! `trailing` bytes.

use super::properties::{PalProperty, PalValue, read_properties_until_none};
#[cfg(test)]
use super::reader::test_support::ascii_fstring;
use super::reader::{RawDataError, Reader};

/// A decoded `CharacterSaveParameterMap` value's `RawData`.
#[derive(Debug, Clone, PartialEq)]
pub struct CharacterSaveParameter {
    /// The nested `SaveParameter` property tree carrying the character's
    /// own fields (`Level`, `NickName`, `CharacterID`, `IsPlayer`,
    /// `OwnerPlayerUId`, ...).
    pub object: Vec<PalProperty>,
    /// 4 bytes of unknown/reserved data immediately preceding `group_id`.
    pub unknown: [u8; 4],
    /// The character's group (guild) id -- matches a `GroupSaveDataMap`
    /// key for characters that belong to one.
    pub group_id: [u8; 16],
    /// Bytes after `group_id` this build's format revision appends, of
    /// undetermined structure.
    pub trailing: Vec<u8>,
}

pub fn decode_character_save_parameter(raw: &[u8]) -> Result<CharacterSaveParameter, RawDataError> {
    let mut r = Reader::new(raw, "CharacterSaveParameterMap.Value.RawData");
    // The whole payload is itself one None-terminated property list
    // holding a single entry: a `SaveParameter` struct wrapping the
    // character's actual fields. Unwrap it so callers see the fields
    // directly rather than needing to know about this wrapper.
    let top_level = read_properties_until_none(&mut r)?;
    let object = match top_level.into_iter().find(|p| p.name == "SaveParameter") {
        Some(PalProperty {
            value: PalValue::Struct(inner),
            ..
        }) => inner,
        _ => {
            return Err(r.error(
                "expected a top-level \"SaveParameter\" struct property wrapping the character's fields",
            ))
        }
    };

    r.push("trailer");
    let unknown: [u8; 4] = r.bytes(4)?.try_into().unwrap();
    let group_id = r.guid()?;
    r.pop();
    let trailing = r.rest().to_vec();

    Ok(CharacterSaveParameter {
        object,
        unknown,
        group_id,
        trailing,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A minimal but real-shaped `RawData` payload: a `SaveParameter`
    /// struct wrapping an empty property list, i.e. what every real
    /// `CharacterSaveParameterMap` entry's `RawData` looks like before its
    /// trailer.
    fn save_parameter_wrapper() -> Vec<u8> {
        let inner_none = ascii_fstring("None");
        let mut data = Vec::new();
        data.extend_from_slice(&ascii_fstring("SaveParameter")); // property name
        data.extend_from_slice(&ascii_fstring("StructProperty")); // property type
        data.extend_from_slice(&(inner_none.len() as u32).to_le_bytes()); // size
        data.extend_from_slice(&0u32.to_le_bytes()); // array index
        data.extend_from_slice(&ascii_fstring("PalIndividualCharacterSaveParameter")); // struct_type
        data.extend_from_slice(&[0u8; 16]); // struct_id guid
        data.push(0); // no optional guid
        data.extend_from_slice(&inner_none); // empty nested property list
        data.extend_from_slice(&ascii_fstring("None")); // top-level terminator
        data
    }

    #[test]
    fn truncated_before_group_id_errors_naming_the_path_no_panic() {
        // A well-formed SaveParameter wrapper, then only 2 of the 4
        // unknown trailer bytes.
        let mut data = save_parameter_wrapper();
        data.extend_from_slice(&[0u8, 0]);

        let err = decode_character_save_parameter(&data).unwrap_err();
        assert_eq!(err.path, "CharacterSaveParameterMap.Value.RawData.trailer");
        assert!(err.message.contains("truncated"));
    }

    #[test]
    fn missing_save_parameter_wrapper_errors_instead_of_misreading_the_trailer() {
        // A top-level property list with no "SaveParameter" entry at all.
        let data = ascii_fstring("None");
        let err = decode_character_save_parameter(&data).unwrap_err();
        assert!(err.message.contains("SaveParameter"));
    }

    #[test]
    fn empty_bytes_error_instead_of_decoding_to_a_default() {
        // Unlike character_container/item_container/dynamic_item, a
        // `CharacterSaveParameterMap` entry's `RawData` is never legitimately
        // empty -- every real entry carries at least the `SaveParameter`
        // wrapper. Empty input should error, not silently produce a
        // default-valued character.
        assert!(decode_character_save_parameter(&[]).is_err());
    }
}
