meta:
  id: zombie_character
  endian: be
  imports:
    - character_shared

params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1

seq:
  - id: loaded_file_version
    type: s4
  - id: time_since_seen_flesh
    type: s4
  - id: state_flags
    type: s4
  - id: num_worn_items
    type: u1
  - id: worn_items
    type: character_shared::worn_item_entry
    repeat: expr
    repeat-expr: num_worn_items
