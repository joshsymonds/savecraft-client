meta:
  id: generator
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: connected
    type: u1
  - id: activated
    type: u1
  - id: raw_fuel
    type: f4
  - id: condition
    type: s4
  - id: last_hour
    type: s4
instances:
  fuel:
    value: 'raw_fuel > 10.0 ? 10.0 : raw_fuel'
