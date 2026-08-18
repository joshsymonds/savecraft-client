// Command parser reads a Project Zomboid save directory — players.db,
// WorldDictionaryReadable.lua and mods.txt, handed over as a tar on stdin — and
// writes the character sheet to stdout as the ndjson lines docs/plugins.md
// specifies.
package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/joshsymonds/savecraft-client/plugins/zomboid/parser/blob"
	"github.com/joshsymonds/savecraft-client/plugins/zomboid/parser/dict"
	"github.com/joshsymonds/savecraft-client/plugins/zomboid/parser/sqlite"
)

const characterDescription = "Character sheet decoded from players.db: identity, skills and XP, traits, health summary, position, worn and carried items, active mods, and other characters in this save"

const (
	// supportedWorldVersion is the save format ../spec/249 describes and
	// blob.Decode ports; anything else is refused rather than guessed at.
	supportedWorldVersion = 249
	playersMember         = "players.db"
	journalMember         = "players.db-journal"
	dictionaryMember      = "WorldDictionaryReadable.lua"
	modsMember            = "mods.txt"
	hoursSurvivedField    = "hoursSurvived"
	aliveDescription      = "alive"
	// fullBodyPartHealth is an undamaged body part's health.
	fullBodyPartHealth = 100
)

// output is one ndjson line of the parser contract: a status, an error, or the result.
type output struct {
	Type      string          `json:"type"`
	Message   string          `json:"message,omitempty"`
	ErrorType string          `json:"errorType,omitempty"`
	Identity  *identity       `json:"identity,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Sections  *outputSections `json:"sections,omitempty"`
}

type identity struct {
	SaveName    string `json:"saveName"`
	GameID      string `json:"gameId"`
	DisplayName string `json:"displayName"`
}

type outputSections struct {
	Character section `json:"character"`
}

type section struct {
	Description string    `json:"description"`
	Data        character `json:"data"`
}

// character is the character sheet emitted as sections.character.data: the primary
// localPlayers row decoded and resolved against the save's item dictionary.
type character struct {
	LocalPlayerID   int64     `json:"localPlayerId"`
	Forename        string    `json:"forename"`
	Surname         string    `json:"surname"`
	Profession      string    `json:"profession"`
	Female          bool      `json:"female"`
	Alive           bool      `json:"alive"`
	Health          health    `json:"health"`
	Position        position  `json:"position"`
	HoursSurvived   float64   `json:"hoursSurvived"`
	ZombieKills     int32     `json:"zombieKills"`
	SurvivorKills   int32     `json:"survivorKills"`
	Traits          []string  `json:"traits"`
	Perks           []perk    `json:"perks"`
	XPBoosts        []boost   `json:"xpBoosts"`
	Nutrition       nutrition `json:"nutrition"`
	WornItems       []worn    `json:"wornItems"`
	CarriedItems    []carried `json:"carriedItems"`
	ActiveMods      []string  `json:"activeMods"`
	OtherCharacters []other   `json:"otherCharacters"`
}
type health struct {
	MinBodyPartHealth float64 `json:"minBodyPartHealth"`
	InjuredBodyParts  int     `json:"injuredBodyParts"`
	Infected          bool    `json:"infected"`
}

// position is the character's world position: in-cell coordinates plus the cell it sits in.
type position struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	CellX int64   `json:"cellX"`
	CellY int64   `json:"cellY"`
}

type perk struct {
	Name  string  `json:"name"`
	Level int32   `json:"level"`
	XP    float32 `json:"xp"`
}

// boost is a per-perk XP multiplier chosen at character creation.
type boost struct {
	Perk  string `json:"perk"`
	Level int32  `json:"level"`
}

type nutrition struct {
	Calories      float32 `json:"calories"`
	Carbohydrates float32 `json:"carbohydrates"`
	Lipids        float32 `json:"lipids"`
	Proteins      float32 `json:"proteins"`
	Weight        float32 `json:"weight"`
}
type worn struct {
	BodyLocation string `json:"bodyLocation"`
	Fulltype     string `json:"fulltype"`
}

type carried struct {
	Fulltype string `json:"fulltype"`
	Count    int32  `json:"count"`
}

// other is a non-primary character in the same save, summarized.
type other struct {
	LocalPlayerID int64   `json:"localPlayerId"`
	Name          string  `json:"name"`
	Alive         bool    `json:"alive"`
	HoursSurvived float64 `json:"hoursSurvived"`
}

// rawCharacter is one localPlayers row: its scalar columns plus the serialized
// character blob, decoded into player once the world version is accepted.
type rawCharacter struct {
	id, wx, wy, worldVersion, isDead int64
	name                             string
	x, y, z                          float64
	data                             []byte
	player                           *blob.Player
}

// writeTo encodes one ndjson line. A line the encoder rejects — a non-finite
// float is the only value in this contract that it can — or a stdout that will
// not take it must not be dropped: the daemon reads the result off these lines
// and would otherwise wait on a run that reported success.
func writeTo(w io.Writer, value output) error {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return fmt.Errorf("encode %s line: %w", value.Type, err)
	}
	return nil
}

func write(value output) {
	if err := writeTo(os.Stdout, value); err != nil {
		fail("parse_error", err.Error())
	}
}

func status(message string) { write(output{Type: "status", Message: message}) }

// fail reports a structured error and exits non-zero. When stdout itself is the
// problem the line cannot be written, so the reason goes to stderr instead.
func fail(errorType, message string) {
	if err := writeTo(os.Stdout, output{
		Type:      "error",
		ErrorType: errorType,
		Message:   message,
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "zomboid parser: %s: %s\n", errorType, message)
	}
	os.Exit(1)
}

func main() {
	archive, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	status("Reading players.db…")
	members := readMembers(archive)
	rawRows := readRows(members[playersMember])
	status(fmt.Sprintf("Decoding %d character(s)…", len(rawRows)))
	decodeRows(rawRows)
	items, err := dict.ParseItems(members[dictionaryMember])
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	sort.Slice(rawRows, func(i, j int) bool { return rawRows[i].id < rawRows[j].id })
	primary := rawRows[0]
	data, dataErr := makeCharacter(primary, rawRows[1:], items, activeMods(members))
	if dataErr != nil {
		fail("corrupt_file", dataErr.Error())
	}
	name := primary.player.Descriptor.Forename + " " + primary.player.Descriptor.Surname
	saveName := "zomboid-save"
	if len(os.Args) > 1 {
		saveName = os.Args[1]
	}
	profession := strings.TrimPrefix(primary.player.Descriptor.Profession, "base:")
	alive := "dead"
	if primary.isDead == 0 {
		alive = aliveDescription
	}
	write(output{
		Type: "result",
		Identity: &identity{
			SaveName:    saveName,
			GameID:      "zomboid",
			DisplayName: name,
		},
		Summary: fmt.Sprintf("%s (%s) — %.1f h survived, %d zombie kills, %s",
			name, profession, primary.player.HoursSurvived, primary.player.ZombieKills, alive),
		Sections: &outputSections{Character: section{
			Description: characterDescription,
			Data:        data,
		}},
	})
}

// readMembers unpacks the tar the daemon hands over and checks that the save
// is complete and settled: both required members present, and no journal left
// behind by a write that is still in flight.
func readMembers(archive []byte) map[string][]byte {
	members, err := readArchive(archive)
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	for _, name := range []string{playersMember, dictionaryMember} {
		if _, ok := members[name]; !ok {
			fail("corrupt_file", "missing member "+name)
		}
	}
	if len(members[journalMember]) > 0 {
		fail("corrupt_file", journalMember+" is non-empty: a save write is in progress; retry on the next rewrite")
	}
	return members
}

// readRows decodes the localPlayers table and admits it only if every row was
// written by the world version this build's spec closure covers.
func readRows(database []byte) []rawCharacter {
	opened, err := sqlite.Open(database)
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	rows, err := opened.Rows("localPlayers")
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	if len(rows) == 0 {
		fail("parse_error", "localPlayers has no rows")
	}
	rawRows := make([]rawCharacter, 0, len(rows))
	for _, row := range rows {
		parsed, parseErr := parseRow(row)
		if parseErr != nil {
			fail("corrupt_file", parseErr.Error())
		}
		rawRows = append(rawRows, parsed)
	}
	for _, row := range rawRows {
		if row.worldVersion != supportedWorldVersion {
			fail(
				"unsupported_version",
				fmt.Sprintf("row %d has world version %d; this build supports %d (Project Zomboid 42.20.x)",
					row.id, row.worldVersion, supportedWorldVersion),
			)
		}
	}
	return rawRows
}

// decodeRows decodes each row's character blob in place. A row whose blob
// carries no survivor_desc has no identity to report, so it is corrupt rather
// than something to render with empty names.
func decodeRows(rawRows []rawCharacter) {
	for index := range rawRows {
		player, decodeErr := blob.Decode(rawRows[index].data, supportedWorldVersion)
		if decodeErr != nil {
			fail("corrupt_file", fmt.Sprintf("row %d: %v", rawRows[index].id, decodeErr))
		}
		if player.Descriptor == nil {
			fail("corrupt_file", fmt.Sprintf("row %d: player has no descriptor", rawRows[index].id))
		}
		rawRows[index].player = player
	}
}

// activeMods reads mods.txt if the save has one. A save with no mods has no
// mods.txt at all, which is an empty list rather than a missing one.
func activeMods(members map[string][]byte) []string {
	src, ok := members[modsMember]
	if !ok {
		return []string{}
	}
	mods, err := dict.ParseMods(src)
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	if mods == nil {
		return []string{}
	}
	return mods
}

func readArchive(src []byte) (map[string][]byte, error) {
	members := make(map[string][]byte)
	reader := tar.NewReader(bytes.NewReader(src))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return members, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("read tar member %s: %w", header.Name, err)
		}
		members[header.Name] = data
	}
}

// Positions of the localPlayers columns, in the order the schema declares them.
const (
	columnID = iota
	columnName
	columnCellX
	columnCellY
	columnX
	columnY
	columnZ
	columnWorldVersion
	columnData
	columnIsDead
)

// rowValues reads one localPlayers row, latching the first column that does not
// hold the storage class the schema promises so the row reads as one literal.
type rowValues struct {
	row     sqlite.Row
	columns []string
	err     error
}

func (v *rowValues) reject(index int) {
	if v.err == nil {
		v.err = fmt.Errorf("invalid column %s", v.columns[index])
	}
}

func (v *rowValues) integer(index int) int64 {
	if v.row.Values[index].Kind() != sqlite.Int64 {
		v.reject(index)
		return 0
	}
	return v.row.Values[index].Int64()
}

// real accepts an integer as well: SQLite's dynamic typing lets a whole
// coordinate be stored in the integer class.
func (v *rowValues) real(index int) float64 {
	switch v.row.Values[index].Kind() {
	case sqlite.Float64:
		value := v.row.Values[index].Float64()
		if err := finite("column "+v.columns[index], value); err != nil {
			if v.err == nil {
				v.err = err
			}
			return 0
		}
		return value
	case sqlite.Int64:
		return float64(v.row.Values[index].Int64())
	case sqlite.Null, sqlite.Text, sqlite.Blob:
		v.reject(index)
	}
	return 0
}

func (v *rowValues) text(index int) string {
	if v.row.Values[index].Kind() != sqlite.Text {
		v.reject(index)
		return ""
	}
	return v.row.Values[index].Text()
}

func (v *rowValues) blob(index int) []byte {
	if v.row.Values[index].Kind() != sqlite.Blob {
		v.reject(index)
		return nil
	}
	return v.row.Values[index].Blob()
}

func parseRow(row sqlite.Row) (rawCharacter, error) {
	want := []string{"id", "name", "wx", "wy", "x", "y", "z", "worldversion", "data", "isDead"}
	if !equalStrings(row.Columns, want) {
		return rawCharacter{}, fmt.Errorf("unexpected localPlayers schema: %v", row.Columns)
	}
	if len(row.Values) != len(want) {
		return rawCharacter{}, fmt.Errorf("unexpected localPlayers row value count: %d", len(row.Values))
	}
	values := &rowValues{row: row, columns: want}
	out := rawCharacter{
		id:           values.integer(columnID),
		name:         values.text(columnName),
		wx:           values.integer(columnCellX),
		wy:           values.integer(columnCellY),
		x:            values.real(columnX),
		y:            values.real(columnY),
		z:            values.real(columnZ),
		worldVersion: values.integer(columnWorldVersion),
		data:         values.blob(columnData),
		isDead:       values.integer(columnIsDead),
	}
	return out, values.err
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// finite rejects a float the result cannot carry. NaN and the infinities are
// unrepresentable in JSON, so one of them reaching the encoder would fail the
// run after the status lines had already promised a result; the field is named
// so the failure points at the column or blob field that holds it.
func finite(field string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("non-finite %s", field)
	}
	return nil
}

func makeCharacter(
	primary rawCharacter,
	remaining []rawCharacter,
	items map[uint16]string,
	mods []string,
) (character, error) {
	player := primary.player
	rowError := func(err error) error { return fmt.Errorf("row %d: %w", primary.id, err) }
	for _, field := range []struct {
		name  string
		value float64
	}{
		{hoursSurvivedField, player.HoursSurvived},
		{"nutrition.calories", float64(player.Nutrition.Calories)},
		{"nutrition.proteins", float64(player.Nutrition.Proteins)},
		{"nutrition.lipids", float64(player.Nutrition.Lipids)},
		{"nutrition.carbohydrates", float64(player.Nutrition.Carbohydrates)},
		{"nutrition.weight", float64(player.Nutrition.Weight)},
	} {
		if err := finite(field.name, field.value); err != nil {
			return character{}, rowError(err)
		}
	}
	flat, carriedItems, err := resolveInventory(player, items)
	if err != nil {
		return character{}, rowError(err)
	}
	wornItems, err := resolveWornItems(player, flat, items)
	if err != nil {
		return character{}, rowError(err)
	}
	healthData, err := summarizeHealth(player)
	if err != nil {
		return character{}, rowError(err)
	}
	perks, err := summarizePerks(player)
	if err != nil {
		return character{}, rowError(err)
	}
	traits := append([]string{}, player.XP.Traits...)
	sort.Strings(traits)
	boosts := make([]boost, 0, len(player.Descriptor.XPBoosts))
	for _, entry := range player.Descriptor.XPBoosts {
		boosts = append(boosts, boost{
			Perk:  entry.Perk,
			Level: entry.Level,
		})
	}
	sort.Slice(boosts, func(i, j int) bool { return boosts[i].Perk < boosts[j].Perk })
	others, err := summarizeOthers(remaining)
	if err != nil {
		return character{}, err
	}
	return character{
		LocalPlayerID: primary.id,
		Forename:      player.Descriptor.Forename,
		Surname:       player.Descriptor.Surname,
		Profession:    player.Descriptor.Profession,
		Female:        player.Descriptor.Female,
		Alive:         primary.isDead == 0,
		Health:        healthData,
		Position: position{
			X:     primary.x,
			Y:     primary.y,
			Z:     primary.z,
			CellX: primary.wx,
			CellY: primary.wy,
		},
		HoursSurvived: player.HoursSurvived,
		ZombieKills:   player.ZombieKills,
		SurvivorKills: player.SurvivorKills,
		Traits:        traits,
		Perks:         perks,
		XPBoosts:      boosts,
		Nutrition: nutrition{
			Calories:      player.Nutrition.Calories,
			Carbohydrates: player.Nutrition.Carbohydrates,
			Lipids:        player.Nutrition.Lipids,
			Proteins:      player.Nutrition.Proteins,
			Weight:        player.Nutrition.Weight,
		},
		WornItems:       wornItems,
		CarriedItems:    carriedItems,
		ActiveMods:      mods,
		OtherCharacters: others,
	}, nil
}

// resolveInventory flattens the item groups into one registry id per carried
// item — the positions worn items index into — and counts them by fulltype.
func resolveInventory(player *blob.Player, items map[uint16]string) ([]uint16, []carried, error) {
	flat := make([]uint16, 0)
	for _, group := range player.Inventory {
		for range group.Count {
			flat = append(flat, group.RegistryID)
		}
	}
	counts := make(map[string]int32)
	for _, registryID := range flat {
		fulltype, ok := items[registryID]
		if !ok {
			return nil, nil, fmt.Errorf("registry id %d not in %s", registryID, dictionaryMember)
		}
		counts[fulltype]++
	}
	carriedItems := make([]carried, 0, len(counts))
	for fulltype, count := range counts {
		carriedItems = append(carriedItems, carried{
			Fulltype: fulltype,
			Count:    count,
		})
	}
	sort.Slice(carriedItems, func(i, j int) bool { return carriedItems[i].Fulltype < carriedItems[j].Fulltype })
	return flat, carriedItems, nil
}

// resolveWornItems resolves each worn entry through the flattened inventory it
// indexes into, sorted by body location so equipment reads the same every run.
func resolveWornItems(player *blob.Player, flat []uint16, items map[uint16]string) ([]worn, error) {
	wornItems := make([]worn, 0, len(player.WornItems))
	for _, item := range player.WornItems {
		if item.ItemIndex < 0 || int(item.ItemIndex) >= len(flat) {
			return nil, fmt.Errorf("worn item index %d out of range", item.ItemIndex)
		}
		wornItems = append(wornItems, worn{
			BodyLocation: item.BodyLocation,
			Fulltype:     items[flat[item.ItemIndex]],
		})
	}
	sort.Slice(wornItems, func(i, j int) bool {
		if wornItems[i].BodyLocation == wornItems[j].BodyLocation {
			return wornItems[i].Fulltype < wornItems[j].Fulltype
		}
		return wornItems[i].BodyLocation < wornItems[j].BodyLocation
	})
	return wornItems, nil
}

// summarizeHealth reduces the seventeen body parts to the worst part's health,
// how many are hurt, and whether any is infected.
func summarizeHealth(player *blob.Player) (health, error) {
	if len(player.BodyDamage.Parts) == 0 {
		return health{}, errors.New("character has no body parts")
	}
	summary := health{MinBodyPartHealth: math.Inf(1)}
	for index, part := range player.BodyDamage.Parts {
		if err := finite(fmt.Sprintf("health of body part %d", index), float64(part.Health)); err != nil {
			return health{}, err
		}
		summary.MinBodyPartHealth = min(summary.MinBodyPartHealth, float64(part.Health))
		if part.Health < fullBodyPartHealth || part.IsBitten || part.IsScratched || part.IsCut ||
			part.IsDeepWounded || part.IsBleeding || part.BurnTime > 0 {
			summary.InjuredBodyParts++
		}
		summary.Infected = summary.Infected || part.IsInfected
	}
	return summary, nil
}

// summarizePerks joins the two perk tables the blob keeps — levels and raw XP —
// into one sorted list, since a perk can appear in either.
func summarizePerks(player *blob.Player) ([]perk, error) {
	levels := make(map[string]int32)
	experience := make(map[string]float32)
	for _, entry := range player.XP.PerkLevels {
		levels[entry.Perk] = entry.Level
	}
	for _, entry := range player.XP.Entries {
		if err := finite("xp for perk "+entry.Perk, float64(entry.XP)); err != nil {
			return nil, err
		}
		experience[entry.Perk] = entry.XP
	}
	names := make(map[string]struct{})
	for name := range levels {
		names[name] = struct{}{}
	}
	for name := range experience {
		names[name] = struct{}{}
	}
	perks := make([]perk, 0, len(names))
	for name := range names {
		perks = append(perks, perk{
			Name:  name,
			Level: levels[name],
			XP:    experience[name],
		})
	}
	sort.Slice(perks, func(i, j int) bool { return perks[i].Name < perks[j].Name })
	return perks, nil
}

// summarizeOthers lists the save's non-primary characters by localPlayers id.
func summarizeOthers(remaining []rawCharacter) ([]other, error) {
	others := make([]other, 0, len(remaining))
	for _, row := range remaining {
		if err := finite(hoursSurvivedField, row.player.HoursSurvived); err != nil {
			return nil, fmt.Errorf("row %d: %w", row.id, err)
		}
		others = append(others, other{
			LocalPlayerID: row.id,
			Name:          row.name,
			Alive:         row.isDead == 0,
			HoursSurvived: row.player.HoursSurvived,
		})
	}
	sort.Slice(others, func(i, j int) bool { return others[i].LocalPlayerID < others[j].LocalPlayerID })
	return others, nil
}
