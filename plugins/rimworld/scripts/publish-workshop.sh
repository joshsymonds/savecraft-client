#!/usr/bin/env bash
# Stage the RimWorld Savecraft mod for Steam Workshop upload, then print the
# steamcmd command to actually upload.
#
# Usage:
#   bash plugins/rimworld/scripts/publish-workshop.sh "Change note"
#
# Steam Workshop item: 3693580596 (RimWorld appid 294100, author Veraticus)
# Published ID is tracked in About/PublishedFileId.txt — single source of truth.

set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APPID=294100
PUBLISHEDFILEID="$(cat "$PLUGIN_DIR/About/PublishedFileId.txt" | tr -d '[:space:]')"
WORKSHOP_DIR="${WORKSHOP_DIR:-$HOME/.local/share/Steam/rimworld-workshop-upload}"
CHANGENOTE="${1:-Update}"

if [[ -z "$PUBLISHEDFILEID" ]]; then
  echo "ERROR: empty publishedfileid in About/PublishedFileId.txt" >&2
  exit 1
fi

echo "==> Building mod (Release)..."
( cd "$PLUGIN_DIR" && \
  NIXPKGS_ALLOW_UNFREE=1 nix-shell -p dotnetCorePackages.sdk_9_0 --run \
    "dotnet build SavecraftRimWorld/SavecraftRimWorld.csproj -c Release" )

echo "==> Staging content to $WORKSHOP_DIR/content/..."
rm -rf "$WORKSHOP_DIR/content"
mkdir -p "$WORKSHOP_DIR/content"
cp -r "$PLUGIN_DIR/About"      "$WORKSHOP_DIR/content/"
cp -r "$PLUGIN_DIR/Assemblies" "$WORKSHOP_DIR/content/"
cp -r "$PLUGIN_DIR/Textures"   "$WORKSHOP_DIR/content/"
cp    "$PLUGIN_DIR/icon.png"   "$WORKSHOP_DIR/content/"

# Workshop preview image lives separately from content/
cp "$PLUGIN_DIR/About/Preview.png" "$WORKSHOP_DIR/preview.png"

# Write upload.vdf — escape changenote quotes for VDF format
ESCAPED_NOTE="${CHANGENOTE//\"/\\\"}"
cat > "$WORKSHOP_DIR/upload.vdf" <<EOF
"workshopitem"
{
  "appid" "$APPID"
  "publishedfileid" "$PUBLISHEDFILEID"
  "contentfolder" "$WORKSHOP_DIR/content"
  "previewfile" "$WORKSHOP_DIR/preview.png"
  "changenote" "$ESCAPED_NOTE"
}
EOF

echo
echo "Staged:"
echo "  appid:           $APPID"
echo "  publishedfileid: $PUBLISHEDFILEID"
echo "  contentfolder:   $WORKSHOP_DIR/content"
echo "  preview:         $WORKSHOP_DIR/preview.png"
echo "  changenote:      $CHANGENOTE"
echo
echo "Upload with:"
echo "  NIXPKGS_ALLOW_UNFREE=1 nix-shell -p steamcmd --run \\"
echo "    \"steamcmd +login Veraticus +workshop_build_item $WORKSHOP_DIR/upload.vdf +quit\""
