meta:
  id: blood_splat
  endian: be
types:
  # iso.IsoFloorBloodSplat.save / load
  floor:
    seq:
      - id: x_byte
        type: u1
      - id: y_byte
        type: u1
      - id: z_byte
        type: u1
      - id: type_id
        type: u1
      - id: world_age
        type: f4
      - id: index
        type: u1

  # iso.IsoWallBloodSplat.save / load
  wall:
    seq:
      - id: world_age
        type: f4
      - id: sprite_id
        type: s4
