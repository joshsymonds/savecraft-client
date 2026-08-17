package blob

// Descriptor is character_shared::survivor_desc — the character's identity.
type Descriptor struct {
	ID          int32
	Forename    string
	Surname     string
	Torso       string
	Female      bool
	Profession  string
	Extras      []string
	XPBoosts    []XPBoost
	VoicePrefix string
	VoicePitch  float32
	VoiceType   int32
}

// XPBoost is character_shared::xp_boost_entry — starting XP granted by the
// profession or a trait.
type XPBoost struct {
	Perk  string
	Level int32
}

// XP is character_shared::character_xp — traits and the perk tables.
type XP struct {
	Traits      []string
	TotalXP     float32
	Level       int32
	LastLevel   int32
	Entries     []PerkXP
	PerkLevels  []PerkLevel
	Multipliers []PerkMultiplier
}

// PerkXP is character_shared::perk_xp_entry.
type PerkXP struct {
	Perk string
	XP   float32
}

// PerkLevel is character_shared::perk_level_entry.
type PerkLevel struct {
	Perk  string
	Level int32
}

// PerkMultiplier is character_shared::perk_multiplier_entry.
type PerkMultiplier struct {
	Perk       string
	Multiplier float32
	MinLevel   int8
	MaxLevel   int8
}

// Stats is character_shared::character_stats.ordered_stats. The index-to-name
// mapping is documented in the spec and applied by the caller.
type Stats [24]float32

// BodyDamage is character_shared::body_damage.
type BodyDamage struct {
	Parts                      []BodyPart
	CatchACold                 float32
	HasACold                   bool
	ColdStrength               float32
	TimeToSneezeOrCough        int32
	ReduceFakeInfection        bool
	HealthFromFoodTimer        float32
	PainReduction              float32
	ColdReduction              float32
	InfectionTime              float32
	InfectionMortalityDuration float32
	ColdDamageStage            float32
}

// BodyPart is character_shared::body_part. Fields guarded by a flag in the
// spec hold the zero value when that flag is unset.
type BodyPart struct {
	IsCut               bool
	IsBitten            bool
	IsScratched         bool
	IsBandaged          bool
	IsBleeding          bool
	IsDeepWounded       bool
	IsFakeInfected      bool
	IsInfected          bool
	Health              float32
	BandageLife         float32
	IsInfectedWound     bool
	WoundInfectionLevel float32
	CutTime1            float32
	BiteTime            float32
	ScratchTime         float32
	BleedingTime        float32
	AlcoholLevel        float32
	AdditionalPain      float32
	DeepWoundTime       float32
	HaveGlass           bool
	GetBandageXP        bool
	Stitched            bool
	StitchTime          float32
	GetStitchXP         bool
	GetSplintXP         bool
	FractureTime        float32
	IsSplint            bool
	SplintFactor        float32
	HaveBullet          bool
	BurnTime            float32
	NeedBurnWash        bool
	LastTimeBurnWash    float32
	SplintItem          string
	BandageType         string
	CutTime2            float32
	Wetness             float32
	Stiffness           float32
	ComfreyFactor       float32
	GarlicFactor        float32
	PlantainFactor      float32
}

// InventoryItem is one inventory::group of identical top-level items. Only the
// leading registry id of the group's item blob is decoded; resolving it to a
// type and name is a dictionary lookup the caller owns.
type InventoryItem struct {
	RegistryID uint16
	Count      int32
}

// WornItem is character_shared::worn_item_entry. ItemIndex points into the
// character's top-level inventory.
type WornItem struct {
	BodyLocation string
	ItemIndex    int16
}

// Nutrition is character_shared::nutrition_data, in spec field order.
type Nutrition struct {
	Calories      float32
	Proteins      float32
	Lipids        float32
	Carbohydrates float32
	Weight        float32
}

// Player is a decoded IsoPlayer: the character_shared::game_character_base
// fields followed by the 1_player.ksy tail.
type Player struct {
	Descriptor       *Descriptor
	Inventory        []InventoryItem
	Stats            Stats
	BodyDamage       BodyDamage
	XP               XP
	KnownRecipes     []string
	KnowAllRecipes   bool
	HoursSurvived    float64
	ZombieKills      int32
	SurvivorKills    int32
	WornItems        []WornItem
	LeftHandIndex    int16
	RightHandIndex   int16
	Nutrition        Nutrition
	AllChatMuted     bool
	TagPrefix        string
	DisplayName      string
	ShowTag          bool
	FactionPVP       bool
	AutoDrink        bool
	ExtraInfoFlags   uint8
	AlreadyReadBooks []int16
	VoiceType        uint8
}
