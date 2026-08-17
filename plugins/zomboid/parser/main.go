package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
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

func write(value output) { _ = json.NewEncoder(os.Stdout).Encode(value) }
func fail(errorType, message string) {
	write(output{
		Type:      "error",
		ErrorType: errorType,
		Message:   message,
	})
	os.Exit(1)
}

func main() {
	archive, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	write(output{
		Type:    "status",
		Message: "Reading players.db…",
	})
	members, err := readArchive(archive)
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	for _, name := range []string{"players.db", "WorldDictionaryReadable.lua"} {
		if _, ok := members[name]; !ok {
			fail("corrupt_file", "missing member "+name)
		}
	}
	if journal := members["players.db-journal"]; len(journal) > 0 {
		fail("corrupt_file", "players.db-journal is non-empty: a save write is in progress; retry on the next rewrite")
	}
	database, err := sqlite.Open(members["players.db"])
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	rows, err := database.Rows("localPlayers")
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
		if row.worldVersion != 249 {
			fail("unsupported_version", fmt.Sprintf("row %d has world version %d; this build supports 249 (Project Zomboid 42.20.x)", row.id, row.worldVersion))
		}
	}
	write(output{
		Type:    "status",
		Message: fmt.Sprintf("Decoding %d character(s)…", len(rawRows)),
	})
	for index := range rawRows {
		player, decodeErr := blob.Decode(rawRows[index].data, 249)
		if decodeErr != nil {
			fail("corrupt_file", fmt.Sprintf("row %d: %v", rawRows[index].id, decodeErr))
		}
		rawRows[index].player = player
	}
	items, err := dict.ParseItems(members["WorldDictionaryReadable.lua"])
	if err != nil {
		fail("corrupt_file", err.Error())
	}
	mods := []string{}
	if src, ok := members["mods.txt"]; ok {
		mods, err = dict.ParseMods(src)
		if err != nil {
			fail("corrupt_file", err.Error())
		}
	}
	if mods == nil {
		mods = []string{}
	}
	sort.Slice(rawRows, func(i, j int) bool { return rawRows[i].id < rawRows[j].id })
	primary := rawRows[0]
	data, dataErr := makeCharacter(primary, rawRows[1:], items, mods)
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
		alive = "alive"
	}
	write(output{
		Type: "result",
		Identity: &identity{
			SaveName:    saveName,
			GameID:      "zomboid",
			DisplayName: name,
		},
		Summary: fmt.Sprintf("%s (%s) — %.1f h survived, %d zombie kills, %s", name, profession, primary.player.HoursSurvived, primary.player.ZombieKills, alive),
		Sections: &outputSections{Character: section{
			Description: characterDescription,
			Data:        data,
		}},
	})
}

func readArchive(src []byte) (map[string][]byte, error) {
	members := make(map[string][]byte)
	reader := tar.NewReader(bytes.NewReader(src))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return members, nil
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		members[header.Name] = data
	}
}

func parseRow(row sqlite.Row) (rawCharacter, error) {
	want := []string{"id", "name", "wx", "wy", "x", "y", "z", "worldversion", "data", "isDead"}
	if !equalStrings(row.Columns, want) {
		return rawCharacter{}, fmt.Errorf("unexpected localPlayers schema: %v", row.Columns)
	}
	if len(row.Values) != len(want) {
		return rawCharacter{}, fmt.Errorf("unexpected localPlayers row value count: %d", len(row.Values))
	}
	integer := func(index int) (int64, error) {
		if row.Values[index].Kind() != sqlite.Int64 {
			return 0, fmt.Errorf("invalid column %s", want[index])
		}
		return row.Values[index].Int64(), nil
	}
	real := func(index int) (float64, error) {
		switch row.Values[index].Kind() {
		case sqlite.Float64:
			return row.Values[index].Float64(), nil
		case sqlite.Int64:
			return float64(row.Values[index].Int64()), nil
		default:
			return 0, fmt.Errorf("invalid column %s", want[index])
		}
	}
	if row.Values[1].Kind() != sqlite.Text {
		return rawCharacter{}, fmt.Errorf("invalid column name")
	}
	if row.Values[8].Kind() != sqlite.Blob {
		return rawCharacter{}, fmt.Errorf("invalid column data")
	}
	var out rawCharacter
	var err error
	if out.id, err = integer(0); err != nil {
		return out, err
	}
	if out.wx, err = integer(2); err != nil {
		return out, err
	}
	if out.wy, err = integer(3); err != nil {
		return out, err
	}
	if out.x, err = real(4); err != nil {
		return out, err
	}
	if out.y, err = real(5); err != nil {
		return out, err
	}
	if out.z, err = real(6); err != nil {
		return out, err
	}
	if out.worldVersion, err = integer(7); err != nil {
		return out, err
	}
	if out.isDead, err = integer(9); err != nil {
		return out, err
	}
	out.name, out.data = row.Values[1].Text(), row.Values[8].Blob()
	return out, nil
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

func makeCharacter(primary rawCharacter, remaining []rawCharacter, items map[uint16]string, mods []string) (character, error) {
	player := primary.player
	flat := make([]uint16, 0)
	for _, group := range player.Inventory {
		for count := int32(0); count < group.Count; count++ {
			flat = append(flat, group.RegistryID)
		}
	}
	counts := make(map[string]int32)
	for _, registryID := range flat {
		fulltype, ok := items[registryID]
		if !ok {
			return character{}, fmt.Errorf("registry id %d not in WorldDictionaryReadable.lua", registryID)
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
	wornItems := make([]worn, 0, len(player.WornItems))
	for _, item := range player.WornItems {
		if item.ItemIndex < 0 || int(item.ItemIndex) >= len(flat) {
			return character{}, fmt.Errorf("row %d: worn item index out of range", primary.id)
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
	if len(player.BodyDamage.Parts) == 0 {
		return character{}, fmt.Errorf("row %d has no body parts", primary.id)
	}
	healthData := health{MinBodyPartHealth: math.Inf(1)}
	for _, part := range player.BodyDamage.Parts {
		healthData.MinBodyPartHealth = min(healthData.MinBodyPartHealth, float64(part.Health))
		if part.Health < 100 || part.IsBitten || part.IsScratched || part.IsCut || part.IsDeepWounded || part.IsBleeding || part.BurnTime > 0 {
			healthData.InjuredBodyParts++
		}
		healthData.Infected = healthData.Infected || part.IsInfected
	}
	levels := make(map[string]int32)
	experience := make(map[string]float32)
	for _, entry := range player.XP.PerkLevels {
		levels[entry.Perk] = entry.Level
	}
	for _, entry := range player.XP.Entries {
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
	others := make([]other, 0, len(remaining))
	for _, row := range remaining {
		others = append(others, other{
			LocalPlayerID: row.id,
			Name:          row.name,
			Alive:         row.isDead == 0,
			HoursSurvived: row.player.HoursSurvived,
		})
	}
	sort.Slice(others, func(i, j int) bool { return others[i].LocalPlayerID < others[j].LocalPlayerID })
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
