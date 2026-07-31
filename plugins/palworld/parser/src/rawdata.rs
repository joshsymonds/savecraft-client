//! Decoders for Palworld's game-native `RawData` byte payloads: the
//! `ArrayProperty(Byte)` blobs uesave hands back as opaque bytes because
//! they're serialized by Palworld's own code, not by GVAS's generic
//! property machinery. This module only decodes (never encodes) -- the
//! plugin never writes saves.
//!
//! Ported from cheahjs/palworld-save-tools `palworld_save_tools/rawdata/*.py`
//! (git main). uesave's own archive reader (`SaveGameArchive`) is private
//! to that crate, so the byte-level reads here are hand-rolled against the
//! same wire format uesave itself parses -- see `reader::Reader`.
//!
//! CAUTION: the published PyPI release of palworld-save-tools is stale for
//! this fixture's save-format revision (build 24467282) -- its character
//! codec raises "EOF not reached" against these exact bytes. Where the
//! real fixture and even upstream's git-main assumptions disagree, the
//! fixture wins: every codec here decodes the fields verified
//! byte-for-byte against `testdata/.../Level.sav`, and leaves anything
//! past that point as opaque `trailing` bytes rather than guessing at
//! further structure. See each submodule's doc comment for what was
//! verified and what wasn't.
//!
//! `sections.rs` is this module's real consumer -- see
//! `tests/rawdata_test.rs` for fixture-backed coverage of the decoders
//! themselves, and each submodule's own unit tests (including its
//! empty-input contract) for coverage that doesn't need the real fixture.

mod character;
mod character_container;
mod dynamic_item;
mod group;
mod item_container;
mod properties;
mod reader;

pub use character::{CharacterSaveParameter, decode_character_save_parameter};
pub use character_container::{CharacterContainerSlot, decode_character_container_slot};
pub use dynamic_item::{DynamicItem, DynamicItemId, DynamicItemKind, decode_dynamic_item};
pub use group::{GroupSaveData, decode_group_save_data};
pub use item_container::{ItemSlot, ItemSlotDynamicId, decode_item_slot};
pub use properties::{PalProperty, PalValue};
pub use reader::{RawDataError, RawDataErrorKind};
