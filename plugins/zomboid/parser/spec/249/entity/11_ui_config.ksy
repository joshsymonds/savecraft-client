meta:
  id: ui_config
  endian: be
  imports:
    - ../../common/common
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.ui.UiConfig.save/load
  - id: has_skin
    type: u1
  - id: xui_skin_name
    type: common::string_utf
    if: has_skin != 0
  - id: has_style
    type: u1
  - id: entity_style_name
    type: common::string_utf
    if: has_style != 0
  - id: ui_enabled
    type: u1
