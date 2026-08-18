# pzdataspec closure (world version 249)

`249/` and `common/` hold the [pzdataspec](https://github.com/cff29546/pzdataspec)
Kaitai Struct definitions that describe a Project Zomboid save, reduced to the
complete transitive import closure of the `249/iso_object.ksy` root — the file a
`localPlayers.data` player BLOB is parsed from. Kaitai imports are file-scoped,
so the closure carries every `iso_object/` subclass and every `entity/` and
`item/` component those files import, not only the class-1 (`IsoPlayer`) path
the decoder walks. The files are copied verbatim from upstream — see
`UPSTREAM.txt` for the commit and `LICENSE.pzdataspec` for the terms. Do not
edit them.

`../blob/decode.go` is a hand-written port of that closure, not generated code.
Every decode function mirrors one `.ksy` type, reads its fields in spec order,
and evaluates each `if: world_version >= N` gate for version 249; the comments
name the spec type or field wherever the mapping is not obvious. The port is
strict, so it is also a check on the closure: an out-of-range read or a single
unconsumed trailing byte fails the decode.

## Bumping the spec

A new world version never edits these files. Vendor the new upstream closure
into a sibling `spec/<version>/` directory, commit a real save blob captured
from that version under `../testdata/`, and pin the decoder's goldens to it.
Existing spec directories and fixtures stay immutable, so a version that once
decoded keeps decoding.

## Vendored files

`../blob/spec_test.go` walks the import graph from `249/iso_object.ksy` and
fails if this list, the files on disk, and that closure ever disagree.

```
249/animal.ksy
249/base_vehicle.ksy
249/blood_splat.ksy
249/entity.ksy
249/entity/11_ui_config.ksy
249/entity/12_craft_logic.ksy
249/entity/13_furnace_logic.ksy
249/entity/14_test_component.ksy
249/entity/15_mashing_logic.ksy
249/entity/16_drying_logic.ksy
249/entity/17_meta_tag_component.ksy
249/entity/18_resources.ksy
249/entity/19_craft_bench.ksy
249/entity/1_attribute_container.ksy
249/entity/20_craft_recipe_component.ksy
249/entity/21_durability.ksy
249/entity/22_drying_craft_logic.ksy
249/entity/23_context_menu_config.ksy
249/entity/24_sprite_overlay_config.ksy
249/entity/25_craft_bench_sounds.ksy
249/entity/26_wall_covering_config.ksy
249/entity/2_fluid_container.ksy
249/entity/3_sprite_config.ksy
249/entity/6_lua_component.ksy
249/entity/7_parts.ksy
249/entity/8_signals.ksy
249/entity/9_entity_script_info.ksy
249/entity/attribute_type.ksy
249/entity/entity_shared.ksy
249/inventory.ksy
249/iso_object.ksy
249/iso_object/10_television.ksy
249/iso_object/11_dead_body.ksy
249/iso_object/12_barbecue.ksy
249/iso_object/13_clothing_dryer.ksy
249/iso_object/14_clothing_washer.ksy
249/iso_object/15_fireplace.ksy
249/iso_object/16_stove.ksy
249/iso_object/17_door.ksy
249/iso_object/18_thumpable.ksy
249/iso_object/19_trap.ksy
249/iso_object/1_player.ksy
249/iso_object/20_broken_glass.ksy
249/iso_object/21_car_battery_charger.ksy
249/iso_object/22_generator.ksy
249/iso_object/23_compost.ksy
249/iso_object/24_mannequin.ksy
249/iso_object/26_window.ksy
249/iso_object/27_barricade.ksy
249/iso_object/28_tree.ksy
249/iso_object/29_light_switch.ksy
249/iso_object/30_zombie_giblets.ksy
249/iso_object/31_molotov_cocktail.ksy
249/iso_object/32_fire.ksy
249/iso_object/34_combination_washer_dryer.ksy
249/iso_object/35_stacked_washer_dryer.ksy
249/iso_object/37_feeding_trough.ksy
249/iso_object/38_hutch.ksy
249/iso_object/39_animal_track.ksy
249/iso_object/3_zombie.ksy
249/iso_object/40_butcher_hook.ksy
249/iso_object/41_window_frame.ksy
249/iso_object/4_pushable_object.ksy
249/iso_object/5_wheelie_bin.ksy
249/iso_object/6_world_inventory_object.ksy
249/iso_object/7_jukebox.ksy
249/iso_object/8_curtain.ksy
249/iso_object/9_radio.ksy
249/iso_object/character_shared.ksy
249/iso_object/iso_object_shared.ksy
249/item/alarm_clock.ksy
249/item/alarm_clock_clothing.ksy
249/item/animal.ksy
249/item/clothing.ksy
249/item/container.ksy
249/item/food.ksy
249/item/hand_weapon.ksy
249/item/key.ksy
249/item/literature.ksy
249/item/map.ksy
249/item/moveable.ksy
249/item/radio.ksy
249/visual.ksy
common/common.ksy
```
