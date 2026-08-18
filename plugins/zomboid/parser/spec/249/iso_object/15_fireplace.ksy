meta:
  id: fireplace
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: fuel_amount_minutes
    type: s4
  - id: lit
    type: u1
  - id: last_update_time
    type: f4
  - id: minutes_since_extinguished
    type: s4
