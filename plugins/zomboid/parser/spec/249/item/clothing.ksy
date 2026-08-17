meta:
  id: item_clothing
  endian: be
  imports:
    - ../../common/common

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.Clothing.save / load
# Extends: InventoryItem
seq:
  - id: flags
    type: u1
  - id: sprite_name
    type: common::string_utf
    if: (flags & 1) != 0
  - id: dirtyness
    type: f4
    if: (flags & 2) != 0
  - id: blood_level
    type: f4
    if: (flags & 4) != 0
  - id: wetness
    type: f4
    if: (flags & 8) != 0
  - id: last_wetness_update
    type: f4
    if: (flags & 16) != 0
  - id: num_patches
    type: u1
    if: (flags & 32) != 0
  - id: patches
    type: patch_data
    repeat: expr
    repeat-expr: num_patches
    if: (flags & 32) != 0

types:
  patch_data:
    seq:
      - id: tailor_lvl
        type: u1
      - id: fabric_type
        type: u1
      - id: scratch_defense
        type: u1
      - id: bite_defense
        type: u1
      - id: has_hole
        type: u1
      - id: condition_gain
        type: s2
