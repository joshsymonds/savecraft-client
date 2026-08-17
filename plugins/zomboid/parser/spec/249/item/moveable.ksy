meta:
  id: item_moveable
  endian: be
  imports:
    - ../../common/common

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.Moveable.save / load
# Extends: InventoryItem
seq:
  - id: world_sprite
    type: common::string_utf
  - id: is_light
    type: u1
  - id: light_use_battery
    type: u1
    if: is_light != 0
  - id: light_has_battery
    type: u1
    if: is_light != 0
  - id: has_light_bulb_item
    type: u1
    if: is_light != 0
  - id: light_bulb_item
    type: common::string_utf
    if: (is_light != 0) and (has_light_bulb_item != 0)
  - id: light_power
    type: f4
    if: is_light != 0
  - id: light_delta
    type: f4
    if: is_light != 0
  - id: light_r
    type: f4
    if: is_light != 0
  - id: light_g
    type: f4
    if: is_light != 0
  - id: light_b
    type: f4
    if: is_light != 0
