meta:
  id: butcher_hook
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  # IsoButcherHook.load only calls super.load
  # No additional fields are read
  []
