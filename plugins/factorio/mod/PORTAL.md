# Savecraft

## Your AI is making things up about your factory. Savecraft fixes that.

Savecraft connects your factory to Claude and ChatGPT. Production rates, bottlenecks, power, logistics, research, pollution, biter pressure — real data from your actual save, updated live. No more alt-tabbing to wikis and calculators. No more pasting stats into ChatGPT and getting outdated patch advice back.

## What it does

Every few seconds, Savecraft reads your factory state and sends it to your AI:

- Every assembler, furnace, and chemical plant: recipe, modules, beacons, effective speed, current throughput
- Resource flow: items per second through every belt, pipe, and train network
- Power: production, consumption, accumulator buffer, brownout risk
- Logistics: train schedules, station throughput, bot network coverage
- Research: current project, time remaining, science pack consumption rate
- Pollution and biters: pollution cloud reach, evolution factor, attack frequency, base proximity
- Inventory: what you have, what you're consuming, what you're stockpiling
- Map state: explored area, resource patches and depletion rates

Your AI also gets **reference modules** that know Factorio's actual formulas:

- Recipe lookup with exact ingredients, craft times, and reverse lookups for 400+ recipes
- Ratio calculator for assembler counts, belt speeds, and raw material rates at any production target
- Module optimizer with productivity, speed, efficiency, and beacon stacking math
- Oil balancer for refinery and cracking plant counts at target fluid rates
- Power calculator for steam, solar, and nuclear sizing with fuel consumption
- Evolution tracker using Factorio's actual asymptotic formula against your pollution data
- Tech tree navigator with prerequisite chains and total science pack costs
- Blueprint analyzer that decodes blueprint strings and evaluates ratios, belts, and inserter adequacy
- Train throughput and station capacity calculator
- Quality calculator for tier probabilities and recycler loop efficiency (Space Age)

## What you can ask

- "What's actually throttling my green circuit production?"
- "If I want to double my purple science output, what do I need to scale up?"
- "Walk me through my power situation — am I about to brown out?"
- "Where should I put my next iron outpost?"
- "I have spare capacity somewhere. Where?"
- "Why is my bot network behind on construction?"

Answers come from your actual save plus deterministic reference data — not the model's training memory, which is where most "ChatGPT for Factorio" advice falls apart. The reference data is versioned to the game, so it knows current recipes, current crafting times, current module effects, and current Space Age content.

## Setup

1. Install and enable this mod
2. Install the Savecraft daemon from [savecraft.gg](https://savecraft.gg) — a link code appears during setup
3. Enter the code at savecraft.gg to connect your machine
4. Start a game — the mod exports data automatically, the daemon picks it up
5. Add the Savecraft integration in Claude or ChatGPT
6. Ask about your factory

### Compatibility

Factorio 2.0 and Space Age. Safe to add or remove mid-save. Works alongside other mods, including overhauls, though modded recipes are not yet parsed by the reference modules (your AI will see the raw data but won't have ratio math for them).

### Privacy

Sends structured game state to Savecraft's server for your AI to read. No save files, no filesystem paths, no personal data. Open source.

[Website](https://savecraft.gg) | [Source](https://github.com/joshsymonds/savecraft.gg) | [Discord](https://discord.gg/YnC8stpEmF)
