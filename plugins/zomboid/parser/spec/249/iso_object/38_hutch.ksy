meta:
  id: hutch
  endian: be
  imports:
    - ../../common/common
    - ../inventory
    - ../animal
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: linked_x
    type: s4
  - id: linked_y
    type: s4
  - id: linked_z
    type: s4
  - id: data
    type: hutch_data(context, world_version, debug)
    if: is_slave == false
instances:
  is_slave:
    value: linked_x > 0 and linked_y > 0

types:
  hutch_data:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: debug
        type: u1
    seq:
    - id: sprite_name
      type: common::string_utf
      if: world_version >= 204
    - id: open
      type: u1
    - id: open_egg_hatch
      type: u1
      if: world_version >= 204
    - id: saved_x
      type: s4
    - id: saved_y
      type: s4
    - id: saved_z
      type: s4
    - id: animal_buffer_size
      type: s4
      if: world_version >= 212
    # After word_version 212, animals are skipped on server
    # Assuming locale
    - id: num_animals
      type: u1
    - id: animals
      type: hutch_animal(context, world_version)
      repeat: expr
      repeat-expr: num_animals
    - id: hutch_dirt
      type: f4
    - id: nest_box_dirt
      type: f4
    - id: num_nest_boxes
      type: u1
    - id: nest_boxes
      type: nest_box(context, world_version)
      repeat: expr
      repeat-expr: num_nest_boxes

  hutch_animal:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: header
        type: common::serialized_class_header
      - id: animal
        type: animal::animal(context, world_version)

  nest_box:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: num_eggs
        type: u1
      - id: eggs
        type: inventory::sized_blob(context, world_version)
        repeat: expr
        repeat-expr: num_eggs
      - id: has_animal
        type: u1
      - id: animal
        type: animal::animal(context, world_version)
        if: has_animal == 1
