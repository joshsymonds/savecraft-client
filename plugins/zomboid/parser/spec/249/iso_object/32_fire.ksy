meta:
  id: fire
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  - id: life
    type: s4
  - id: spread_delay
    type: s4
  - id: life_stage
    type: s4
  - id: life_stage_timer
    type: s4
  - id: life_stage_duration
    type: s4
  - id: energy
    type: s4
  - id: num_flame_particles
    type: s4
  - id: spread_timer
    type: s4
  - id: age
    type: s4
  - id: perm
    type: u1
  - id: light_radius
    type: u1
  - id: smoke
    type: u1
