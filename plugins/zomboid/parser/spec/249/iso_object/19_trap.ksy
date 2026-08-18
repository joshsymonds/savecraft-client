meta:
  id: trap
  endian: be
  imports:
    - ../../common/common
    - ../inventory
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: sensor_range
    type: s4
  - id: fire_starting_chance
    type: s4
  - id: fire_starting_energy
    type: s4
  - id: fire_range
    type: s4
  - id: explosion_power
    type: s4
  - id: explosion_range
    type: s4
  - id: explosion_duration
    type: s4
    if: world_version >= 219
  - id: explosion_start_time
    type: f4
    if: world_version >= 219
  - id: smoke_range
    type: s4
  - id: noise_range
    type: s4
  - id: noise_duration
    type: s4
  - id: noise_start_time
    type: f4
  - id: extra_damage
    type: f4
  - id: remote_control_id
    type: s4
  - id: timer_before_explosion
    type: s4
  - id: count_down_sound
    type: common::string_utf
  - id: raw_explosion_sound
    type: common::string_utf
  - id: has_weapon
    type: u1
  - id: weapon
    type: inventory::sized_blob(context, world_version)
    if: has_weapon == 1
instances:
  explosion_sound:
    value: '(raw_explosion_sound.value == "bigExplosion") ? "BigExplosion" : 
      (raw_explosion_sound.value == "smallExplosion") ? "SmallExplosion" : 
      (raw_explosion_sound.value == "feedback") ? "NoiseTrapExplosion" : raw_explosion_sound.value'
