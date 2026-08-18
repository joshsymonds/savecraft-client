meta:
  id: thumpable
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
  - id: bit_header
    type: u8
    doc: "BitHeader with Long size (8 bytes)"
  - id: max_health
    type: s4
    if: (bit_header & 8) != 0
  - id: health
    type: s4
    if: (bit_header & 16) != 0
  - id: closed_sprite_id
    type: s4
    if: (bit_header & 32) != 0
  - id: open_sprite_id
    type: s4
    if: (bit_header & 64) != 0
  - id: thump_dmg
    type: s4
    if: (bit_header & 128) != 0
  - id: cross_speed
    type: f4
    if: (bit_header & 1048576) != 0
  - id: table
    type: common::ktable
    if: (bit_header & 2097152) != 0
  - id: mod_data
    type: common::ktable
    if: (bit_header & 4194304) != 0
  - id: light_source_life
    type: s4
    if: (bit_header & 67108864) != 0
  - id: light_source_radius
    type: s4
    if: (bit_header & 134217728) != 0
  - id: light_source_x_offset
    type: s4
    if: (bit_header & 268435456) != 0
  - id: light_source_y_offset
    type: s4
    if: (bit_header & 536870912) != 0
  - id: light_source_fuel_registry_id
    type: s2
    if: (bit_header & 1073741824) != 0
  - id: life_delta
    type: f4
    if: (bit_header & 2147483648) != 0
  - id: life_left
    type: f4
    if: (bit_header & 4294967296) != 0
  - id: key_id
    type: s4
    if: (bit_header & 8589934592) != 0
  - id: locked_by_code
    type: s4
    if: (bit_header & 137438953472) != 0
  - id: raw_thump_sound
    type: common::string_utf
    if: (bit_header & 274877906944) != 0
  - id: last_update_hours
    type: f4
    if: (bit_header & 549755813888) != 0
  - id: build_materials
    type: common::ktable
    if: (bit_header & 4398046511104) != 0
instances:
  open:
    value: (bit_header & 1) != 0
  locked:
    value: (bit_header & 2) != 0
  north:
    value: (bit_header & 4) != 0
  is_door:
    value: (bit_header & 512) != 0
  is_door_frame:
    value: (bit_header & 1024) != 0
  is_corner:
    value: (bit_header & 2048) != 0
  is_stairs:
    value: (bit_header & 4096) != 0
  is_container:
    value: (bit_header & 8192) != 0
  is_floor:
    value: (bit_header & 16384) != 0
  can_barricade:
    value: (bit_header & 32768) != 0
  can_pass_through:
    value: (bit_header & 65536) != 0
  dismantable:
    value: (bit_header & 131072) != 0
  can_be_plastered:
    value: (bit_header & 262144) != 0
  paintable:
    value: (bit_header & 524288) != 0
  block_all_the_square:
    value: (bit_header & 8388608) != 0
  is_thumpable:
    value: (bit_header & 16777216) != 0
  is_hoppable:
    value: (bit_header & 33554432) != 0
  locked_by_key:
    value: (bit_header & 17179869184) != 0
  locked_by_padlock:
    value: (bit_header & 34359738368) != 0
  can_be_lock_by_padlock:
    value: (bit_header & 68719476736) != 0
  have_fuel:
    value: (bit_header & 1099511627776) != 0
  light_source_on:
    value: (bit_header & 2199023255552) != 0
  thump_sound_value:
    value: '(bit_header & 274877906944) != 0 ? raw_thump_sound.value : ""'
  thump_sound:
    value: 'thump_sound_value == "ZombieThumpGeneric" ? "thumpa2" :
        (thump_sound_value == "ZombieThumpMetal" ? "metalthump" : thump_sound_value)'
