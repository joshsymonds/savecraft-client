// Package blob decodes a Project Zomboid localPlayers.data character BLOB.
//
// It is a hand-written port of the vendored Kaitai closure in ../spec: every
// function below mirrors one .ksy type and reads its fields in spec order,
// with the world-version gates evaluated for version 249. Decoding is strict —
// an out-of-range read, a malformed value, or a single unconsumed trailing
// byte is an error.
package blob

import "fmt"

const (
	// supportedWorldVersion is the only version the vendored spec covers
	// (spec/249, Project Zomboid 42.20.x).
	supportedWorldVersion = 249
	// playerClassID is IsoObject.factoryFromFileInput's id for IsoPlayer.
	playerClassID = 1
	// numBodyParts is body_damage.body_parts' fixed length.
	numBodyParts = 17
	// colorRGBSize is the width of common::color_rgb.
	colorRGBSize = 3
	// craftKeyCharSize is the width of one craft_history_entry key char (u2).
	craftKeyCharSize = 2
	// numItemVisualArrays is how many length-prefixed u1 arrays close
	// item_visual: blood, dirt, holes, basic_patches, denim_patches and
	// leather_patches.
	numItemVisualArrays = 6
)

// common::kobject type tags.
const (
	kobjectString = 0
	kobjectNumber = 1
	kobjectTable  = 2
	kobjectByte   = 3
)

// visual::human_visual flags1 and flags2 bits.
const (
	humanVisualBeardColor      = 1 << 1
	humanVisualHairColor       = 1 << 2
	humanVisualSkinColor       = 1 << 3
	humanVisualBeardModel      = 1 << 4
	humanVisualHairModel       = 1 << 5
	humanVisualSkinTextureName = 1 << 6
)

// visual::item_visual flags1 bits.
const (
	itemVisualTint          = 1 << 0
	itemVisualBaseTexture   = 1 << 1
	itemVisualTextureChoice = 1 << 2
	itemVisualHue           = 1 << 3
	itemVisualDecal         = 1 << 4
)

// Decode parses a player BLOB written by world version 249.
func Decode(data []byte, worldVersion uint32) (*Player, error) {
	if worldVersion != supportedWorldVersion {
		return nil, fmt.Errorf("%w %d, want %d", ErrUnsupportedVersion, worldVersion, supportedWorldVersion)
	}
	r := &reader{data: data}
	if err := decodeClassHeader(r); err != nil {
		return nil, err
	}
	player := decodeGameCharacterBase(r)
	decodePlayerTail(r, player)
	if r.err != nil {
		return nil, r.err
	}
	if r.pos != len(data) {
		return nil, fmt.Errorf("%w: consumed %d of %d bytes", ErrTrailingBytes, r.pos, len(data))
	}
	return player, nil
}

// decodeClassHeader ports common::serialized_class_header and admits only the
// IsoPlayer class this package decodes.
func decodeClassHeader(r *reader) error {
	serialize := r.u8()
	if r.err != nil {
		return r.err
	}
	if serialize != 1 {
		return fmt.Errorf("zomboid blob: serialize byte %d, want 1", serialize)
	}
	classID := r.u8()
	if r.err != nil {
		return r.err
	}
	if classID != playerClassID {
		return fmt.Errorf("zomboid blob: class id %d, want %d (IsoPlayer)", classID, playerClassID)
	}
	return nil
}

// decodeGameCharacterBase ports character_shared::game_character_base with
// is_zombie = 0.
func decodeGameCharacterBase(r *reader) *Player {
	player := &Player{}
	decodeMovingObject(r)
	if r.boolean() { // has_descriptor
		player.Descriptor = decodeSurvivorDesc(r)
	}
	decodeHumanVisual(r)
	player.Inventory = decodeContainer(r)
	r.boolean() // asleep
	r.f32()     // force_wake_up_time
	for i := range player.Stats {
		player.Stats[i] = r.f32()
	}
	player.BodyDamage = decodeBodyDamage(r)
	player.XP = decodeCharacterXP(r)
	r.i32()       // left_hand_item_index
	r.i32()       // right_hand_item_index
	r.boolean()   // on_fire
	for range 8 { // depress, beta, pain and sleeping-tablet effects and deltas
		r.f32()
	}
	for range r.count() { // read_books
		r.stringUTF() // full_type
		r.i32()       // already_read_pages
	}
	r.f32() // reduce_infection_power
	player.KnownRecipes = repeat(r, r.count(), (*reader).stringUTF)
	r.i32() // last_hour_sleeped
	for range 3 {
		r.f32() // time_since_last_smoke, beard_grow_timing, hair_grow_timing
	}
	for range 6 { // unlimited_carry .. farming_cheat
		r.boolean()
	}
	r.boolean() // fishing_cheat (world_version >= 202)
	r.boolean() // can_use_brush_tool (>= 217)
	r.boolean() // fast_move_cheat (>= 217)
	r.boolean() // timed_action_instant_cheat
	r.boolean() // unlimited_endurance
	r.boolean() // unlimited_ammo (>= 230)
	player.KnowAllRecipes = r.boolean()
	r.boolean()           // sneaking
	r.boolean()           // death_drag_down
	for range r.count() { // read_literatures
		r.stringUTF() // title
		r.i32()       // day
	}
	for range r.count() { // read_print_media (>= 222)
		r.stringUTF()
	}
	r.i64()            // last_animal_pet
	r.bytes(r.count()) // player_cheats: cheat ids (>= 231)
	return player
}

// decodeMovingObject ports iso_object_shared::iso_moving_object.
func decodeMovingObject(r *reader) {
	for range 5 {
		r.f32() // offset_x, offset_y, x, y, z
	}
	r.i32()          // dir_index
	if r.boolean() { // has_table
		decodeKTable(r)
	}
}

// decodeKTable ports common::ktable, the serialized Lua table attached to the
// object; its contents are game script state the caller does not read.
func decodeKTable(r *reader) {
	for range r.count() {
		if r.err != nil {
			return
		}
		decodeKObject(r) // key
		decodeKObject(r) // value
	}
}

func decodeKObject(r *reader) {
	switch kind := r.u8(); kind {
	case kobjectString:
		r.stringUTF()
	case kobjectNumber:
		r.f64()
	case kobjectTable:
		decodeKTable(r)
	case kobjectByte:
		r.u8()
	default:
		if r.err == nil {
			r.fail(fmt.Errorf("zomboid blob: ktable object type %d at offset %d", kind, r.pos-1))
		}
	}
}

// decodeSurvivorDesc ports character_shared::survivor_desc.
func decodeSurvivorDesc(r *reader) *Descriptor {
	desc := &Descriptor{}
	desc.ID = r.i32()
	desc.Forename = r.stringUTF()
	desc.Surname = r.stringUTF()
	desc.Torso = r.stringUTF()
	desc.Female = r.i32() == 1
	desc.Profession = r.stringUTF()
	if r.i32() == 1 { // has_extras
		desc.Extras = repeat(r, r.count(), (*reader).stringUTF)
	}
	desc.XPBoosts = repeat(r, r.count(), decodeXPBoost)
	// voice fields: world_version >= 208.
	desc.VoicePrefix = r.stringUTF()
	desc.VoicePitch = r.f32()
	desc.VoiceType = r.i32()
	return desc
}

func decodeXPBoost(r *reader) XPBoost {
	var boost XPBoost
	boost.Perk = r.stringUTF()
	boost.Level = r.i32()
	return boost
}

// decodeHumanVisual ports visual::human_visual. The appearance it describes is
// not part of the character sheet, so it is consumed but not surfaced.
func decodeHumanVisual(r *reader) {
	flags1 := r.u8()
	readColorIf(r, flags1, humanVisualHairColor)
	readColorIf(r, flags1, humanVisualBeardColor)
	readColorIf(r, flags1, humanVisualSkinColor)
	r.u8() // body_hair
	r.u8() // skin_texture
	r.u8() // zombie_rot_stage
	readStringIf(r, flags1, humanVisualSkinTextureName)
	readStringIf(r, flags1, humanVisualBeardModel)
	readStringIf(r, flags1, humanVisualHairModel)
	for range 3 {
		r.bytes(int(r.u8())) // blood, dirt, holes
	}
	for range int(r.u8()) { // body_visuals
		decodeItemVisual(r)
	}
	r.stringUTF() // non_attached_hair
	flags2 := r.u8()
	readColorIf(r, flags2, humanVisualHairColor)  // natural_hair_color
	readColorIf(r, flags2, humanVisualBeardColor) // natural_beard_color
}

// decodeItemVisual ports visual::item_visual.
func decodeItemVisual(r *reader) {
	flags1 := r.u8()
	r.stringUTF() // full_type
	r.stringUTF() // alternate_model_name
	r.stringUTF() // clothing_item_name
	readColorIf(r, flags1, itemVisualTint)
	if flags1&itemVisualBaseTexture != 0 {
		r.u8()
	}
	if flags1&itemVisualTextureChoice != 0 {
		r.u8()
	}
	if flags1&itemVisualHue != 0 {
		r.f32()
	}
	readStringIf(r, flags1, itemVisualDecal)
	for range numItemVisualArrays {
		r.bytes(int(r.u8()))
	}
}

func readColorIf(r *reader, flags, mask uint8) {
	if flags&mask != 0 {
		r.bytes(colorRGBSize)
	}
}

func readStringIf(r *reader, flags, mask uint8) {
	if flags&mask != 0 {
		r.stringUTF()
	}
}

// decodeContainer ports inventory::container, keeping the registry id and
// count of each top-level item group.
func decodeContainer(r *reader) []InventoryItem {
	r.stringUTF() // type_name
	r.boolean()   // explored
	items := repeat(r, int(r.u16()), decodeItemGroup)
	r.boolean() // has_been_looted
	r.i32()     // capacity
	return items
}

// decodeItemGroup ports inventory::group. Past the leading registry id the
// item blob holds inventory::item_base and a subclass selected by a dictionary
// lookup on that id, so the rest of the blob is consumed unread.
func decodeItemGroup(r *reader) InventoryItem {
	var item InventoryItem
	identical := r.count()
	item.Count = int32(identical)
	size := r.byteLen() // sized_blob.len_data
	end := r.pos + size
	item.RegistryID = r.u16()
	r.skipTo(end)
	for range max(identical-1, 0) {
		r.i32() // duplicate_ids
	}
	return item
}

// decodeBodyDamage ports character_shared::body_damage.
func decodeBodyDamage(r *reader) BodyDamage {
	var damage BodyDamage
	damage.Parts = repeat(r, numBodyParts, decodeBodyPart)
	damage.CatchACold = r.f32()
	damage.HasACold = r.boolean()
	damage.ColdStrength = r.f32()
	damage.TimeToSneezeOrCough = r.i32() // world_version >= 222
	damage.ReduceFakeInfection = r.boolean()
	damage.HealthFromFoodTimer = r.f32()
	damage.PainReduction = r.f32()
	damage.ColdReduction = r.f32()
	damage.InfectionTime = r.f32()
	damage.InfectionMortalityDuration = r.f32()
	damage.ColdDamageStage = r.f32()
	if r.boolean() { // has_thermoregulator
		decodeThermoregulator(r)
	}
	return damage
}

// decodeBodyPart ports character_shared::body_part.
func decodeBodyPart(r *reader) BodyPart {
	var part BodyPart
	part.IsCut = r.boolean()
	part.IsBitten = r.boolean()
	part.IsScratched = r.boolean()
	part.IsBandaged = r.boolean()
	part.IsBleeding = r.boolean()
	part.IsDeepWounded = r.boolean()
	part.IsFakeInfected = r.boolean()
	part.IsInfected = r.boolean()
	part.Health = r.f32()
	if part.IsBandaged {
		part.BandageLife = r.f32()
	}
	part.IsInfectedWound = r.boolean()
	if part.IsInfectedWound {
		part.WoundInfectionLevel = r.f32()
	}
	part.CutTime1 = r.f32()
	part.BiteTime = r.f32()
	part.ScratchTime = r.f32()
	part.BleedingTime = r.f32()
	part.AlcoholLevel = r.f32()
	part.AdditionalPain = r.f32()
	part.DeepWoundTime = r.f32()
	part.HaveGlass = r.boolean()
	part.GetBandageXP = r.boolean()
	part.Stitched = r.boolean()
	part.StitchTime = r.f32()
	part.GetStitchXP = r.boolean()
	part.GetSplintXP = r.boolean()
	part.FractureTime = r.f32()
	part.IsSplint = r.boolean()
	if part.IsSplint {
		part.SplintFactor = r.f32()
	}
	part.HaveBullet = r.boolean()
	part.BurnTime = r.f32()
	part.NeedBurnWash = r.boolean()
	part.LastTimeBurnWash = r.f32()
	part.SplintItem = r.stringUTF()
	part.BandageType = r.stringUTF()
	part.CutTime2 = r.f32()
	part.Wetness = r.f32()
	part.Stiffness = r.f32()
	// comfrey, garlic and plantain factors: world_version >= 227.
	part.ComfreyFactor = r.f32()
	part.GarlicFactor = r.f32()
	part.PlantainFactor = r.f32()
	return part
}

// decodeThermoregulator ports character_shared::thermoregulator, whose per-node
// heat model the character sheet does not use.
func decodeThermoregulator(r *reader) {
	for range 9 { // set_point .. damage_counter, plus the >= 243 and >= 249 fields
		r.f32()
	}
	for range r.count() {
		if r.err != nil {
			return
		}
		r.i32() // node_index
		for range 9 {
			r.f32() // celsius .. clothing_wetness, including the >= 241 and >= 243 fields
		}
	}
}

// decodeCharacterXP ports character_shared::character_xp.
func decodeCharacterXP(r *reader) XP {
	var xp XP
	xp.Traits = repeat(r, r.count(), (*reader).stringUTF) // character_traits
	xp.TotalXP = r.f32()
	xp.Level = r.i32()
	xp.LastLevel = r.i32()
	xp.Entries = repeat(r, r.count(), decodePerkXP)
	xp.PerkLevels = repeat(r, r.count(), decodePerkLevel)
	xp.Multipliers = repeat(r, r.count(), decodePerkMultiplier)
	return xp
}

func decodePerkXP(r *reader) PerkXP {
	var entry PerkXP
	entry.Perk = r.stringUTF()
	entry.XP = r.f32()
	return entry
}

func decodePerkLevel(r *reader) PerkLevel {
	var entry PerkLevel
	entry.Perk = r.stringUTF()
	entry.Level = r.i32()
	return entry
}

func decodePerkMultiplier(r *reader) PerkMultiplier {
	var entry PerkMultiplier
	entry.Perk = r.stringUTF()
	entry.Multiplier = r.f32()
	entry.MinLevel = r.i8()
	entry.MaxLevel = r.i8()
	return entry
}

// decodePlayerTail ports the IsoPlayer subclass fields in 1_player.ksy.
func decodePlayerTail(r *reader, player *Player) {
	player.HoursSurvived = r.f64()
	player.ZombieKills = r.i32()
	player.WornItems = repeat(r, int(r.u8()), decodeWornItem)
	player.LeftHandIndex = r.i16()
	player.RightHandIndex = r.i16()
	player.SurvivorKills = r.i32()
	player.Nutrition = decodeNutrition(r)
	player.AllChatMuted = r.boolean()
	player.TagPrefix = r.stringUTF()
	for range 3 {
		r.f32() // tag_color_r, tag_color_g, tag_color_b
	}
	player.DisplayName = r.stringUTF()
	player.ShowTag = r.boolean()
	player.FactionPVP = r.boolean()
	player.AutoDrink = r.boolean() // world_version >= 239
	player.ExtraInfoFlags = r.u8()
	if r.boolean() { // has_saved_vehicle
		r.f32() // saved_vehicle_x
		r.f32() // saved_vehicle_y
		r.i8()  // saved_vehicle_seat
		r.u8()  // saved_vehicle_running
	}
	for range r.count() { // mechanics_items
		r.i64() // key
		r.i64() // value
	}
	decodeFitness(r)
	player.AlreadyReadBooks = repeat(r, r.count16(), (*reader).i16)
	for range r.count16() { // known_media_lines: guids
		r.stringUTF()
	}
	player.VoiceType = r.u8() // world_version >= 203
	decodeCraftHistory(r)     // world_version >= 228
}

func decodeWornItem(r *reader) WornItem {
	var worn WornItem
	worn.BodyLocation = r.stringUTF()
	worn.ItemIndex = r.i16()
	return worn
}

// decodeNutrition ports character_shared::nutrition_data.
func decodeNutrition(r *reader) Nutrition {
	var nutrition Nutrition
	nutrition.Calories = r.f32()
	nutrition.Proteins = r.f32()
	nutrition.Lipids = r.f32()
	nutrition.Carbohydrates = r.f32()
	nutrition.Weight = r.f32()
	return nutrition
}

// decodeFitness ports character_shared::fitness_data, the exercise bookkeeping
// behind the fitness perk.
func decodeFitness(r *reader) {
	for range r.count() { // stiffness_inc
		r.stringUTF()
		r.f32()
	}
	for range r.count() { // stiffness_timer
		r.stringUTF()
		r.i32()
	}
	for range r.count() { // regularity
		r.stringUTF()
		r.f32()
	}
	for range r.count() { // bodypart_to_inc_stiffness
		r.stringUTF()
	}
	for range r.count() { // exe_timer
		r.stringUTF()
		r.i64()
	}
}

// decodeCraftHistory ports character_shared::craft_history_data.
func decodeCraftHistory(r *reader) {
	for range r.count() {
		if r.err != nil {
			return
		}
		r.bytes(craftKeyCharSize * r.count()) // key_chars
		r.i32()                               // craft_count
		r.f64()                               // last_craft_time
	}
}

// repeat decodes n elements, stopping as soon as the reader fails.
func repeat[T any](r *reader, n int, decode func(*reader) T) []T {
	out := make([]T, 0, n)
	for range n {
		if r.err != nil {
			return out
		}
		out = append(out, decode(r))
	}
	return out
}
