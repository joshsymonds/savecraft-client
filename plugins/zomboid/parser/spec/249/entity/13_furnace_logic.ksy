meta:
  id: furnace_logic
  endian: be
  imports:
    - ../../common/common
    - entity_shared
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.crafting.FurnaceLogic.load / save
  - id: header
    type: u2
  - id: furnace_recipe_tag_query
    type: common::string_utf
    if: (header & 1) != 0
  - id: fuel_recipe_tag_query
    type: common::string_utf
    if: (header & 2) != 0
  - id: start_mode
    type: u1
    if: (header & 4) != 0
  - id: fuel_inputs_group_name
    type: common::string_utf
    if: (header & 8) != 0
  - id: fuel_outputs_group_name
    type: common::string_utf
    if: (header & 16) != 0
  - id: furnace_inputs_group_name
    type: common::string_utf
    if: (header & 32) != 0
  - id: furnace_outputs_group_name
    type: common::string_utf
    if: (header & 64) != 0
  - id: current_recipe
    type: current_recipe_block(context, world_version)
    if: (header & 128) != 0
types:
  current_recipe_block:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: recipe_name
        type: common::string_utf
      - id: script_version
        type: u8
      - id: elapsed_time
        type: u4
      - id: craft_recipe
        type: entity_shared::craft_recipe_block(context, world_version)
