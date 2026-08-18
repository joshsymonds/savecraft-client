meta:
  id: television
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
  - id: television
    type: iso_object_shared::iso_wave_signal
