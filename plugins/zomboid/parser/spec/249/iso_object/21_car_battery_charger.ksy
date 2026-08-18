meta:
  id: car_battery_charger
  endian: be
  imports:
    - ../../common/common
    - ../inventory
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: has_item
    type: u1
  - id: item
    type: inventory::sized_blob(context, world_version)
    if: has_item == 1
  - id: has_battery
    type: u1
  - id: battery
    type: inventory::sized_blob(context, world_version)
    if: has_battery == 1
  - id: activated
    type: u1
  - id: last_update
    type: f4
  - id: charge_rate
    type: f4
