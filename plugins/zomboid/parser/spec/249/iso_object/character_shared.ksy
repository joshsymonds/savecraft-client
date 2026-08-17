meta:
  id: character_shared
  endian: be
  imports:
    - ../../common/common
    - ../inventory
    - ../visual
    - iso_object_shared

types:
  game_character_base:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: debug
        type: u1
      - id: is_zombie
        type: u1
    seq:
      - id: iso_moving_object_base
        type: iso_object_shared::iso_moving_object
      - id: has_descriptor
        type: u1
      - id: descriptor
        type: survivor_desc(context, world_version)
        if: has_descriptor == 1
      - id: visual
        type: visual::human_visual
      - id: inventory
        type: inventory::container(context, world_version)
      - id: asleep
        type: u1
      - id: force_wake_up_time
        type: f4
      - id: stats
        type: character_stats
        if: is_zombie == 0
      - id: body_damage
        type: body_damage(context, world_version)
        if: is_zombie == 0
      - id: xp
        type: character_xp
        if: is_zombie == 0
      - id: left_hand_item_index
        type: s4
        if: is_zombie == 0
      - id: right_hand_item_index
        type: s4
        if: is_zombie == 0
      - id: on_fire
        type: u1
      - id: depress_effect
        type: f4
      - id: depress_first_take_time
        type: f4
      - id: beta_effect
        type: f4
      - id: beta_delta
        type: f4
      - id: pain_effect
        type: f4
      - id: pain_delta
        type: f4
      - id: sleeping_tablet_effect
        type: f4
      - id: sleeping_tablet_delta
        type: f4
      - id: num_read_books
        type: s4
      - id: read_books
        type: read_book
        repeat: expr
        repeat-expr: num_read_books
      - id: reduce_infection_power
        type: f4
      - id: num_known_recipes
        type: s4
      - id: known_recipes
        type: common::string_utf
        repeat: expr
        repeat-expr: num_known_recipes
      - id: last_hour_sleeped
        type: s4
      - id: time_since_last_smoke
        type: f4
      - id: beard_grow_timing
        type: f4
      - id: hair_grow_timing
        type: f4
      - id: unlimited_carry
        type: u1
      - id: build_cheat
        type: u1
      - id: health_cheat
        type: u1
      - id: mechanics_cheat
        type: u1
      - id: movables_cheat
        type: u1
      - id: farming_cheat
        type: u1
      - id: fishing_cheat
        type: u1
        if: world_version >= 202
      - id: can_use_brush_tool
        type: u1
        if: world_version >= 217
      - id: fast_move_cheat
        type: u1
        if: world_version >= 217
      - id: timed_action_instant_cheat
        type: u1
      - id: unlimited_endurance
        type: u1
      - id: unlimited_ammo
        type: u1
        if: world_version >= 230
      - id: know_all_recipes
        type: u1
        if: world_version >= 230
      - id: sneaking
        type: u1
      - id: death_drag_down
        type: u1
      - id: num_read_literatures
        type: s4
      - id: read_literatures
        type: read_literature_entry
        repeat: expr
        repeat-expr: num_read_literatures
      - id: num_read_print_media
        type: s4
        if: world_version >= 222
      - id: read_print_media
        type: common::string_utf
        repeat: expr
        repeat-expr: num_read_print_media
        if: world_version >= 222
      - id: last_animal_pet
        type: s8
      - id: player_cheats
        type: character_cheats
        if: world_version >= 231

  survivor_desc:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      # SurvivorDesc.load(ByteBuffer, int WorldVersion, IsoGameCharacter)
      # and SurvivorDesc.save(ByteBuffer) ordering
      - id: id
        type: s4
      - id: forename
        type: common::string_utf
      - id: surname
        type: common::string_utf
      - id: torso
        type: common::string_utf
      - id: female_i
        type: s4
        doc: "1 for female, 0 for male (stored as 32-bit int)"
      - id: character_profession
        type: common::string_utf
        doc: "Registries.CHARACTER_PROFESSION location string"
      - id: has_extras
        type: s4
        doc: "1 if extras are present, else 0"
      - id: num_extras
        type: s4
        if: has_extras == 1
      - id: extras
        type: common::string_utf
        repeat: expr
        repeat-expr: num_extras
        if: has_extras == 1
      - id: num_xp_boosts
        type: s4
      - id: xp_boosts
        type: xp_boost_entry
        repeat: expr
        repeat-expr: num_xp_boosts
      - id: voice_prefix
        type: common::string_utf
        if: world_version >= 208
      - id: voice_pitch
        type: f4
        if: world_version >= 208
      - id: voice_type
        type: s4
        if: world_version >= 208
    instances:
      is_female:
        value: female_i == 1

  xp_boost_entry:
    seq:
      - id: perk_name
        type: common::string_utf
      - id: level
        type: s4

  read_book:
    seq:
      - id: full_type
        type: common::string_utf
      - id: already_read_pages
        type: s4

  read_literature_entry:
    seq:
      - id: title
        type: common::string_utf
      - id: day
        type: s4

  character_stats:
    seq:
      - id: ordered_stats
        type: f4
        repeat: expr
        repeat-expr: 24
        doc: |
          order: ANGER, BOREDOM, DISCOMFORT, ENDURANCE, FATIGUE, FITNESS, FOOD_SICKNESS, HUNGER, IDLENESS, INTOXICATION, MORALE, NICOTINE_WITHDRAWAL, PAIN, PANIC, POISON, SANITY, SICKNESS, STRESS, TEMPERATURE, THIRST, UNHAPPINESS, WETNESS, ZOMBIE_FEVER, ZOMBIE_INFECTION

  body_damage:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: body_parts
        type: body_part(context, world_version)
        repeat: expr
        repeat-expr: 17
        doc: |
          order: Hand_L, Hand_R, ForeArm_L, ForeArm_R, UpperArm_L, UpperArm_R, Torso_Upper, Torso_Lower, Head, Neck, Groin, UpperLeg_L, UpperLeg_R, LowerLeg_L, LowerLeg_R, Foot_L, Foot_R
      - id: catch_a_cold
        type: f4
      - id: has_a_cold
        type: u1
      - id: cold_strength
        type: f4
      - id: time_to_sneeze_or_cough
        type: s4
        if: world_version >= 222
      - id: reduce_fake_infection
        type: u1
      - id: health_from_food_timer
        type: f4
      - id: pain_reduction
        type: f4
      - id: cold_reduction
        type: f4
      - id: infection_time
        type: f4
      - id: infection_mortality_duration
        type: f4
      - id: cold_damage_stage
        type: f4
      - id: has_thermoregulator
        type: u1
      - id: thermoregulator
        type: thermoregulator(context, world_version)
        if: has_thermoregulator == 1

  body_part:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: is_cut
        type: u1
      - id: is_bitten
        type: u1
      - id: is_scratched
        type: u1
      - id: is_bandaged
        type: u1
      - id: is_bleeding
        type: u1
      - id: is_deep_wounded
        type: u1
      - id: is_fake_infected
        type: u1
      - id: is_infected
        type: u1
      - id: health
        type: f4
      - id: bandage_life
        type: f4
        if: is_bandaged == 1
      - id: is_infected_wound
        type: u1
      - id: wound_infection_level
        type: f4
        if: is_infected_wound == 1
      - id: cut_time_1
        type: f4
      - id: bite_time
        type: f4
      - id: scratch_time
        type: f4
      - id: bleeding_time
        type: f4
      - id: alcohol_level
        type: f4
      - id: additional_pain
        type: f4
      - id: deep_wound_time
        type: f4
      - id: have_glass
        type: u1
      - id: get_bandage_xp
        type: u1
      - id: stitched
        type: u1
      - id: stitch_time
        type: f4
      - id: get_stitch_xp
        type: u1
      - id: get_splint_xp
        type: u1
      - id: fracture_time
        type: f4
      - id: is_splint
        type: u1
      - id: splint_factor
        type: f4
        if: is_splint == 1
      - id: have_bullet
        type: u1
      - id: burn_time
        type: f4
      - id: need_burn_wash
        type: u1
      - id: last_time_burn_wash
        type: f4
      - id: splint_item
        type: common::string_utf
      - id: bandage_type
        type: common::string_utf
      - id: cut_time_2
        type: f4
      - id: wetness
        type: f4
      - id: stiffness
        type: f4
      - id: comfrey_factor
        type: f4
        if: world_version >= 227
      - id: garlic_factor
        type: f4
        if: world_version >= 227
      - id: plantain_factor
        type: f4
        if: world_version >= 227

  thermoregulator:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: set_point
        type: f4
      - id: metabolic_rate
        type: f4
      - id: metabolic_rate_real
        type: f4
        if: world_version >= 243
      - id: metabolic_target
        type: f4
      - id: body_heat_delta
        type: f4
      - id: core_heat_delta
        type: f4
      - id: thermal_damage
        type: f4
      - id: damage_counter
        type: f4
      - id: thermal_duration
        type: f4
        if: world_version >= 249
      - id: num_nodes
        type: s4
      - id: nodes
        type: thermoregulator_node(context, world_version)
        repeat: expr
        repeat-expr: num_nodes

  thermoregulator_node:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: node_index
        type: s4
      - id: celcius
        type: f4
      - id: skin_celcius
        type: f4
      - id: heat_delta
        type: f4
      - id: primary_delta
        type: f4
      - id: secondary_delta
        type: f4
      - id: insulation
        type: f4
        if: world_version >= 241
      - id: windresist
        type: f4
        if: world_version >= 243
      - id: body_wetness
        type: f4
        if: world_version >= 243
      - id: clothing_wetness
        type: f4
        if: world_version >= 243

  character_xp:
    seq:
      - id: traits
        type: character_traits
      - id: total_xp
        type: f4
      - id: level
        type: s4
      - id: lastlevel
        type: s4
      - id: num_xp_entries
        type: s4
      - id: xp_entries
        type: perk_xp_entry
        repeat: expr
        repeat-expr: num_xp_entries
      - id: num_perk_levels
        type: s4
      - id: perk_levels
        type: perk_level_entry
        repeat: expr
        repeat-expr: num_perk_levels
      - id: num_multipliers
        type: s4
      - id: multipliers
        type: perk_multiplier_entry
        repeat: expr
        repeat-expr: num_multipliers

  character_traits:
    seq:
      - id: num_known_traits
        type: s4
      - id: known_traits
        type: common::string_utf
        repeat: expr
        repeat-expr: num_known_traits

  perk_xp_entry:
    seq:
      - id: perk
        type: common::string_utf
      - id: xp
        type: f4

  perk_level_entry:
    seq:
      - id: perk
        type: common::string_utf
      - id: level
        type: s4

  perk_multiplier_entry:
    seq:
      - id: perk
        type: common::string_utf
      - id: multiplier
        type: f4
      - id: min_level
        type: s1
      - id: max_level
        type: s1

  character_cheats:
    seq:
      - id: num_cheat_ids
        type: s4
      - id: cheat_ids
        type: u1
        repeat: expr
        repeat-expr: num_cheat_ids

  worn_item_entry:
    seq:
      - id: body_location
        type: common::string_utf
      - id: item_index
        type: s2

  nutrition_data:
    seq:
      - id: calories
        type: f4
      - id: proteins
        type: f4
      - id: lipids
        type: f4
      - id: carbohydrates
        type: f4
      - id: weight
        type: f4

  mechanics_item_entry:
    seq:
      - id: key
        type: s8
      - id: value
        type: s8

  fitness_data:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
    seq:
      - id: num_stiffness_inc
        type: s4
      - id: stiffness_inc
        type: map_string_f4
        repeat: expr
        repeat-expr: num_stiffness_inc
      - id: num_stiffness_timer
        type: s4
      - id: stiffness_timer
        type: map_string_s4
        repeat: expr
        repeat-expr: num_stiffness_timer
      - id: num_regularity
        type: s4
      - id: regularity
        type: map_string_f4
        repeat: expr
        repeat-expr: num_regularity
      - id: num_bodypart_to_inc_stiffness
        type: s4
      - id: bodypart_to_inc_stiffness
        type: common::string_utf
        repeat: expr
        repeat-expr: num_bodypart_to_inc_stiffness
      - id: num_exe_timer
        type: s4
      - id: exe_timer
        type: map_string_s8
        repeat: expr
        repeat-expr: num_exe_timer

  map_string_f4:
    seq:
      - id: key
        type: common::string_utf
      - id: value
        type: f4

  map_string_s4:
    seq:
      - id: key
        type: common::string_utf
      - id: value
        type: s4

  map_string_s8:
    seq:
      - id: key
        type: common::string_utf
      - id: value
        type: s8

  already_read_book_entry:
    seq:
      - id: registry_item_id
        type: s2

  known_media_lines_data:
    seq:
      - id: num_guid
        type: s2
      - id: guid
        type: common::string_utf
        repeat: expr
        repeat-expr: num_guid

  craft_history_data:
    seq:
      - id: num_entries
        type: s4
      - id: entries
        type: craft_history_entry
        repeat: expr
        repeat-expr: num_entries

  craft_history_entry:
    seq:
      - id: num_key_chars
        type: s4
      - id: key_chars
        type: u2
        repeat: expr
        repeat-expr: num_key_chars
      - id: craft_count
        type: s4
      - id: last_craft_time
        type: f8
