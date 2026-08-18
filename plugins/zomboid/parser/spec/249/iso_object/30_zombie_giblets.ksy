meta:
  id: zombie_giblets
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  # IsoZombieGiblets.Serialize() returns false
  # This class should never be serialized to world files
  # Keeping this as empty for completeness
  []
