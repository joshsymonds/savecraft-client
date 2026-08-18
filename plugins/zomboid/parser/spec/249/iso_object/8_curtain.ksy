meta:
  id: curtain
  endian: be
  imports:
    - ../../common/common
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
  - id: barricade_strength
    type: s4
  # If open, the sprite read is closedSprite; otherwise it's openSprite
  # The other sprite is set from the base object's sprite
  - id: other_sprite_id
    type: s4
