meta:
  id: molotov_cocktail
  endian: be
params:
  - id: context
    type: any
  - id: world_version
    type: u4
  - id: debug
    type: u1
seq:
  # IsoMolotovCocktail inherits from MovingObject which overrides IsoObject.load
  # It likely doesn't serialize (no load method override found)
  # Keeping this as empty for completeness
  []
