meta:
  id: resources
  endian: be
  imports:
    - ../../common/common
    - ../inventory
    - 2_fluid_container
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.resources.Resources.save / load
  - id: num_groups
    type: u4
  - id: groups
    type: group(context, world_version)
    repeat: expr
    repeat-expr: num_groups
types:
  group:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: name
        type: common::string_utf
      - id: num_resources
        type: u4
      - id: resources
        type: resource_entry(context, world_version)
        repeat: expr
        repeat-expr: num_resources
  
  resource_entry:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      # Outer resource type id (redundant with inner base.type)
      - id: outer_type
        type: u1
      # zombie.entity.components.resources.Resource.save / load
      - id: body
        type:
          switch-on: outer_type
          cases:
            1: resource_item(context, world_version)
            2: resource_fluid(context, world_version)
            3: resource_energy(context, world_version)
            _: resource_item(context, world_version)

  # Base Resource payload (header + core fields + optionals)
  # zombie.entity.components.resources.Resource.load / save
  resource_base:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: header
        type: u1
      - id: id
        type: common::string_utf
      - id: res_type
        type: u1
      - id: res_io
        type: u1
      - id: progress
        type: f8
        if: (header & 2) != 0
      - id: channel
        type: u1
        if: (header & 4) != 0
      - id: flags_bits
        type: u4
        if: (header & 8) != 0

  resource_item:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: base
        type: resource_base(context, world_version)
      - id: capacity
        type: f4
      - id: stack_any_item
        type: u1
        if: world_version >= 238
      - id: items
        type: inventory::compressed_identical_items(context, world_version)

  resource_fluid:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: base
        type: resource_base(context, world_version)
      - id: fluid
        type: fluid_container(context, world_version)

  resource_energy:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: base
        type: resource_base(context, world_version)
      # Energy.saveEnergy
      - id: has_energy
        type: u1
      - id: energy
        type: energy
        if: has_energy == 1
      - id: capacity
        type: f4
      - id: stored_energy
        type: f4

  energy:
    seq:
      - id: is_modded
        type: u1
      - id: type_string
        type: common::string_utf
        if: is_modded == 1
      - id: type_id
        type: u1
        if: is_modded == 0
