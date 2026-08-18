meta:
  id: animal_track
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
  - id: animal_type
    type: common::string_utf
  - id: track_type
    type: common::string_utf
  - id: x
    type: s4
  - id: y
    type: s4
  - id: has_dir
    type: u1
  - id: dir_index
    type: s4
    if: has_dir == 1
  - id: added_time
    type: s8
  - id: added_to_world
    type: u1
