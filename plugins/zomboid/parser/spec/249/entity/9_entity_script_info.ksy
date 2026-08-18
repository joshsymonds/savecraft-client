meta:
  id: entity_script_info
  endian: be
  imports:
    - ../../common/common
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.script.EntityScriptInfo.save/load
  - id: original_is_item
    type: u1
  - id: has_original
    type: u1
  - id: original_script
    type: common::string_utf
    if: has_original != 0
