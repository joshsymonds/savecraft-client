//! Confirmed fixed-width references in map-object module payloads.

use super::reader::{RawDataError, Reader};

pub fn decode_base_id(raw: &[u8]) -> Result<[u8; 16], RawDataError> {
    let mut r = Reader::new(raw, "MapObjectSaveData.Model.RawData");
    r.bytes(32)?;
    r.guid()
}

pub fn decode_module_guid(raw: &[u8], path: &str) -> Result<[u8; 16], RawDataError> {
    let mut r = Reader::new(raw, path);
    r.guid()
}
