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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn short_model_payload_reports_the_confirmed_base_id_path() {
        let error = decode_base_id(&[0; 47]).unwrap_err();
        assert_eq!(error.path, "MapObjectSaveData.Model.RawData");
    }

    #[test]
    fn short_module_payload_preserves_the_callers_path() {
        let error = decode_module_guid(&[0; 15], "test.module.RawData").unwrap_err();
        assert_eq!(error.path, "test.module.RawData");
    }
}
