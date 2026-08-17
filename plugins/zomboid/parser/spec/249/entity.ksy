meta:
  id: entity
  endian: be
  imports:
    - ../common/common

    # component types
    - entity/1_attribute_container
    - entity/2_fluid_container
    - entity/3_sprite_config
    - entity/6_lua_component
    - entity/7_parts
    - entity/8_signals
    - entity/9_entity_script_info
    - entity/11_ui_config
    - entity/12_craft_logic
    - entity/13_furnace_logic
    - entity/14_test_component
    - entity/15_mashing_logic
    - entity/16_drying_logic
    - entity/17_meta_tag_component
    - entity/18_resources
    - entity/19_craft_bench
    - entity/20_craft_recipe_component
    - entity/21_durability
    - entity/22_drying_craft_logic
    - entity/23_context_menu_config
    - entity/24_sprite_overlay_config
    - entity/25_craft_bench_sounds
    - entity/26_wall_covering_config
types:
  # zombie.entity.GameEntity.saveEntity / loadEntity
  game_entity:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: num_components
        type: u1
      - id: components
        type: entity_component(context, world_version)
        repeat: expr
        repeat-expr: num_components

  entity_component:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: block_len
        type: u4
      - id: component_id
        type: u2
      - id: data
        type: component(context, world_version, component_id)
        size: block_len - 2

  component:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: component_id
        type: u2
    seq:
      - id: component_data
        type:
          switch-on: component_id
          cases:
            0: common::unknown(0)
            1: attribute_container(context, world_version)
            2: fluid_container(context, world_version)
            3: sprite_config(context, world_version)
            6: lua_component(context, world_version)
            7: parts(context, world_version)
            8: signals(context, world_version)
            9: entity_script_info(context, world_version)
            11: ui_config(context, world_version)
            12: craft_logic(context, world_version)
            13: furnace_logic(context, world_version)
            14: test_component(context, world_version)
            15: mashing_logic(context, world_version)
            16: drying_logic(context, world_version)
            17: meta_tag_component(context, world_version)
            18: resources(context, world_version)
            19: craft_bench(context, world_version)
            20: craft_recipe_component(context, world_version)
            21: durability(context, world_version)
            22: drying_craft_logic(context, world_version)
            23: context_menu_config(context, world_version)
            24: sprite_overlay_config(context, world_version)
            25: craft_bench_sounds(context, world_version)
            26: wall_covering_config(context, world_version)
            _: common::unknown(component_id.as<u4>)
        doc: |-
          Dynamic zombie.entity.Component, the type is determined by:
              ComponentType zombie.entity.ComponentType.FromId(short id)
          Each type is registered in zombie.entity.ComponentType enum definition
