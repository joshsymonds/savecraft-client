meta:
  id: drying_craft_logic
  endian: be
  imports:
    - ../../common/common
    - entity_shared
    - 12_craft_logic
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # Extends CraftLogic, then appends wetness table
  - id: craft_logic
    type: craft_logic(context, world_version)
  - id: num_wetnesses
    type: u4
  - id: wetnesses
    type: f8
    repeat: expr
    repeat-expr: num_wetnesses
    if: num_wetnesses == craft_logic.in_progress.num_entries
