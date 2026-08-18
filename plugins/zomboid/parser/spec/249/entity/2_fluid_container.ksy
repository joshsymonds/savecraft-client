meta:
  id: fluid_container
  endian: be
  imports:
    - ../../common/common
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.fluids.FluidContainer.load / save
  - id: header
    type: u2
  - id: capacity
    type: f4
    if: (header & 1) != 0
  # fluids
  - id: single_fluid
    type: fluid_instance
    if: (header & 2) != 0 and (header & 4) != 0
  - id: num_fluids
    type: u1
    if: (header & 2) != 0 and (header & 4) == 0
  - id: fluids
    type: fluid_instance
    repeat: expr
    repeat-expr: num_fluids
    if: (header & 2) != 0 and (header & 4) == 0
  # filters
  - id: whitelist
    type: fluid_filter
    if: (header & 8) != 0
  - id: blacklist
    type: fluid_filter
    if: (header & 16) != 0
  # container name
  - id: raw_container_name
    type: common::string_utf
    if: (header & 128) != 0
  # rain catcher
  - id: rain_catcher
    type: f4
    if: (header & 512) != 0
instances:
  input_locked:
    value: (header & 32) != 0
  can_player_empty:
    value: (header & 64) != 0
  hidden_amount:
    value: (header & 256) != 0
  container_name:
    value: '(header & 128) != 0 ? raw_container_name.value : "FluidContainer"'
types:
  fluid_instance:
    seq:
      - id: header
        type: u1
      - id: fluid_id
        type: u1
        if: (header & 1) != 0
      - id: fluid_string
        type: common::string_utf
        if: (header & 1) == 0 and (header & 2) != 0
      - id: custom_color
        type: common::color_rgb
        if: (header & 4) != 0
      - id: amount
        type: f4
  fluid_filter:
    seq:
      - id: filter_type
        type: u1
      - id: num_enums
        type: u1
      - id: enums
        type: u1
        repeat: expr
        repeat-expr: num_enums
      - id: num_strings
        type: u1
      - id: strings
        type: common::string_utf
        repeat: expr
        repeat-expr: num_strings
      - id: num_categories
        type: u1
      - id: categories
        type: u1
        repeat: expr
        repeat-expr: num_categories
    instances:
      is_whitelist:
        value: filter_type == 1
      is_blacklist:
        value: filter_type == 0
