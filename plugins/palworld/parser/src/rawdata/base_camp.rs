//! Confirmed `BaseCampSaveData.Value.RawData` fields.

use super::reader::{RawDataError, Reader};

#[derive(Debug, Clone, PartialEq)]
pub struct BaseCamp {
    pub base_id: [u8; 16],
    pub name: String,
    pub position: [f64; 3],
    pub guild_id: [u8; 16],
    pub trailing: Vec<u8>,
}

pub fn decode_base_camp(raw: &[u8]) -> Result<BaseCamp, RawDataError> {
    let mut r = Reader::new(raw, "BaseCampSaveData.Value.RawData");
    let base_id = r.guid()?;
    let name = r.fstring()?;
    r.bytes(33)?; // [56..89], deliberately opaque
    let position = [r.f64()?, r.f64()?, r.f64()?];
    r.bytes(28)?; // [113..141], deliberately opaque
    let guild_id = r.guid()?;
    Ok(BaseCamp {
        base_id,
        name,
        position,
        guild_id,
        trailing: r.rest().to_vec(),
    })
}
