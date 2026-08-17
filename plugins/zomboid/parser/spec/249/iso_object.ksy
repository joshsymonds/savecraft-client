meta:
  id: iso_object
  endian: be
  imports:
    - ../common/common
    - blood_splat
    - inventory
    - animal
    - base_vehicle
    - entity
    - iso_object/iso_object_shared
    - iso_object/character_shared

    # Inherited types
    # see iso.IsoObject.initFactory()
    # 1: IsoPlayer
    - iso_object/1_player
    # 3: IsoZombie
    - iso_object/3_zombie
    # 4: IsoPushableObject
    - iso_object/4_pushable_object
    # 5: IsoWheelieBin
    - iso_object/5_wheelie_bin
    # 6: IsoWorldInventoryObject (NOTE: does NOT call super.load)
    - iso_object/6_world_inventory_object
    # 7: IsoJukebox
    - iso_object/7_jukebox
    # 8: IsoCurtain
    - iso_object/8_curtain
    # 9: IsoRadio
    - iso_object/9_radio
    # 10: IsoTelevision
    - iso_object/10_television
    # 11: IsoDeadBody
    - iso_object/11_dead_body
    # 12: IsoBarbecue
    - iso_object/12_barbecue
    # 13: IsoClothingDryer
    - iso_object/13_clothing_dryer
    # 14: IsoClothingWasher
    - iso_object/14_clothing_washer
    # 15: IsoFireplace
    - iso_object/15_fireplace
    # 16: IsoStove
    - iso_object/16_stove
    # 17: IsoDoor
    - iso_object/17_door
    # 18: IsoThumpable
    - iso_object/18_thumpable
    # 19: IsoTrap
    - iso_object/19_trap
    # 20: IsoBrokenGlass (no extra fields)
    - iso_object/20_broken_glass
    # 21: IsoCarBatteryCharger
    - iso_object/21_car_battery_charger
    # 22: IsoGenerator
    - iso_object/22_generator
    # 23: IsoCompost
    - iso_object/23_compost
    # 24: IsoMannequin
    - iso_object/24_mannequin
    # 26: IsoWindow
    - iso_object/26_window
    # 27: IsoBarricade (NOTE: does NOT call super.load)
    - iso_object/27_barricade
    # 28: IsoTree
    - iso_object/28_tree
    # 29: IsoLightSwitch
    - iso_object/29_light_switch
    # 30: IsoZombieGiblets (not serialized - Serialize returns false)
    - iso_object/30_zombie_giblets
    # 31: IsoMolotovCocktail (likely not serialized)
    - iso_object/31_molotov_cocktail
    # 32: IsoFire
    - iso_object/32_fire
    # 33: BaseVehicle (use base_vehicle::vehicle)
    # 34: IsoCombinationWasherDryer
    - iso_object/34_combination_washer_dryer
    # 35: IsoStackedWasherDryer
    - iso_object/35_stacked_washer_dryer
    # 36: IsoAnimal (use animal::animal)
    # 37: IsoFeedingTrough
    - iso_object/37_feeding_trough
    # 38: IsoHutch
    - iso_object/38_hutch
    # 39: IsoAnimalTrack
    - iso_object/39_animal_track
    # 40: IsoButcherHook (no extra fields)
    - iso_object/40_butcher_hook
    # 41: IsoWindowFrame
    - iso_object/41_window_frame

# zombie.iso.IsoObject with header
# zombie.iso.IsoObject.factoryFromFileInput(cell, inputBuffer) -> instances of IsoObject subclasses
# load(inputBuffer, WorldVersion, IS_DEBUG_SAVE) method on subclass instances
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: class_header
    type: common::serialized_class_header
  - id: base_object
    if: class_header.serialize == 1
    type:
      switch-on: base_type
      cases:
        '"IsoObject"': base_object(context, world_version, debug)
        '"IsoMovingObject"': iso_object_shared::iso_moving_object
        '"IsoGameCharacter"': character_shared::game_character_base(context, world_version, debug, is_zombie)
        _: common::empty
  - id: subclass_object
    if: class_header.serialize == 1 and class_header.class_id != 0
    type:
      switch-on: class_id
      cases:
        # See IsoObject.factoryFromFileInput for class ID mappings
        0: common::empty
        1: player(context, world_version, debug)
        # NOTE: ID 2 (IsoSurvivor) doesn't have serialization implemented
        3: zombie_character(context, world_version, debug)
        4: pushable_object(context, world_version, debug)
        5: wheelie_bin(context, world_version, debug)
        6: world_inventory_object(context, world_version, debug)
        7: jukebox(context, world_version, debug)
        8: curtain(context, world_version, debug)
        9: radio(context, world_version, debug)
        10: television(context, world_version, debug)
        11: dead_body(context, world_version, debug)
        12: barbecue(context, world_version, debug)
        13: clothing_dryer(context, world_version, debug)
        14: clothing_washer(context, world_version, debug)
        15: fireplace(context, world_version, debug)
        16: stove(context, world_version, debug)
        17: door(context, world_version, debug)
        18: thumpable(context, world_version, debug)
        19: trap(context, world_version, debug)
        20: broken_glass(context, world_version, debug)
        21: car_battery_charger(context, world_version, debug)
        22: generator(context, world_version, debug)
        23: compost(context, world_version, debug)
        24: mannequin(context, world_version, debug)
        # NOTE: ID 25 is not used
        26: window(context, world_version, debug)
        27: barricade(context, world_version, debug)
        28: tree(context, world_version, debug)
        29: light_switch(context, world_version, debug)
        30: zombie_giblets(context, world_version, debug)
        31: molotov_cocktail(context, world_version, debug)
        32: fire(context, world_version, debug)
        33: base_vehicle::vehicle(context, world_version)
        34: combination_washer_dryer(context, world_version, debug)
        35: stacked_washer_dryer(context, world_version, debug)
        36: animal::animal(context, world_version)
        37: feeding_trough(context, world_version, debug)
        38: hutch(context, world_version, debug)
        39: animal_track(context, world_version, debug)
        40: butcher_hook(context, world_version, debug)
        41: window_frame(context, world_version, debug)
        _: common::unknown(class_header.class_id.as<u4>)
instances:
  class_id:
    value: class_header.class_id
  # Some classes inherit from IsoGameCharacter and it overrides IsoObject.load():
  # - class_id 1 (IsoPlayer)
  # - class_id 2 (IsoSurvivor)
  # - class_id 3 (IsoZombie)
  is_game_character:
    value: class_id == 1 or class_id == 2 or class_id == 3
  # Some classes inherit from MovingObject and it overrides IsoObject.load():
  # - class_id 11 (IsoDeadBody)
  # - class_id 30 (IsoZombieGiblets)
  # - class_id 31 (IsoMolotovCocktail)
  # - class_id 33 (BaseVehicle)
  is_moving_object_base:
    value: class_id == 30 or class_id == 31 or class_id == 33 or class_id == 11
  # Some classes don't call super.load() and need special handling:
  # - class_id 6 (IsoWorldInventoryObject): has own format, no base_object
  # - class_id 27 (IsoBarricade): has own format, no base_object
  # - class_id 36 (IsoAnimal): has own format, no base_object
  is_base_object_overridden:
    value: class_id < 0 or class_id == 6 or class_id == 27 or class_id == 36
  base_type:
    value: 'is_game_character ? "IsoGameCharacter" : (is_moving_object_base ? "IsoMovingObject" : (is_base_object_overridden ? "None" : "IsoObject"))'
  is_zombie:
    value: '(class_id == 3) ? 1 : 0'

types:
  # iso.IsoObject.load / save (no header)
  base_object:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: debug
        type: u1
    seq:
      - id: sprite_id
        type: s4
      - id: flags
        type: u1
      - id: raw_sprite_count
        type: u1
        if: (flags & 1) != 0 and (flags & 2) == 0
      - id: debug_sprite_count
        type: common::string_utf
        if: (flags & 1) != 0 and debug != 0
      - id: sprites
        type: sprite
        repeat: expr
        repeat-expr: num_sprites
      - id: debug_info_writing_name
        type: common::string_utf
        if: (flags & 4) != 0 and debug != 0
      - id: sprite_name
        type: sprite_name
        if: (flags & 4) != 0
      - id: custom_color
        type: common::color_rgb
        if: (flags & 8) != 0
      - id: extra_data
        type: extra_data(context, world_version, debug)
        if: (flags & 64) != 0
    instances:
      num_sprites:
        value: '(flags & 3) == 3 ? 1 : (flags & 1) != 0 ? raw_sprite_count : 0'

  sprite:
    seq:
      - id: sprite_id
        type: s4
      - id: flags
        type: u1
      - id: offset_x
        type: f4
        if: (flags & 2) != 0
      - id: offset_y
        type: f4
        if: (flags & 2) != 0
      - id: offset_z
        type: f4
        if: (flags & 2) != 0
      - id: tint_r
        type: s1
        if: (flags & 2) != 0
      - id: tint_g
        type: s1
        if: (flags & 2) != 0
      - id: tint_b
        type: s1
        if: (flags & 2) != 0
      - id: alpha
        type: f4
        if: (flags & 16) != 0

  sprite_name:
    seq:
      - id: flag
        type: u1
      - id: raw_name
        type: common::id_or_name_u1((flag & 4) != 0)
        if: (flag & 4) != 0 or (flag & 8) != 0
      - id: sprite_name
        type: common::id_or_name_s4be((flag & 16) != 0)
        if: (flag & 16) != 0 or (flag & 32) != 0
    instances:
      name:
        value: '((flag & 2) != 0) ? "Grass" : (((flag & 4) != 0 or (flag & 8) != 0) ? raw_name.value : (((flag & 16) != 0 or (flag & 32) != 0) ? sprite_name.value : ""))'

  extra_data:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: debug
        type: u1
    seq:
      - id: bits_flags
        type: u2
      - id: num_wall_blood_splats
        type: u1
        if: (bits_flags & 1) != 0
      - id: wall_blood_splats
        type: blood_splat::wall
        repeat: expr
        repeat-expr: num_wall_blood_splats
        if: (bits_flags & 1) != 0
      - id: debug_writing_container
        type: common::string_utf
        if: (bits_flags & 2) != 0 and debug != 0
      - id: num_containers
        type: u1
        if: (bits_flags & 2) != 0
      - id: containers
        type: inventory::container(context, world_version)
        repeat: expr
        repeat-expr: num_containers
        if: (bits_flags & 2) != 0
      - id: table
        type: common::ktable
        if: (bits_flags & 4) != 0
      - id: key_id
        type: s4
        if: (bits_flags & 16) != 0
      - id: sheet_rope_health
        type: f4
        if: (bits_flags & 64) != 0
      - id: render_y_offset
        type: f4
        if: (bits_flags & 128) != 0
      - id: overlay_sprite_name
        type: common::id_or_name_s4be((bits_flags & 512) == 0)
        if: (bits_flags & 256) != 0
      - id: overlay_sprite_color
        type: common::color_rgba
        if: (bits_flags & 1024) != 0
      - id: entity
        type: entity::game_entity(context, world_version)
        if: (bits_flags & 4096) != 0
      - id: sprite_model_name
        type: common::string_utf
        if: (bits_flags & 8192) != 0
