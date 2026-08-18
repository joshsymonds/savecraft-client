meta:
  id: wheelie_bin
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
  - id: bin
    type: iso_object_shared::iso_pushable_object(context, world_version, 1)
