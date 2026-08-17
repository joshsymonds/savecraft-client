meta:
  id: item_container
  endian: be
  imports:
    - ../../common/common
    - ../inventory

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.InventoryContainer.save / load
# Extends: InventoryItem
seq:
  - id: container_id
    type: s4
  - id: weight_reduction
    type: s4
  - id: container_items
    type: inventory::container(context, world_version)
