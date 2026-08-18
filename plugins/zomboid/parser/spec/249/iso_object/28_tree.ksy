meta:
  id: tree
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: log_yield
    type: u1
  - id: damage_raw
    type: u1
instances:
  damage:
    value: damage_raw * 10
