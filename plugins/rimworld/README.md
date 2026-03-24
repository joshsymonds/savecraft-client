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
