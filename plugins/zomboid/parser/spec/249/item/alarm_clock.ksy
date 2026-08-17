meta:
  id: item_alarm_clock
  endian: be

params:
  - id: context
    type: any
  - id: world_version
    type: u4

# zombie.inventory.types.AlarmClock.save / load
# Extends: InventoryItem
seq:
  - id: alarm_hour
    type: s4
  - id: alarm_minutes
    type: s4
  - id: alarm_set
    type: u1
  - id: ring_since
    type: f4
  - id: force_dont_ring
    type: s4
    if: world_version >= 205
