meta:
  id: craft_bench
  endian: be
  imports:
    - ../../common/common
    - ../inventory
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.crafting.CraftBench.save / load
  - id: recipe_tag_query
    type: common::string_utf
  # EnumBitStore<ResourceChannel>.save -> u4 bits
  - id: fluid_input_channels_bits
    type: u4
  - id: energy_input_channels_bits
    type: u4
