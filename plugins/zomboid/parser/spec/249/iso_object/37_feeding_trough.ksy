meta:
  id: feeding_trough
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
  - id: num_feeding_types
    type: s4
  - id: feeding_types
    type: feeding_type_entry
    repeat: expr
    repeat-expr: num_feeding_types
  - id: water
    type: f4
  - id: is_slave
    type: u1
  - id: linked_x
    type: s4
    if: is_slave == 1
  - id: linked_y
    type: s4
    if: is_slave == 1
  - id: north
    type: u1
types:
  feeding_type_entry:
    seq:
      - id: food_type
        type: common::string_utf
      - id: amount
        type: f4
