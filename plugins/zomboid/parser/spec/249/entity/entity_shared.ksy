meta:
  id: entity_shared
  endian: be
  imports:
    - ../../common/common
    - ../inventory
types:
  # ByteBlock-framed CraftRecipeData payload
  # zombie.entity.components.crafting.recipe.CraftRecipeData.load / save
  craft_recipe_block:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: len_block
        type: u4
      - id: block
        type: craft_recipe_data(context, world_version)
        size: len_block

  craft_recipe_data:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: has_recipe_flag
        type: u1
        if: world_version >= 238
      - id: recipe_name
        type: common::string_utf
        if: has_recipe
      - id: script_version
        type: u8
        if: has_recipe
      - id: elapsed_time
        type: f8
        if: world_version >= 238
      - id: num_inputs
        type: u4
      - id: inputs
        type: craft_recipe_input_data(context, world_version)
        repeat: expr
        repeat-expr: num_inputs
      - id: has_mod_data
        type: u1
      - id: mod_data
        type: common::ktable
        if: has_mod_data != 0
      - id: has_consumed_inputs
        type: u1
      - id: target_variable_input_ratio
        type: f4
        if: world_version >= 235
    instances:
      has_recipe:
        value: '(world_version >= 238) ? (has_recipe_flag != 0) : true'

  # zombie.entity.components.crafting.recipe.CraftRecipeData.CacheData.loadInputs / saveInputs
  craft_recipe_input_data:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: move_to_outputs
        type: u1
      - id: uses_consumed
        type: f4
      - id: fluid_consumed
        type: f4
      - id: energy_consumed
        type: f4
      - id: fluid_sample
        type: fluid_sample(context, world_version)
      - id: fluid_consume
        type: fluid_consume(context, world_version)
      - id: applied_items
        type: inventory::compressed_identical_items(context, world_version)
      - id: most_recent_item
        type: inventory::compressed_identical_items(context, world_version)
        repeat: expr
        repeat-expr: 1
      - id: cached_can_consume
        type: u1

  # zombie.entity.components.fluids.FluidSample.Load / Save
  fluid_sample:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: sealed
        type: u1
      - id: amount
        type: f4
      - id: num_fluids
        type: u4
      - id: fluids
        type: fluid_instance(context, world_version)
        repeat: expr
        repeat-expr: num_fluids

  # zombie.entity.components.fluids.FluidInstance.load / save
  fluid_instance:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: header
        type: u1
      - id: fluid_id
        type: u1
        if: (header & 1) != 0
      - id: fluid_type
        type: common::string_utf
        if: (header & 1) == 0 and (header & 2) != 0
      - id: color
        type: common::color_rgb
        if: (header & 4) != 0
      - id: amount
        type: f4

  # zombie.entity.components.fluids.FluidConsume.Load / Save
  fluid_consume:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: amount
        type: f4
      - id: poison_effect_level
        type: u4
      # zombie.entity.components.fluids.SealedFluidProperties.load
      - id: header
        type: u4
      - id: fatigue_change
        type: f4
        if: (header & 1) != 0
      - id: hunger_change
        type: f4
        if: (header & 2) != 0
      - id: stress_change
        type: f4
        if: (header & 4) != 0
      - id: thirst_change
        type: f4
        if: (header & 8) != 0
      - id: unhappy_change
        type: f4
        if: (header & 16) != 0
      - id: calories
        type: f4
        if: (header & 32) != 0
      - id: carbohydrates
        type: f4
        if: (header & 64) != 0
      - id: lipids
        type: f4
        if: (header & 128) != 0
      - id: proteins
        type: f4
        if: (header & 256) != 0
      - id: alcohol
        type: f4
        if: (header & 512) != 0
      - id: flu_reduction
        type: f4
        if: (header & 1024) != 0
      - id: pain_reduction
        type: f4
        if: (header & 2048) != 0
      - id: endurance_change
        type: f4
        if: (header & 4096) != 0
      - id: food_sickness_change
        type: s4
        if: (header & 8192) != 0
      - id: poison
        type: f4
        if: (header & 16384) != 0
