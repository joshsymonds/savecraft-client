meta:
    id: animal
    endian: be
    imports:
        - ../common/common
types:
  # characters.animal.IsoAnimal.save / load
  animal:
    params:
      - id: context
        type: any
      - id: world_version
        type: s4
      # the debug param is unused in animal load/save methods
    seq:
      - id: uuid_high
        type: u8
      - id: uuid_low
        type: u8
      - id: x
        type: f4
      - id: y
        type: f4
      - id: z
        type: f4
      - id: dir
        type: s4
      - id: stats
        type: stats
      - id: type
        type: common::string_utf
      - id: breed
        type: common::string_utf
      - id: custom_name
        type: common::string_utf
      - id: table
        type: common::ktable
      - id: item_id
        type: s4
      - id: is_female
        type: u1
      - id: animal_id
        type: s4
      - id: num_genome
        type: s4
      - id: genome
        type: animal_gene
        repeat: expr
        repeat-expr: num_genome
      - id: attach_back_to_tree
        type: u1
      - id: tree_x
        type: s4
        if: attach_back_to_tree == 1
      - id: tree_y
        type: s4
        if: attach_back_to_tree == 1
      - id: age
        type: s4
      - id: hours_survived
        type: f8
      - id: time_since_last_update
        type: s8
      - id: data_size
        type: f4
      - id: attach_back_to_mother
        type: s4
      - id: has_mother
        type: u1
      - id: mother_id
        type: s4
        if: has_mother == 1
      - id: is_pregnant
        type: u1
      - id: pregnancy_time
        type: s4
        if: is_pregnant == 1
      - id: can_have_milk
        type: u1
      - id: milk_amount
        type: f4
      - id: max_milk_amount
        type: f4
      - id: milk_removed
        type: s4
      - id: preferred_hutch_position
        type: u1
      - id: wool_amount
        type: f4
        if: type.value == "lamb" or type.value == "ewe" or type.value == "ram"
      - id: fertilized_time
        type: s4
      - id: fertilized
        type: u1
      - id: eggs_today
        type: s4
        if: type.value == "hen" or type.value == "turkeyhen"
      - id: stress_level
        type: f4
      - id: num_player_acceptances
        type: s4
      - id: player_acceptances
        type: player_acceptance
        repeat: expr
        repeat-expr: num_player_acceptances
      - id: weight
        type: f4
      - id: last_pregnancy_time
        type: s8
      - id: last_milk_timer
        type: s8
      - id: last_impregnate_time
        type: s4
      - id: health
        type: f4
      - id: virtual_id
        type: f8
      - id: migration_group
        type: common::string_utf
      - id: clutch_size
        type: s4
      - id: on_hook
        type: u1
      - id: attach_back_to_hook_x
        type: s4
        if: on_hook == 1
      - id: attach_back_to_hook_y
        type: s4
        if: on_hook == 1
      - id: attach_back_to_hook_z
        type: s4
        if: on_hook == 1
      - id: pet_timer
        type: f4
        if: world_version >= 236
      - id: is_wild
        type: u1
        if: world_version >= 245
      - id: online_id
        type: s2
        if: world_version >= 247

  # characters.animal.AnimalGene.save / load
  animal_gene:
    seq:
      - id: id
        type: s4
      - id: name
        type: common::string_utf
      - id: allele1
        type: animal_allele
      - id: allele2
        type: animal_allele

  # characters.animal.AnimalAllele.save / load
  animal_allele:
    seq:
      - id: name
        type: common::string_utf
      - id: current_value
        type: f4
      - id: true_ratio_value
        type: f4
      - id: is_dominant
        type: u1
      - id: genetic_disorder
        type: common::string_utf

  player_acceptance:
    seq:
      - id: key
        type: u2
      - id: value
        type: f4

  stats:
    seq:
      - id: anger
        type: f4
      - id: boredom
        type: f4
      - id: discomfort
        type: f4
      - id: endurance
        type: f4
      - id: fatigue
        type: f4
      - id: fitness
        type: f4
      - id: food_sickness
        type: f4
      - id: hunger
        type: f4
      - id: idleness
        type: f4
      - id: intoxication
        type: f4
      - id: morale
        type: f4
      - id: nicotine_withdrawal
        type: f4
      - id: pain
        type: f4
      - id: panic
        type: f4
      - id: poison
        type: f4
      - id: sanity
        type: f4
      - id: sickness
        type: f4
      - id: stress
        type: f4
      - id: temperature
        type: f4
      - id: thirst
        type: f4
      - id: unhappiness
        type: f4
      - id: wetness
        type: f4
      - id: zombie_fever
        type: f4
      - id: zombie_infection
        type: f4

  # zombie.characters.animals.AnimalTracks.load / save
  animal_tracks:
    seq:
      - id: animal_type
        type: common::string_utf
      - id: track_type
        type: common::string_utf
      - id: x
        type: s4
      - id: y
        type: s4
      - id: has_dir
        type: u1
      - id: dir_index
        type: s4
        if: has_dir == 1
      - id: added_time
        type: u8
      - id: added_to_world
        type: u1
