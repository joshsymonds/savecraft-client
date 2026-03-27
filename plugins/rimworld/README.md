# Savecraft RimWorld Plugin

## Build

Requires `dotnet` SDK capable of building `net472`.

```bash
just build
```

### RimWorld DLL Resolution

The project resolves RimWorld managed DLLs in order:

1. `RIMWORLD_MANAGED_DIR` environment variable (direct path to Managed/)
2. `RIMWORLD_INSTALL_DIR` environment variable (appends RimWorldLinux_Data/Managed/)
3. Default Linux Steam install: `~/.local/share/Steam/steamapps/common/RimWorld/RimWorldLinux_Data/Managed`
4. `.reference/RimWorldDLLs/` fallback

If none are found, falls back to Krafs.Rimworld.Ref stubs (suitable for CI).

```bash
# Force stub build even with local game install
dotnet build SavecraftRimWorld/SavecraftRimWorld.csproj -c Release -p:UseGameDlls=false
```

## Output

Release builds write to `Assemblies/`:
- `SavecraftRimWorld.dll` (symbols embedded, paths remapped)
- `Google.Protobuf.dll`

## Workshop Preview Image

`About/Preview.png` is generated from two source screenshots composited with a scanline dissolve effect and Savecraft branding. To regenerate:

1. Place source images in the repo root:
   - `colony_screenshot.jpg` — in-game colony screenshot
   - `fangbourne.png` — Claude conversation screenshot showing colony data
2. Run the generation script:
   ```bash
   nix-shell -p imagemagick -p google-fonts --run 'bash plugins/rimworld/scripts/generate-preview.sh'
   ```

The script creates a 640x360 PNG with:
- Colony on the left, Claude conversation on the right
- Horizontal 4px scanline dissolve (gaussian distribution, centered at 50%)
- Savecraft icon (feathered edges) + gold "SAVECRAFT" title + tagline
- Dark glow halo behind branding for legibility
- Brand fonts: Press Start 2P (title), Chakra Petch (tagline)
- Brand colors: gold #c8a84e (title), blue-gray #a0a8cc (tagline)
