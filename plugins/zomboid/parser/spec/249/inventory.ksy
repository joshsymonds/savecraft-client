meta:
  id: inventory
  endian: be
  imports:
    - ../common/common
    - entity
    - animal
    - visual
    - item/alarm_clock
    - item/alarm_clock_clothing
    - item/animal
    - item/clothing
    - item/container
    - item/food
    - item/hand_weapon
    - item/key
    - item/literature
    - item/map
    - item/moveable
    - item/radio
types:
  item:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: registry_id
        type: u2
      - id: item_type
        type: common::strz_utf
        size: 0
        process: context.lookup_strz(context, "item_id_to_type", registry_id)
      - id: item_name
        type: common::strz_utf
        size: 0
        process: context.lookup_strz(context, "item_id_to_name", registry_id)
      - id: save_type
        type: u1
      - id: base
        type: item_base(context, world_version)
      - id: subclass
        type:
          switch-on: item_type.value
          cases:
            '"base:alarmclock"': item_alarm_clock(context, world_version)
            '"base:alarmclockclothing"': item_alarm_clock_clothing(context, world_version)
            '"base:animal"': item_animal(context, world_version)
            '"base:clothing"': item_clothing(context, world_version)
            '"base:container"': item_container(context, world_version)
            '"base:normal"': common::empty
            '"base:drainable"': common::empty
            '"base:food"': item_food(context, world_version)
            '"base:weapon"': item_hand_weapon(context, world_version)
            '"base:key"': item_key(context, world_version)
            '"base:keyring"': common::empty
            '"base:literature"': item_literature(context, world_version)
            '"base:map"': item_map(context, world_version)
            '"base:moveable"': item_moveable(context, world_version)
            '"base:radio"': item_radio(context, world_version)
            '"base:weaponpart"': common::empty
            _: common::empty

  # for debugging purposes, to dump the remaining bytes of an item
  sized_item:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: item
        type: item(context, world_version)
      - id: remaining
        type: common::bytes_eos
      - id: nonzero
        size: 0
        if: remaining.size > 0

  # inventory.InventoryItem.saveWithSize / loadItem (static method)
  sized_blob:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: len_data
        type: u4
      - id: data
        type: item(context, world_version)
        size: len_data

  group:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: identical
        type: s4
      - id: item
        type: sized_blob(context, world_version)
      - id: duplicate_ids
        type: s4
        repeat: expr
        repeat-expr: 'identical > 1 ? (identical - 1) : 0'

  # inventory.CompressIdenticalItems.save / load
  compressed_identical_items:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: num_item_groups
        type: u2
      - id: item_groups
        type: group(context, world_version)
        repeat: expr
        repeat-expr: num_item_groups


  # inventory.ItemContainer.save / load
  container:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: type_name
        type: common::string_utf
      - id: explored
        type: u1
      - id: items
        type: compressed_identical_items(context, world_version)
      - id: has_been_looted
        type: u1
      - id: capacity
        type: s4

  # inventory.InventoryItem.load / save (instance method)
  item_base:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: id
        type: s4
      - id: flags
        type: u1
      - id: current_uses
        type:
          switch-on: world_version >= 220
          cases:
            true: s4
            false: s2
        if: (flags & 0x01) != 0
      - id: deprecated
        type: u1
        if: world_version < 220 and (flags & 0x02) != 0
      - id: condition
        type: u1
        if: (flags & 0x04) != 0
      - id: visual
        type: visual::item_visual
        if: (flags & 0x08) != 0
      - id: custom_color
        type: common::color_rgba
        if: (flags & 0x10) != 0
      - id: item_capacity
        type: f4
        if: (flags & 0x20) != 0
      - id: extra
        type: item_extra(context, world_version)
        if: (flags & 0x40) != 0

  item_extra:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: flags
        type: u4
      - id: mod_data
        type: common::ktable
        if: (flags & 1) != 0
      - id: have_been_repaired
        type: u2
        if: (flags & 4) != 0
      - id: name
        type: common::string_utf
        if: (flags & 8) != 0
      - id: len_byte_data
        type: u4
        if: (flags & 0x00000010) != 0
      - id: byte_data
        type: common::blob
        size: len_byte_data
        if: (flags & 0x00000010) != 0
      - id: num_extra_items_registry_id
        type: u4
        if: (flags & 0x00000020) != 0
      - id: extra_items_registry_id
        type: u2
        repeat: expr
        repeat-expr: num_extra_items_registry_id
        if: (flags & 0x00000020) != 0
      - id: actual_weight
        type: f4
        if: (flags & 0x00000080) != 0
      - id: key_id
        type: u4
        if: (flags & 0x00000100) != 0
      - id: remote_control_id
        type: u4
        if: (flags & 0x00000400) != 0
      - id: remote_range
        type: u4
        if: (flags & 0x00000400) != 0
      - id: color_override_rgb
        type: common::color_rgb
        if: (flags & 0x00000800) != 0
      - id: worker
        type: common::string_utf
        if: (flags & 0x00001000) != 0
      - id: wet_cooldown
        type: f4
        if: (flags & 0x00002000) != 0
      - id: stash_map
        type: common::string_utf
        if: (flags & 0x00008000) != 0
      - id: current_ammo_count
        type: u4
        if: (flags & 0x00020000) != 0
      - id: attached_slot
        type: u4
        if: (flags & 0x00040000) != 0
      - id: attached_slot_type
        type: common::string_utf
        if: (flags & 0x00080000) != 0
      - id: attached_to_model
        type: common::string_utf
        if: (flags & 0x00100000) != 0
      - id: max_capacity
        type: u4
        if: (flags & 0x00200000) != 0
      - id: recorded_media_index
        type: u2
        if: (flags & 0x00400000) != 0
      - id: world_z_rotation_legacy
        type: s4
        if: (world_version < 232) and ((flags & 0x00800000) != 0)
      - id: world_scale
        type: f4
        if: (flags & 0x01000000) != 0
      - id: entity_components
        type: entity::game_entity(context, world_version)
        if: (flags & 0x04000000) != 0
      - id: animal_tracks
        type: animal::animal_tracks
        if: (flags & 0x08000000) != 0
      - id: texture_name
        type: common::string_utf
        if: (flags & 0x10000000) != 0
      - id: model_index
        type: u4
        if: (flags & 0x20000000) != 0
      - id: world_rotation
        type:
          switch-on: (world_version < 232) and ((flags & 0x00800000) != 0)
          cases:
            true: world_rotation(context, world_version, world_z_rotation_legacy)
            false: world_rotation(context, world_version, 0)
        if: (flags & 0x40000000) != 0
    instances:
      activated:
        value: (flags & 0x00000002) != 0
      custom_name:
        value: (flags & 0x00000040) != 0
      favorite:
        value: (flags & 0x00004000) != 0
      infected:
        value: (flags & 0x00010000) != 0
      initialised:
        value: (flags & 0x02000000) != 0

  world_rotation:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: world_z_rotation_legacy
        type: s4
    seq:
      - id: world_x_rotation
        type: f4
        if: world_version >= 232
      - id: world_y_rotation
        type: f4
        if: world_version >= 232
      - id: world_z_rotation
        type: f4
        if: world_version >= 232
      - id: world_y_rotation_legacy
        type: s4
        if: world_version < 232
      - id: world_x_rotation_legacy
        type: s4
        if: world_version < 232
    instances:
      x:
        value: '(world_version >= 232) ? world_x_rotation : world_x_rotation_legacy'
      y:
        value: '(world_version >= 232) ? world_y_rotation : world_y_rotation_legacy'
      z:
        value: '(world_version >= 232) ? world_z_rotation : world_z_rotation_legacy'
