meta:
  id: compost
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: compost
    type: f4
  - id: last_updated
    type: f4
  - id: health
    type: s4
    if: world_version >= 213
  - id: max_health
    type: s4
    if: world_version >= 213
