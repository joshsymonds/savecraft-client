meta:
  id: item_food
  endian: be
  imports:
    - ../../common/common
    - ../animal

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.Food.save / load
# Extends: InventoryItem
# NOTE: Nested BitHeaders - one Byte header, one Integer header
seq:
  - id: age
    type: f4
  - id: last_aged
    type: f4
  - id: flags
    type: u1
  - id: calories
    type: f4
    if: (flags & 1) != 0
  - id: proteins
    type: f4
    if: (flags & 1) != 0
  - id: lipids
    type: f4
    if: (flags & 1) != 0
  - id: carbohydrates
    type: f4
    if: (flags & 1) != 0
  - id: hunger_change
    type: f4
    if: (flags & 2) != 0
  - id: base_hunger
    type: f4
    if: (flags & 4) != 0
  - id: unhappy_change
    type: f4
    if: (flags & 8) != 0
  - id: boredom_change
    type: f4
    if: (flags & 16) != 0
  - id: thirst_change
    type: f4
    if: (flags & 32) != 0
  - id: food_extra
    type: food_extra
    if: (flags & 64) != 0

types:
  food_extra:
    seq:
      - id: flags
        type: u4
      - id: heat
        type: f4
        if: (flags & 1) != 0
      - id: last_cook_minute
        type: s4
        if: (flags & 2) != 0
      - id: cooking_time
        type: f4
        if: (flags & 4) != 0
      - id: poison_detection_level
        type: u1
        if: (flags & 128) != 0
      - id: num_spices
        type: u1
        if: (flags & 256) != 0
      - id: spices
        type: common::string_utf
        repeat: expr
        repeat-expr: num_spices
        if: (flags & 256) != 0
      - id: poison_power
        type: u1
        if: (flags & 512) != 0
      - id: chef
        type: common::string_utf
        if: (flags & 1024) != 0
      - id: off_age
        type: s4
        if: (flags & 2048) != 0
      - id: off_age_max
        type: s4
        if: (flags & 4096) != 0
      - id: pain_reduction
        type: f4
        if: (flags & 8192) != 0
      - id: flu_reduction
        type: s4
        if: (flags & 16384) != 0
      - id: food_sickness_change
        type: s4
        if: (flags & 32768) != 0
      - id: use_for_poison
        type: u2
        if: (flags & 131072) != 0
      - id: freezing_time
        type: f4
        if: (flags & 262144) != 0
      - id: last_frozen_update
        type: f4
        if: (flags & 1048576) != 0
      - id: rotten_time
        type: f4
        if: (flags & 2097152) != 0
      - id: compost_time
        type: f4
        if: (flags & 4194304) != 0
      - id: fatigue_change
        type: f4
        if: (flags & 16777216) != 0
      - id: end_change
        type: f4
        if: (flags & 33554432) != 0
      - id: milk_qty
        type: s4
        if: (flags & 67108864) != 0
      - id: milk_type
        type: common::string_utf
        if: (flags & 67108864) != 0
      - id: time_to_hatch
        type: s4
        if: (flags & 134217728) != 0
      - id: fertilized_time
        type: s4
        if: (flags & 134217728) != 0
      - id: animal_hatch
        type: common::string_utf
        if: (flags & 134217728) != 0
      - id: animal_hatch_breed
        type: common::string_utf
        if: (flags & 134217728) != 0
      - id: num_egg_genome
        type: s4
        if: (flags & 134217728) != 0
      - id: egg_genome
        type: animal::animal_gene
        repeat: expr
        repeat-expr: num_egg_genome
        if: (flags & 134217728) != 0
      - id: mother_id
        type: s4
        if: (flags & 134217728) != 0
      - id: stress_change
        type: f4
        if: (flags & 268435456) != 0
    instances:
      cooked:
        value: (flags & 8) != 0
      burnt:
        value: (flags & 16) != 0
      is_cookable:
        value: (flags & 32) != 0
      dangerous_uncooked:
        value: (flags & 64) != 0
      is_poison:
        value: (flags & 65536) != 0
      is_frozen:
        value: (flags & 524288) != 0
      cooked_in_microwave:
        value: (flags & 8388608) != 0
      is_fertilized:
        value: (flags & 134217728) != 0
      
