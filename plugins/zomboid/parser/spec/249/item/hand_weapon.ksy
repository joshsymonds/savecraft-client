meta:
  id: item_hand_weapon
  endian: be
  imports:
    - ../../common/common
    - ../inventory

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.HandWeapon.save / load
# Extends: InventoryItem
# NOTE: Complex Integer BitHeader with many conditional fields
seq:
  - id: flags
    type: u4
  - id: max_range
    type: f4
    if: (flags & 1) != 0
  - id: min_range_ranged
    type: f4
    if: (flags & 2) != 0
  - id: clip_size
    type: s4
    if: (flags & 4) != 0
  - id: min_damage
    type: f4
    if: (flags & 8) != 0
  - id: max_damage
    type: f4
    if: (flags & 16) != 0
  - id: recoil_delay
    type: s4
    if: (flags & 32) != 0
  - id: aiming_time
    type: s4
    if: (flags & 64) != 0
  - id: reload_time
    type: s4
    if: (flags & 128) != 0
  - id: hit_chance
    type: s4
    if: (flags & 256) != 0
  - id: min_angle
    type: f4
    if: (flags & 512) != 0
  - id: num_attachments
    type: u1
    if: (flags & 1024) != 0
  - id: attachments
    type: inventory::item(context, world_version)
    repeat: expr
    repeat-expr: num_attachments
    if: (flags & 1024) != 0
  - id: fire_mode
    type: common::string_utf
    if: (flags & 2048) != 0
  - id: cyclic_rate_multiplier
    type: f4
    if: (flags & 4096) != 0
  - id: explosion_timer
    type: s4
    if: (flags & 65536) != 0
  - id: max_angle
    type: f4
    if: (flags & 131072) != 0
  - id: blood_level
    type: f4
    if: (flags & 262144) != 0
  - id: weapon_sprite
    type: common::string_utf
    if: (flags & 4194304) != 0
  - id: min_sight_range
    type: f4
    if: (flags & 8388608) != 0
  - id: max_sight_range
    type: f4
    if: (flags & 16777216) != 0
  - id: num_attachment_ids
    type: u1
    if: (flags & 33554432) != 0
  - id: attachment_ids
    type: u2
    repeat: expr
    repeat-expr: num_attachment_ids
    if: (flags & 33554432) != 0
instances:
  contains_clip:
    value: (flags & 524288) != 0
  round_chambered:
    value: (flags & 1048576) != 0
  jammed:
    value: (flags & 2097152) != 0
