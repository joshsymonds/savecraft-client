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
//! This module is gated behind `#[cfg(test)]` in `main.rs`: section
//! builders that consume these decoders are a follow-on task, so nothing
//! here is wired into the plugin's real output yet. A follow-on task
//! removes that gate when it does the wiring. In the meantime, this
//! module is fully implemented and tested -- see `tests/rawdata_test.rs`
//! for the real, fixture-backed coverage, and `keep_alive` below for why
//! this file also references every decoder once itself.

// Explicit `#[path]` on every submodule (rather than the usual implicit
// `rawdata/foo.rs` lookup) so this file resolves identically whether it's
// loaded normally (`mod rawdata;` in main.rs) or pulled into
// `tests/rawdata_test.rs` via `#[path]` -- an explicitly-`#[path]`'d file
// only looks for its *own* children as siblings in its containing
// directory by default, not in a `<stem>/` subdirectory, so the implicit
// form breaks under the latter.
#[path = "rawdata/character.rs"]
mod character;
#[path = "rawdata/character_container.rs"]
mod character_container;
#[path = "rawdata/dynamic_item.rs"]
mod dynamic_item;
#[path = "rawdata/group.rs"]
mod group;
#[path = "rawdata/item_container.rs"]
mod item_container;
#[path = "rawdata/properties.rs"]
mod properties;
#[path = "rawdata/reader.rs"]
mod reader;

pub use character::{CharacterSaveParameter, decode_character_save_parameter};
pub use character_container::{CharacterContainerSlot, decode_character_container_slot};
pub use dynamic_item::{DynamicItem, DynamicItemId, DynamicItemKind, decode_dynamic_item};
pub use group::{GroupSaveData, decode_group_save_data};
pub use item_container::{ItemContainerPermission, decode_item_container_permission};
pub use properties::{PalProperty, PalValue};
pub use reader::RawDataError;

// Because `main.rs` only pulls this module in under `#[cfg(test)]` (see
// above), `cargo test`'s "unittests src/main.rs" target compiles it with
// nothing in that same compilation calling these `pub` decoders --
// `tests/rawdata_test.rs`, which has the real coverage, is a separate
// crate from that target's perspective. Without a real call site *inside*
// this compilation, `-D warnings` (specifically `dead_code`) fails the
// build. This module references each public decoder once so that doesn't
// happen; the substantive pinned-value and truncation assertions live in
// `tests/rawdata_test.rs` and each submodule's own unit tests.
#[cfg(test)]
mod keep_alive {
    use super::*;

    #[test]
    fn every_public_decoder_is_reachable() {
        // Empty input is a real, documented contract for these decoders
        // (see each's own doc comment), not just a way to touch them once
        // for `-D warnings` -- assert it, not merely call it. The type
        // annotations (rather than `let _ = ...`) are what keep the
        // re-exports above from being flagged as unused imports in this
        // crate, which has no other consumer of them until a follow-on
        // task wires section builders to this module (see the module doc
        // comment).
        let character: Result<CharacterSaveParameter, RawDataError> =
            decode_character_save_parameter(&[]);
        assert!(character.is_err());

        let slot: Result<Option<CharacterContainerSlot>, RawDataError> =
            decode_character_container_slot(&[]);
        assert_eq!(slot, Ok(None));

        let group: Result<GroupSaveData, RawDataError> = decode_group_save_data(&[]);
        assert!(group.is_err());

        let permission: Result<Option<ItemContainerPermission>, RawDataError> =
            decode_item_container_permission(&[]);
        assert_eq!(permission, Ok(None));

        let item: Result<Option<DynamicItem>, RawDataError> = decode_dynamic_item(&[]);
        assert_eq!(item, Ok(None));

        let _: Option<fn(&PalProperty) -> &PalValue> = None;
        let _: Option<fn(&DynamicItemKind) -> &DynamicItemId> = None;
    }
}
