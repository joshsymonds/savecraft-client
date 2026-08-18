meta:
  id: door
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
  - id: locked
    type: u1
  - id: north
    type: u1
  - id: health
    type: s4
  - id: max_health
    type: s4
  - id: closed_sprite_id
    type: s4
  - id: open_sprite_id
    type: s4
  - id: key_id
    type: s4
  - id: locked_by_key
    type: u1
  - id: curtain_flags
    type: u1
instances:
  has_curtain:
    value: (curtain_flags & 1) != 0
  curtain_open:
    value: (curtain_flags & 2) != 0
  curtain_inside:
    value: (curtain_flags & 4) != 0
