meta:
  id: iso_object_shared
  endian: be
  imports:
    - ../../common/common
    - ../inventory
types:
  # zombie.iso.IsoMovingObject.save / load
  iso_moving_object:
    seq:
      - id: offset_x
        type: f4
      - id: offset_y
        type: f4
      - id: x
        type: f4
      - id: y
        type: f4
      - id: z
        type: f4
      - id: dir_index
        type: s4
      - id: has_table
        type: u1
      - id: table
        type: common::ktable
        if: has_table == 1

  # zombie.iso.IsoPushableObject.load / save
  iso_pushable_object:
    params:
      - id: context
        type: any
      - id: world_version
        type: u4
      - id: is_iso_wheel_bin
        type: u1
    seq:
      - id: sprite_index
        type: s4
        if: is_iso_wheel_bin == 0
      - id: has_container
        type: u1
      - id: container
        type: inventory::container(context, world_version)
        if: has_container == 1

  # zombie.iso.objects.ClothingDryerLogic.load / save
  clothing_dryer:
    seq:
    - id: activated
      type: u1

  # zombie.iso.objects.ClothingWasherLogic.load / save
  clothing_washer:
    seq:
      - id: activated
        type: u1
      - id: last_update
        type: f4

  # zombie.iso.objects.IsoWaveSignal.load / save
  iso_wave_signal:
    seq:
      - id: has_device_data
        type: u1
      - id: device_data
        type: device_data
        if: has_device_data == 1

  # Device data for radios/TVs
  # zombie.radio.devices.DeviceData.load / save
  device_data:
    seq:
      - id: device_name
        type: common::string_utf
      - id: two_way
        type: u1
      - id: transmit_range
        type: s4
      - id: mic_range
        type: s4
      - id: mic_is_muted
        type: u1
      - id: base_volume_range
        type: f4
      - id: device_volume
        type: f4
      - id: is_portable
        type: u1
      - id: is_television
        type: u1
      - id: is_high_tier
        type: u1
      - id: is_turned_on
        type: u1
      - id: channel
        type: s4
      - id: min_channel_range
        type: s4
      - id: max_channel_range
        type: s4
      - id: is_battery_powered
        type: u1
      - id: has_battery
        type: u1
      - id: power_delta
        type: f4
      - id: use_delta
        type: f4
      - id: headphone_type
        type: s4
      - id: has_presets
        type: u1
      - id: presets
        type: device_presets
        if: has_presets == 1
      - id: media_index
        type: s2
      - id: media_type
        type: u1
      - id: has_media_item
        type: u1
      - id: media_item
        type: common::string_utf
        if: has_media_item == 1
      - id: no_transmit
        type: u1

  # zombie.radio.devices.DevicePresets.load / save
  device_presets:
    seq:
      - id: max_presets
        type: s4
      - id: num_presets
        type: s4
      - id: presets
        type: preset
        repeat: expr
        repeat-expr: num_presets

  preset:
    seq:
      - id: name
        type: common::string_utf
      - id: frequency
        type: s4
