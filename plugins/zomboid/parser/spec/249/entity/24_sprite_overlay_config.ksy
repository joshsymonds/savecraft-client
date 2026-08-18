meta:
  id: sprite_overlay_config
  endian: be
  imports:
    - ../../common/common
params:
  - id: context
    type: any
  - id: world_version
    type: u4
seq:
  # zombie.entity.components.spriteconfig.SpriteOverlayConfig.save/load
  - id: has_script
    type: u1
  - id: script
    type: script_ref
    if: has_script != 0
  - id: script_version
    type: u8
    if: has_script != 0
  - id: has_style
    type: u1
  - id: style
    type: common::string_utf
    if: has_style != 0
types:
  script_ref:
    seq:
      - id: present
        type: u1
      - id: id
        type: u2
        if: present != 0
