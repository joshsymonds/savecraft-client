meta:
  id: dead_body
  endian: be
  imports:
    - ../../common/common
    - ../inventory
    - ../visual
    - ../animal
    - character_shared

params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: female
    type: u1
  - id: was_zombie
    type: u1
  - id: is_animal
    type: u1
  - id: animal_data
    type: animal_corpse_data
    if: is_animal == 1
  - id: network_id
    type: common::network_id(2)
    if: world_version >= 199
  - id: legacy_id
    type: s2
    if: world_version < 199
  - id: b_server
    type: u1
  - id: persistent_outfit_id
    type: s4
  - id: has_desc
    type: u1
  - id: desc
    type: character_shared::survivor_desc(context, world_version)
    if: has_desc == 1
  - id: visual_type
    type: u1
    doc: "0 = HumanVisual, 1 = AnimalVisual"
  - id: visual
    if: visual_type == 0 or visual_type == 1
    type:
      switch-on: visual_type
      cases:
        0: visual::human_visual
        1: visual::animal_visual
  - id: has_container
    type: u1
  - id: container_data
    type: container_with_worn_items(context, world_version)
    if: has_container == 1
  - id: death_time
    type: f4
  - id: reanimate_time
    type: f4
  - id: flags
    type: u1
  - id: was_skeleton
    type: u1
  - id: angle
    type: f4
  - id: zombie_rot_stage_at_death
    type: u1
  - id: animal_rot_stage_at_death
    type: u1
    if: world_version >= 222
  - id: rotten_texture
    type: common::string_utf
    if: world_version >= 225
  - id: skel_inv_icon
    type: common::string_utf
    if: world_version >= 225
  - id: crawling
    type: u1
  - id: fake_dead
    type: u1
  - id: ragdoll_fall
    type: u1
  - id: bone_transforms
    type: bone_transform_array
    if: ragdoll_fall == 1
types:
  animal_corpse_data:
    seq:
      - id: animal_type
        type: common::string_utf
      - id: animal_size
        type: f4
      - id: num_genes
        type: u1
        if: _root.world_version >= 246
      - id: genes
        type: animal::animal_gene
        repeat: expr
        repeat-expr: num_genes
        if: _root.world_version >= 246
      - id: num_genetic_disorders
        type: u1
        if: _root.world_version >= 246
      - id: genetic_disorders
        type: common::string_utf
        repeat: expr
        repeat-expr: num_genetic_disorders
        if: _root.world_version >= 246
      - id: custom_name
        type: common::string_utf
      - id: corpse_item
        type: common::string_utf
      - id: weight
        type: f4
      - id: inv_icon
        type: common::string_utf
      - id: shadow_bm
        type: f4
      - id: shadow_fm
        type: f4
      - id: shadow_w
        type: f4

  container_with_worn_items:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: container_id
        type: s4
      - id: container
        type: inventory::container(context, world_version)
      - id: num_worn_items
        type: u1
      - id: worn_items
        type: character_shared::worn_item_entry
        repeat: expr
        repeat-expr: num_worn_items
      - id: num_attached_items
        type: u1
      - id: attached_items
        type: attached_item_entry
        repeat: expr
        repeat-expr: num_attached_items
  attached_item_entry:
    seq:
      - id: location
        type: common::string_utf
      - id: item_index
        type: s2
  bone_transform_array:
    seq:
      - id: num_bones
        type: s4
      - id: bones
        type: bone_transform
        repeat: expr
        repeat-expr: num_bones
  bone_transform:
    seq:
      - id: bone_id
        type: s4
      - id: position_x
        type: f4
      - id: position_y
        type: f4
      - id: position_z
        type: f4
      - id: quaternion_x
        type: f4
      - id: quaternion_y
        type: f4
      - id: quaternion_z
        type: f4
      - id: quaternion_w
        type: f4
      - id: scale_x
        type: f4
      - id: scale_y
        type: f4
      - id: scale_z
        type: f4
instances:
  fall_on_front:
    value: (flags & 1) != 0
  killed_by_fall:
    value: (flags & 2) != 0
