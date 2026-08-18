meta:
  id: base_vehicle
  title: Project Zomboid BaseVehicle
  endian: be
  imports:
    - ../common/common
    - inventory
    - entity
    - animal
    - iso_object/iso_object_shared

doc: |
  Binary layout for a BaseVehicle save record as written with ByteBuffer in-game.
  Structure derived from vehicles.VehiclesDB2.SQLStore.loadChunk, BaseVehicle.save/load and related classes.

params:
  - id: context
    type: any
  - id: world_version
    type: s4

seq:
  - id: class_header
    type: common::serialized_class_header
    valid:
      # Must be a serialized Vehicle (class ID 33)
      expr: _.serialize == 1 and _.class_id == 33
  - id: iso_moving_object_base
    type: iso_object_shared::iso_moving_object
    if: class_header.serialize == 1 and class_header.class_id == 33
  - id: vehicle
    type: vehicle(context, world_version)
    if: class_header.serialize == 1 and class_header.class_id == 33

types:
  # vehicles.BaseVehicle.load / save (no header and base moving object)
  vehicle:
    params:
      - id: context
        type: any
      - id: world_version
        type: s4
    seq:
      # Physics Z and rotation (quaternion)
      - id: physics_z
        type: f4
      - id: rot_x
        type: f4
      - id: rot_y
        type: f4
      - id: rot_z
        type: f4
      - id: rot_w
        type: f4

      # Script and visuals
      - id: script_name
        type: common::string_utf
      - id: skin_index
        type: s4

      # Engine and durability flags/values
      - id: engine_running
        type: u1
      - id: front_end_durability
        type: s4
      - id: rear_end_durability
        type: s4
      - id: current_front_end_durability
        type: s4
      - id: current_rear_end_durability
        type: s4
      - id: engine_loudness
        type: s4
      - id: engine_quality
        type: s4
      - id: key_id
        type: s4
      - id: key_spawned
        type: u1
      - id: headlights_on
        type: u1
      - id: created
        type: u1
      - id: sound_horn_on
        type: u1
      - id: sound_back_move_on
        type: u1
      - id: lightbar_lights_mode
        type: u1
      - id: lightbar_siren_mode
        type: u1

      # Parts
      - id: num_parts
        type: u2
      - id: parts
        type: vehicle_part(context, world_version)
        repeat: expr
        repeat-expr: num_parts

      # Misc flags and appearance
      - id: key_is_on_door
        type: u1
      - id: hotwired
        type: u1
      - id: hotwired_broken
        type: u1
      - id: keys_in_ignition
        type: u1
      - id: rust
        type: f4
      - id: color_hue
        type: f4
      - id: color_saturation
        type: f4
      - id: color_value
        type: f4
      - id: engine_power
        type: s4
      - id: vehicle_id
        type: u2
      - id: null_string
        type: common::string_utf
        if: world_version < 229
      - id: mechanical_id
        type: s4

      # Alarm
      - id: alarmed
        type: u1
      - id: alarm_start_time
        type: f8
        if: world_version >= 229
      - id: chosen_alarm_sound
        type: common::string_utf
        if: world_version >= 229
      - id: siren_start_time
        type: f8

      # Optional ignition key item (length-prefixed blob)
      - id: has_current_key
        type: u1
      - id: current_key
        type: inventory::sized_blob(context, world_version)
        if: has_current_key == 1

      # Blood intensity map (id -> byte)
      - id: num_blood
        type: u1
      - id: blood
        type: blood_entry
        repeat: expr
        repeat-expr: num_blood

      # Towing information
      - id: has_tow
        type: u1
      - id: vehicle_towing_id
        type: s4
        if: has_tow == 1
      - id: tow_attachment_self
        type: common::string_utf
        if: has_tow == 1
      - id: tow_attachment_other
        type: common::string_utf
        if: has_tow == 1
      - id: row_constraint_z_offset
        type: f4
        if: has_tow == 1

      # Cruise/Regulator
      - id: regulator_speed
        type: f4

      # State flags
      - id: previously_entered
        type: u1
      - id: previously_moved
        type: u1
        if: world_version >= 196

      # Animals chunk (length-prefixed opaque block)
      - id: buffer_size
        type: u4
        if: world_version >= 212
      - id: animals_data_new
        size: buffer_size + 0
        type: animals_data(context, world_version, buffer_size)
        if: world_version >= 212
      - id: animals_data
        type: animals_data(context, world_version, 0)
        if: world_version < 212

      - id: remaining
        type: common::remaining_bytes(0)

  # animals_warpper
  animals_data:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: buffer_size
        type: u4
    seq:
      - id: has_animals_data
        type: u1
      - id: raw_num_animals
        type: u4
        if: has_animals_data == 1
      - id: animals
        type: animal::animal(context, world_version)
        repeat: expr
        repeat-expr: num_animals
    instances:
      num_animals:
        value: '(has_animals_data == 1) ? raw_num_animals : 0'

  # vehicle.VehiclePart.save / load
  vehicle_part:
    params:
      - id: context
        type: any
      - id: world_version
        type: s4
    seq:
      - id: part_id
        type: common::string_utf
      - id: created
        type: u1
      - id: last_updated_hours
        type: f4
      # Inventory item (length-prefixed)
      - id: has_item
        type: u1
      - id: item
        type: inventory::sized_blob(context, world_version)
        if: has_item == 1
      # Item container
      - id: has_container
        type: u1
      - id: container
        type: inventory::container(context, world_version)
        if: has_container == 1
      # Mod data (Lua table)
      - id: has_mod_data
        type: u1
      - id: mod_data
        type: common::ktable
        if: has_mod_data == 1
      # Radio/Device data
      - id: has_device_data
        type: u1
      - id: device_data
        type: iso_object_shared::device_data
        if: has_device_data == 1
      # Light/Door/Window
      - id: has_light
        type: u1
      - id: light
        type: vehicle_light
        if: has_light == 1
      - id: has_door
        type: u1
      - id: door
        type: vehicle_door
        if: has_door == 1
      - id: has_window
        type: u1
      - id: window
        type: vehicle_window
        if: has_window == 1
      # Condition and tuning
      - id: condition
        type: s4
      - id: wheel_friction
        type: f4
      - id: mechanic_skill_installer
        type: s4
      - id: suspension_compression
        type: f4
      - id: suspension_damping
        type: f4
      # Optional entity components
      - id: has_entity
        type: u1
        if: world_version >= 200
      - id: entity
        type: entity::game_entity(context, world_version)
        if: world_version >= 200 and has_entity == 1

  # Lua table entries map for blood intensity
  blood_entry:
    seq:
      - id: id
        type: common::string_utf
      - id: value
        type: u1

  vehicle_light:
    seq:
      - id: active
        type: u1
      - id: offset_x
        type: f4
      - id: offset_y
        type: f4
      - id: intensity
        type: f4
      - id: dist
        type: f4
      - id: focusing
        type: s4

  vehicle_door:
    seq:
      - id: open
        type: u1
      - id: locked
        type: u1
      - id: lock_broken
        type: u1

  vehicle_window:
    seq:
      - id: condition
        type: u1
      - id: open
        type: u1
