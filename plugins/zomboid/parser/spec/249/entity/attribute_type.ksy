meta:
  id: attribute_type
  endian: be
  imports:
    - ../../common/common
types:
  # zombie.entity.components.attributes.AttributeType.Bool
  bool:
    params:
      - id: name
        type: str
    seq:
      - id: raw
        type: u1
    instances:
      value:
        value: raw == 1

  # zombie.entity.components.attributes.AttributeType.Byte
  byte:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: s1

  # zombie.entity.components.attributes.AttributeType.Short
  short:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: s2

  # zombie.entity.components.attributes.AttributeType.Int
  int:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: s4

  # zombie.entity.components.attributes.AttributeType.Long
  long:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: s8

  # zombie.entity.components.attributes.AttributeType.Float
  float:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: f4

  # zombie.entity.components.attributes.AttributeType.Double
  double:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: f8

  # zombie.entity.components.attributes.AttributeType.String
  string:
    params:
      - id: name
        type: str
    seq:
      - id: value
        type: common::string_utf

  # zombie.entity.components.attributes.AttributeType.Enum
  enum:
    params:
      - id: name
        type: str
    seq:
      # type id
      - id: value
        type: u1

  # zombie.entity.components.attributes.AttributeType.EnumSet
  enum_set:
    params:
      - id: name
        type: str
    seq:
      - id: num_values
        type: u1
      - id: values
        type: u1
        repeat: expr
        repeat-expr: num_values

  # zombie.entity.components.attributes.AttributeType.EnumStringSet
  enum_string_set:
    params:
      - id: name
        type: str
    seq:
      - id: num_id_values
        type: u1
      - id: id_values
        type: u1
        repeat: expr
        repeat-expr: num_id_values
      - id: num_string_values
        type: u1
      - id: string_values
        type: common::string_utf
        repeat: expr
        repeat-expr: num_string_values
