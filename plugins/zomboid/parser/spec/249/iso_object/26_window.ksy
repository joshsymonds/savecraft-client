meta:
  id: window
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: open
    type: u1
  - id: north
    type: u1
  - id: health
    type: s4
  - id: locked
    type: u1
  - id: perma_locked
    type: u1
  - id: destroyed
    type: u1
  - id: glass_removed
    type: u1
  - id: has_open_sprite
    type: u1
  - id: open_sprite_id
    type: s4
    if: has_open_sprite == 1
  - id: has_closed_sprite
    type: u1
  - id: closed_sprite_id
    type: s4
    if: has_closed_sprite == 1
  - id: has_smashed_sprite
    type: u1
  - id: smashed_sprite_id
    type: s4
    if: has_smashed_sprite == 1
  - id: has_glass_removed_sprite
    type: u1
  - id: glass_removed_sprite_id
    type: s4
    if: has_glass_removed_sprite == 1
  - id: max_health
    type: s4
