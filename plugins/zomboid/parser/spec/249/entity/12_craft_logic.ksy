meta:
  id: craft_logic
  endian: be
  imports:
    - ../../common/common
    - ../inventory
    - entity_shared
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.crafting.CraftLogic.load / save
  - id: header
    type: u1
  # 1: recipe_tag_query
  - id: recipe_tag_query
    type: common::string_utf
    if: (header & 1) != 0
  # 2: start_mode (u1)
  - id: start_mode
    type: u1
    if: (header & 2) != 0
  # 4: inputs_group_name
  - id: inputs_group_name
    type: common::string_utf
    if: (header & 4) != 0
  # 8: outputs_group_name
  - id: outputs_group_name
    type: common::string_utf
    if: (header & 8) != 0
  # 16: in-progress craft data (list of ByteBlock-framed entries)
  - id: in_progress
    type: in_progress_list(context, world_version)
    if: (header & 16) != 0
  # 32: action_anim_override
  - id: action_anim_override
    type: common::string_utf
    if: (header & 32) != 0
types:
  # List of in-progress CraftRecipeData blocks
  in_progress_list:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: count
        type: u4
        if: world_version >= 238
      - id: entries
        type: entity_shared::craft_recipe_block(context, world_version)
        repeat: expr
        repeat-expr: num_entries
    instances:
      num_entries:
        value: '(world_version >= 238) ? count : 1'
