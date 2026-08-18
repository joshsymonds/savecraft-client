meta:
  id: combination_washer_dryer
  endian: be
  imports:
    - iso_object_shared
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: mode
    type: u1
  - id: washer
    type: iso_object_shared::clothing_washer
  - id: dryer
    type: iso_object_shared::clothing_dryer
instances:
  is_washer_mode:
    value: mode == 0
  is_dryer_mode:
    value: mode == 1
