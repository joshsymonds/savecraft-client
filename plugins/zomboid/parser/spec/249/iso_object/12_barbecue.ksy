meta:
  id: barbecue
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: has_propane_tank
    type: u1
  - id: fuel_amount_minutes
    type: s4
  - id: lit
    type: u1
  - id: last_update_time
    type: f4
  - id: minutes_since_extinguished
    type: s4
  - id: has_normal_sprite
    type: u1
  - id: normal_sprite_id
    type: s4
    if: has_normal_sprite == 1
  - id: has_no_tank_sprite
    type: u1
  - id: no_tank_sprite_id
    type: s4
    if: has_no_tank_sprite == 1
