meta:
  id: stove
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: activated
    type: u1
  - id: seconds_timer
    type: s4
  - id: max_temperature
    type: f4
  - id: first_turn_on
    type: u1
  - id: broken
    type: u1
