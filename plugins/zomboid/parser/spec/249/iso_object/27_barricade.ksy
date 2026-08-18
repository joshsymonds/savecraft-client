meta:
  id: barricade
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  # NOTE: IsoBarricade does NOT call super.load()
  - id: dir_index
    type: u1
  - id: num_planks
    type: u1
  - id: plank_healths
    type: s2
    repeat: expr
    repeat-expr: num_plank_healths
    doc: "Only first 4 values are used in game logic"
  - id: metal_health
    type: s2
  - id: metal_bar_health
    type: s2
instances:
  num_plank_healths:
    value: num_planks
