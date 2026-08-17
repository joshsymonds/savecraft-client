meta:
  id: item_animal
  endian: be
  imports:
    - ../../common/common
    - ../animal

# zombie.inventory.types.AnimalInventoryItem.save / load
# Extends: InventoryItem
# Note: delegates to IsoAnimal serialization
params:
  - id: context
    type: any
  - id: world_version
    type: u4

seq:
  - id: animal_data
    type: animal::animal(context, world_version)
