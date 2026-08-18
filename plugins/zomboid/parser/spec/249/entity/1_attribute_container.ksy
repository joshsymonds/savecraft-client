meta:
  id: attribute_container
  endian: be
  imports:
    - ../../common/common
    - attribute_type
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.attributes.AttributeContainer.load / save
  - id: saved_mode
    type: u1
  - id: data
    type:
      switch-on: saved_mode
      cases:
        1: attribute_storages
        _: attribute_list((saved_mode & 8) != 0)
    if: saved_mode != 0
types:
  attribute_storages:
    seq:
      - id: num_storages
        type: u1
      - id: headers
        type: u8
        repeat: expr
        repeat-expr: num_storages * 1
      - id: attribute_storages
        type: attribute_storage(headers[_index], _index)
        repeat: expr
        repeat-expr: num_storages * 1

  attribute_storage:
    params:
      - id: header
        type: u8
      - id: header_index
        type: u4
    seq:
      - id: attributes
        type: attribute_from_header(header, header_index, _index)
        repeat: expr
        repeat-expr: 64

  attribute_from_header:
    params:
      - id: header
        type: u8
      - id: header_index
        type: u4
      - id: bit_index
        type: u4
    seq:
      - id: attribute
        type: attribute(type_id.as<u2>)
        if: (header & (1 << bit_index)) != 0
    instances:
      type_id:
        value: header_index * 64 + bit_index

  attribute_list:
    params:
      - id: is_short_count
        type: b1
    seq:
      - id: num_attributes
        type: u2
      - id: attributes
        type: attribute_instance(is_short_count)
        repeat: expr
        repeat-expr: num_attributes

  attribute_instance:
    params:
      - id: is_short_count
        type: b1
    seq:
      - id: type_id
        type:
          switch-on: is_short_count
          cases:
            true: u1
            false: u2
      - id: attribute
        type: attribute(type_id.as<u2>)

  attribute:
    params:
      - id: type_id
        type: u2
    seq:
      - id: value
        type:
          switch-on: type_id
          cases:
            0: attribute_type::float("Sharpness")
            1: attribute_type::int("HeadCondition")
            2: attribute_type::int("HeadConditionMax")
            3: attribute_type::int("Quality")
            4: attribute_type::int("TimesHeadRepaired")
            5: attribute_type::int("OriginX")
            6: attribute_type::int("OriginY")
            7: attribute_type::int("OriginZ")
            100: attribute_type::string("TestString")
            102: attribute_type::string("TestString2")
            103: attribute_type::float("TestQuality")
            104: attribute_type::float("TestCondition")
            105: attribute_type::bool("TestBool")
            106: attribute_type::int("TestUses")
            121: attribute_type::enum("TestItemType")
            123: attribute_type::enum_set("TestCategories")
            124: attribute_type::enum_string_set("TestTags")
            _: common::unknown(type_id)
        doc: |-
          Dynamic zombie.entity.components.attributes.AttributeType, 
              the type is determined by zombie.entity.components.attributes.Attribute.TypeFromId(short typeId)
          Each type is registered in zombie.entity.components.attributes.Attribute class definition
              via zombie.entity.components.attributes.Attribute.registerType(AttributeType type)
