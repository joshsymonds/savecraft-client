meta:
  id: world_inventory_object
  endian: be
  imports:
    - ../../common/common
    - ../inventory
    - ../entity
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  # NOTE: IsoWorldInventoryObject does NOT call super.load()
  - id: xoff
    type: f4
  - id: yoff
    type: f4
  - id: zoff
    type: f4
  - id: offset_x
    type: f4
  - id: offset_y
    type: f4
  - id: item
    type: inventory::sized_blob(context, world_version)
  - id: drop_time
    type: f8
  - id: bit_flags
    type: u1
  - id: entity
    type: entity::game_entity(context, world_version)
    if: (bit_flags & 2) != 0
instances:
  ignore_remove_sandbox:
    value: (bit_flags & 1) != 0
  has_extended_placement:
    value: (bit_flags & 4) != 0
