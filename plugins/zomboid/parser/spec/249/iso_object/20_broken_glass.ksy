meta:
  id: broken_glass
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  # IsoBrokenGlass.load only calls super.load, no additional fields
  []
