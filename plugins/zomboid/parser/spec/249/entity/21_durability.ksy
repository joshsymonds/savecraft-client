meta:
  id: durability
  endian: be
  imports:
    - ../../common/common
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.combat.Durability.save/load (BitHeader Short)
  - id: flags
    type: u2
  - id: current_hit_points
    type: f4
    if: (flags & 1) != 0
  - id: max_hit_points
    type: f4
    if: (flags & 2) != 0
  - id: material
    type: common::string_utf
    if: (flags & 4) != 0
