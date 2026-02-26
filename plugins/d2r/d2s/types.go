package d2s

// D2S represents a fully parsed Diablo II save file.
type D2S struct {
	Version     uint32
	Header      Header
	Attributes  Attributes
	Skills      []Skill
	Items       []Item
	CorpseItems []Item
	MercItems   []Item
	GolemItem   *Item
}

// Header contains the fixed-size character data (bytes 0-764).
type Header struct {
	Name       string
	Status     Status
	Class      Class
	Level      byte
	LastPlayed uint32

	AssignedSkills     [16]uint32
	LeftSkill          uint32
	RightSkill         uint32
	LeftSwapSkill      uint32
	RightSwapSkill     uint32

	CurrentDifficulty  Difficulty
	MapID              uint32
	Mercenary          Mercenary

	// Raw section data preserved for round-tripping.
	QuestData    []byte
	WaypointData []byte
	NPCData      []byte
}

// Status holds character status bit flags (byte 36).
type Status struct {
	Hardcore  bool
	Died      bool
	Expansion bool
	Ladder    bool
}

// Class represents a D2 character class.
type Class byte

const (
	Amazon     Class = 0
	Sorceress  Class = 1
	Necromancer Class = 2
	Paladin    Class = 3
	Barbarian  Class = 4
	Druid      Class = 5
	Assassin   Class = 6
)

func (c Class) String() string {
	switch c {
	case Amazon:
		return "Amazon"
	case Sorceress:
		return "Sorceress"
	case Necromancer:
		return "Necromancer"
	case Paladin:
		return "Paladin"
	case Barbarian:
		return "Barbarian"
	case Druid:
		return "Druid"
	case Assassin:
		return "Assassin"
	default:
		return "Unknown"
	}
}

// Difficulty tracks which difficulty+act the character is in.
type Difficulty struct {
	Active     bool
	Difficulty DiffLevel
	Act        byte
}

// DiffLevel is the difficulty tier.
type DiffLevel byte

const (
	Normal    DiffLevel = 0
	Nightmare DiffLevel = 1
	Hell      DiffLevel = 2
)

func (d DiffLevel) String() string {
	switch d {
	case Normal:
		return "Normal"
	case Nightmare:
		return "Nightmare"
	case Hell:
		return "Hell"
	default:
		return "Unknown"
	}
}

// Mercenary holds hired mercenary data.
type Mercenary struct {
	Dead       bool
	ID         uint32
	NameID     uint16
	Type       uint16
	Experience uint32
}

// Attributes holds character stats parsed from the bit-packed section.
type Attributes struct {
	Strength       uint32
	Energy         uint32
	Dexterity      uint32
	Vitality       uint32
	UnusedStats    uint32
	UnusedSkills   uint32
	CurrentHP      uint32 // stored * 256
	MaxHP          uint32 // stored * 256
	CurrentMana    uint32 // stored * 256
	MaxMana        uint32 // stored * 256
	CurrentStamina uint32 // stored * 256
	MaxStamina     uint32 // stored * 256
	Level          uint32
	Experience     uint32
	Gold           uint32
	StashedGold    uint32
}

// Skill represents a single skill allocation.
type Skill struct {
	ID    int
	Name  string
	Level byte
}

// Item represents a parsed item from the save file.
type Item struct {
	// Flags
	Identified   bool
	Socketed     bool
	New          bool
	IsEar        bool
	StarterItem  bool
	SimpleItem   bool
	Ethereal     bool
	Personalized bool
	Runeword     bool

	// Location
	Location  byte // 0=stored, 1=equipped, 2=belt, 4=cursor, 6=socketed
	EquipSlot byte
	PositionX byte
	PositionY byte
	Page      byte // 1=inventory, 4=cube, 5=stash

	// Identity
	Code     string // 4-char item code (e.g. "hax", "cap")
	TypeID   string // "armor", "weapon", "shield", "misc"
	TypeName string // Human-readable name (e.g. "Hand Axe")

	// Extended data (non-simple items only)
	ID        uint32
	ItemLevel byte
	Quality   Quality

	// Quality-specific names
	UniqueName       string
	SetName          string
	RareName         string
	RareName2        string
	MagicPrefixName  string
	MagicSuffixName  string
	RunewordName     string
	PersonalizedName string

	// Quality-specific IDs
	UniqueID    uint64
	SetID       uint64
	MagicPrefix uint64
	MagicSuffix uint64
	RunewordID  uint64

	// Defense / Durability
	Defense       int
	MaxDurability uint
	CurDurability uint

	// Sockets
	TotalSockets  byte
	SocketedItems []Item

	// Properties
	MagicAttributes    []MagicAttribute
	RunewordAttributes []MagicAttribute
	SetAttributes      [][]MagicAttribute

	// Quantity (for stackable items)
	Quantity uint

	// Weapon damage (resolved from item code)
	BaseDamage *WeaponDamage

	// Ear data (PvP kill trophy)
	EarClass byte
	EarLevel byte
	EarName  string

	// Item version from the bitstream (not the file version)
	Version uint
}

// Quality represents the quality tier of an item.
type Quality byte

const (
	QualityLow     Quality = 1
	QualityNormal  Quality = 2
	QualityHigh    Quality = 3
	QualityMagical Quality = 4
	QualitySet     Quality = 5
	QualityRare    Quality = 6
	QualityUnique  Quality = 7
	QualityCrafted Quality = 8
)

func (q Quality) String() string {
	switch q {
	case QualityLow:
		return "Low Quality"
	case QualityNormal:
		return "Normal"
	case QualityHigh:
		return "Superior"
	case QualityMagical:
		return "Magical"
	case QualitySet:
		return "Set"
	case QualityRare:
		return "Rare"
	case QualityUnique:
		return "Unique"
	case QualityCrafted:
		return "Crafted"
	default:
		return "Unknown"
	}
}

// MagicAttribute represents a decoded magical property on an item.
type MagicAttribute struct {
	ID     uint64
	Name   string
	Values []int64
}

// WeaponDamage holds base weapon damage values.
type WeaponDamage struct {
	Min1H uint
	Max1H uint
	Min2H uint
	Max2H uint
}

// MagicPropertyDef describes how to read a magical property from the bit stream.
type MagicPropertyDef struct {
	Bits []uint  // Bit lengths for each value component
	Bias int64   // Offset subtracted from decoded values
	Name string  // Display template with {0}, {1} placeholders
}
