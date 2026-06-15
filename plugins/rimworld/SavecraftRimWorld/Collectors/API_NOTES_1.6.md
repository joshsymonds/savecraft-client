# RimWorld 1.6.4850 API reference — confirmed member signatures

Extracted via reflection (`MetadataLoadContext`) over the real `Assembly-CSharp.dll`
from RimWorld 1.6.4850 rev653 (all DLCs). These are the **confirmed** members the
issue-#5 field collectors depend on. Do not guess names — use these.

`Pawn` tracker fields (all present): `genes` (Pawn_GeneTracker), `royalty`
(Pawn_RoyaltyTracker), `ideo` (Pawn_IdeoTracker), `abilities` (Pawn_AbilityTracker),
`psychicEntropy` (Pawn_PsychicEntropyTracker), `relations` (Pawn_RelationsTracker),
`story`, `equipment`, `apparel`, `health`.

## R1 — Biotech genes (`pawn.genes`, RimWorld.Pawn_GeneTracker)
- `Xenotype` → `XenotypeDef` (`.label` for def name)
- `XenotypeLabelCap` → `string` — display label, already handles custom xenotypes. **Use for `xenotype`.**
- `CustomXenotype` → `CustomXenotype` (null for non-custom). **`xenotype_custom` = `CustomXenotype != null`.**
- `Endogenes` → `List<Gene>`; `Xenogenes` → `List<Gene>`; `GenesListForReading` → `List<Gene>`
- `Gene` (Verse.Gene): field `def` → `GeneDef`; props `Active` (bool), `Overridden` (bool)
- `GeneDef` (Verse.GeneDef, base Def): int fields `biostatMet`, `biostatCpx`, `biostatArc`; prop `Archite` (bool); `label` (from Def)
- `gene_metabolism` = Σ `g.def.biostatMet` over active genes; `gene_complexity` = Σ `biostatCpx`; `archite_count` = count where `biostatArc > 0` (or `g.def.Archite`)

## R2 — Royalty (`pawn.royalty`, RimWorld.Pawn_RoyaltyTracker)
- `MostSeniorTitle` → `RoyalTitle` (null if none). `RoyalTitle`: field `def` (RoyalTitleDef), field `faction` (Faction), prop `Label` (string).
  - `royalty_title` = `MostSeniorTitle?.def.GetLabelCapFor(pawn)` or `MostSeniorTitle?.Label`
  - `royalty_faction` = `MostSeniorTitle?.faction.Name`
- `GetFavor(Faction)` → int. `royalty_honor` = `GetFavor(MostSeniorTitle.faction)`. (favor stored in `Dictionary<Faction,int> favor`.)
- `AllFactionPermits` → `List<FactionPermit>`. `FactionPermit`: props `Permit` (RoyalTitlePermitDef), `Faction`, `OnCooldown` (bool), `LastUsedTick` (int).
  - `royal_permits[]` = each `{Permit.label, OnCooldown}`
- Psycasts: `pawn.abilities.AllAbilitiesForReading` → `List<Ability>`; `Ability.def` → `AbilityDef`; **`AbilityDef.IsPsycast` (bool) is the clean filter.**
  - `psycasts[]` = abilities where `a.def.IsPsycast`, project `a.def.label`
- Psychic (`pawn.psychicEntropy`, RimWorld.Pawn_PsychicEntropyTracker):
  - `psychic_entropy_max` = `MaxEntropy` (float)
  - `psychic_sensitivity` = `PsychicSensitivity` (float)
  - `psylink_level`: `Psylink` → `Hediff_Psylink` (base `Hediff_Level`, which has int `level`). Use `(pawn.psychicEntropy.Psylink as Hediff_Level)?.level ?? 0`. (`MaxAbilityLevel` also tracks this.)

## R3 — Ideology (`pawn.ideo`, RimWorld.Pawn_IdeoTracker)
- `Ideo` → `Ideo` (RimWorld.Ideo). `Ideo`: field `name` (string); method `GetRole(Pawn p)` → `Precept_Role` (null if none).
  - `ideo_name` = `Ideo.name`
  - `ideo_role` = `Ideo.GetRole(pawn)?.LabelCap` (Precept_Role; label via base Precept)
- `Certainty` → float (0–1). `ideo_certainty` = `Certainty`.
- NOTE: issue suggested `pawn.story.socialRole` — that is NOT how 1.6 exposes the role; use `Ideo.GetRole(pawn)`.

## R4 — Capacities (`pawn.health.capacities.GetLevel(PawnCapacityDefOf.X)`)
`PawnCapacityDefOf` static fields confirmed present (9): `Consciousness`, `Sight`,
`Hearing`, `Moving`, `Manipulation`, `Talking`, `Breathing`, `BloodFiltration`,
`BloodPumping`.
- **`Eating` is NOT in `PawnCapacityDefOf`.** Resolve via
  `DefDatabase<PawnCapacityDef>.GetNamedSilentFail("Eating")` (the def exists; it's just
  not a DefOf member). Skip if null.

## R7 — Relationships (`pawn.relations`, RimWorld.Pawn_RelationsTracker)
- `DirectRelations` → `List<DirectPawnRelation>`. `DirectPawnRelation`: field `def`
  (PawnRelationDef, `.label` for relation name), field `otherPawn` (Pawn).
- `OpinionOf(Pawn other)` → int.
- `RelatedPawns` → `IEnumerable<Pawn>`.

## R5 — Anomaly — **DEVIATION: not per-pawn in 1.6**
The issue assumed per-pawn `void_curiosity` / `void_connections` / `study_unlocks`.
These DO NOT exist on `Pawn` in 1.6 (reflection over Pawn shows zero void/anomaly/
curiosity members). Anomaly state is COLONY/GAME-level:
- `GameComponent_Anomaly` (via `Current.Game.GetComponent<GameComponent_Anomaly>()`):
  `Level`, `HighestLevelReached`, `LevelDef` (MonolithLevelDef), `MonolithStudyProgress`,
  `MonolithStudyCompleted`, `AnomalyStudyEnabled`, `hasPerformedVoidProvocation`,
  `hypnotisedPawns` (Dictionary, game-level), `corpseTrackers`.
- Void curiosity is an INCIDENT (`IncidentWorker_VoidCuriosity`,
  `VoidCuriosityIncidentDelayRangeDays`), not a pawn or colony stat.
- Entity knowledge / study unlocks live in `EntityCodex` / `EntityCodexEntryDef` /
  `StudyManager` (game-level), not per-pawn.
- Per-pawn anomaly effects that DO exist are hediffs (inhumanized, void-touched, etc.),
  already surfaced by the existing `health[]` list (no per-pawn anomaly fields added).

**Resolution:** R5 reshaped into a colony-level `anomaly` section (new AnomalyCollector).
Confirmed accessors:
- `Find.Anomaly` → `GameComponent_Anomaly`: `Level` (int), `HighestLevelReached` (int),
  `LevelDef` (MonolithLevelDef, `.label`/`.defName`), `MonolithStudyProgress` (int),
  `MonolithStudyCompleted` (bool), `AnomalyStudyEnabled` (bool), `MonolithSpawned` (bool),
  field `hasPerformedVoidProvocation` (bool).
- `Find.EntityCodex` → `EntityCodex`: field `discoveredEntities` (HashSet) →
  `.Count` for entities discovered. (No separate "studied" count — studying an entity is
  what discovers it; `entities_studied` dropped as redundant.)
- Both are null when Anomaly DLC inactive — gate on `ModsConfig.AnomalyActive`.

## R6 — Weapon null
`Google.Protobuf.WellKnownTypes.Value.ForNull()` is available for emitting JSON null.
Add a `StructHelper.SetNull(this Struct, string key)` helper.

## R8 — Roster scalars
Reuse the same accessors as R1/R2/R3 for `xenotype` (genes.XenotypeLabelCap),
`royalty_title` (royalty.MostSeniorTitle), `ideo_role` (ideo.Ideo.GetRole(pawn)),
`weapon_name` (equipment.Primary?.LabelCap), each null when DLC inactive / absent.

## Build
- `Krafs.Rimworld.Ref` pinned to **1.6.4850** (matches the installed game build).
- Real-DLL build: 6 referenced assemblies copied to gitignored `.reference/RimWorldDLLs/`
  from Gnomon. Builds clean both ways (`UseGameDlls=true` real DLLs, and `=false` Krafs stubs).
