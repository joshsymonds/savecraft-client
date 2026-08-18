meta:
  id: clothing_dryer
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
  - id: dryer
    type: iso_object_shared::clothing_dryer
