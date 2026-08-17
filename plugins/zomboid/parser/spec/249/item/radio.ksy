meta:
  id: item_radio
  endian: be
  imports:
    - ../../common/common
    - moveable
    - ../iso_object/iso_object_shared

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.Radio.save / load
# Extends: Moveable (which extends InventoryItem)
seq:
  - id: moveable_data
    type: item_moveable(context, world_version)
  - id: has_device_data
    type: u1
  - id: device_data
    type: iso_object_shared::device_data
    if: has_device_data != 0
