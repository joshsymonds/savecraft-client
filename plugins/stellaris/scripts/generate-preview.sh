#!/bin/bash
# Generates the Steam Workshop thumbnail.png for the Stellaris mod.
#
# Requires:
#   - imagemagick (via nix-shell or system install)
#   - google-fonts (via nix-shell)
#   - Two source images in the repo root:
#     - stellaris_screenshot.jpg — in-game Stellaris screenshot
#     - stellaris1.jpg — Claude conversation screenshot showing parsed empire data
#
# Usage:
#   nix-shell -p imagemagick -p google-fonts --run 'bash plugins/stellaris/scripts/generate-preview.sh'
#
# The output is written to plugins/stellaris/mod/thumbnail.png (640x360, Workshop format).
#
# Design:
#   - Left half: Stellaris game screenshot
#   - Right half: Claude conversation showing parsed empire data
#   - Middle: horizontal scanline dissolve (4px bars, gaussian distribution centered at 50%)
#   - Center: Savecraft icon (feathered edges) + "SAVECRAFT" in Press Start 2P + tagline in Chakra Petch
#   - Dark glow halo behind branding for legibility

set -e

BASE="$(cd "$(dirname "$0")/../../.." && pwd)"
PLUGIN="$BASE/plugins/stellaris"
GAME="$BASE/stellaris_screenshot.jpg"
CLAUDE="$BASE/stellaris1.jpg"
ICON="$BASE/web/static/icon-192.png"
OUTPUT="$PLUGIN/mod/thumbnail.png"
WORK="$(mktemp -d)"

FONT_DIR=$(find /nix/store -maxdepth 1 -name "*google-fonts*" -not -name "*adobeBlank*" -not -name "*.drv" 2>/dev/null | head -1)/share/fonts/truetype
PRESS="$FONT_DIR/PressStart2P-Regular.ttf"
CHAKRA="$FONT_DIR/ChakraPetch-SemiBold.ttf"

if [ ! -f "$GAME" ]; then echo "Missing $GAME"; exit 1; fi
if [ ! -f "$CLAUDE" ]; then echo "Missing $CLAUDE"; exit 1; fi
if [ ! -f "$PRESS" ]; then echo "Missing Press Start 2P font — run via: nix-shell -p imagemagick -p google-fonts"; exit 1; fi

W=1280; H=720
ICON_SIZE=120; ICON_Y=200; TITLE_Y=340; TAG_Y=410
PAD=100
CANVAS=$((ICON_SIZE + PAD * 2))
GLOW_Y=$((ICON_Y - PAD))

echo "=== Generating thumbnail.png ==="

# 1. Prepare source images at working resolution
echo "  Preparing source images..."
magick "$GAME" -resize ${W}x${H}^ -gravity center -extent ${W}x${H} "$WORK/game.png"
magick "$CLAUDE" -resize 1280x -gravity center -extent ${W}x${H} "$WORK/claude.png"

# 2. Generate dissolve masks (paired, gaussian distribution, 4px bars)
echo "  Generating dissolve masks..."
python3 - "$W" "$H" "$WORK" << 'PYEOF'
import random, struct, zlib, sys

def make_png(width, height, rows_data):
    def chunk(chunk_type, data):
        c = chunk_type + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)
    header = struct.pack('>IIBBBBB', width, height, 8, 0, 0, 0, 0)
    raw = b''
    for row in rows_data:
        raw += b'\x00' + bytes(row)
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', header) + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b'')

w, h = int(sys.argv[1]), int(sys.argv[2])
work = sys.argv[3]
row_h = 4
random.seed(42)
num_rows = h // row_h

game_rows, claude_rows = [], []
for i in range(num_rows):
    val = random.gauss(0.5, 0.1)
    val = max(0.2, min(0.8, val))
    cutoff = int(w * val)
    game_px = [255 if x <= cutoff else 0 for x in range(w)]
    claude_px = [255 if x >= cutoff else 0 for x in range(w)]
    for _ in range(row_h):
        game_rows.append(game_px)
        claude_rows.append(claude_px)

with open(f'{work}/game_mask.png', 'wb') as f:
    f.write(make_png(w, h, game_rows))
with open(f'{work}/claude_mask.png', 'wb') as f:
    f.write(make_png(w, h, claude_rows))
PYEOF

# 3. Apply masks and composite
echo "  Compositing..."
magick "$WORK/game.png" "$WORK/game_mask.png" \
  -alpha off -compose CopyOpacity -composite "$WORK/game_frayed.png"
magick "$WORK/claude.png" "$WORK/claude_mask.png" \
  -alpha off -compose CopyOpacity -composite "$WORK/claude_frayed.png"
magick "$WORK/game_frayed.png" -alpha set -channel A -evaluate multiply 0.80 +channel "$WORK/game_faded.png"
magick "$WORK/claude_frayed.png" -alpha set -channel A -evaluate multiply 0.80 +channel "$WORK/claude_faded.png"
magick -size ${W}x${H} xc:black \
  "$WORK/game_faded.png" -composite \
  "$WORK/claude_faded.png" -composite \
  "$WORK/composited.png"

# 4. Branding: feathered icon + text with dark glow halo
echo "  Adding branding..."

# Feathered icon on padded canvas
magick -size ${ICON_SIZE}x${ICON_SIZE} radial-gradient:'white-black' \
  -level 0%,55% "$WORK/feather_mask.png"
magick "$ICON" -resize ${ICON_SIZE}x${ICON_SIZE} \
  "$WORK/feather_mask.png" -compose CopyOpacity -composite "$WORK/icon_feathered.png"
magick -size ${CANVAS}x${CANVAS} xc:none \
  "$WORK/icon_feathered.png" -gravity center -composite "$WORK/icon_padded.png"

# Glow layers (text + icon)
magick -size ${W}x${H} xc:none \
  -font "$PRESS" -pointsize 52 -fill black -gravity north -annotate +0+${TITLE_Y} 'SAVECRAFT' \
  -font "$CHAKRA" -pointsize 34 -fill black -gravity north -annotate +0+${TAG_Y} 'Real empire data for your AI' \
  -blur 0x20 "$WORK/glow_wide.png"
magick -size ${W}x${H} xc:none \
  -font "$PRESS" -pointsize 52 -fill black -gravity north -annotate +0+${TITLE_Y} 'SAVECRAFT' \
  -font "$CHAKRA" -pointsize 34 -fill black -gravity north -annotate +0+${TAG_Y} 'Real empire data for your AI' \
  -blur 0x10 "$WORK/glow_tight.png"
magick "$WORK/icon_padded.png" -channel RGB -evaluate set 0 +channel -blur 0x20 "$WORK/ig_wide.png"
magick "$WORK/icon_padded.png" -channel RGB -evaluate set 0 +channel -blur 0x10 "$WORK/ig_tight.png"

# Stack glow passes for strong dark halo (extra passes for bright Stellaris background)
magick "$WORK/composited.png" \
  "$WORK/glow_wide.png" -composite \
  "$WORK/ig_wide.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_wide.png" -composite \
  "$WORK/ig_wide.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_wide.png" -composite \
  "$WORK/ig_wide.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_wide.png" -composite \
  "$WORK/ig_wide.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_wide.png" -composite \
  "$WORK/ig_wide.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_wide.png" -composite \
  "$WORK/ig_wide.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_tight.png" -composite \
  "$WORK/ig_tight.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_tight.png" -composite \
  "$WORK/ig_tight.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_tight.png" -composite \
  "$WORK/ig_tight.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_tight.png" -composite \
  "$WORK/ig_tight.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/glow_tight.png" -composite \
  "$WORK/ig_tight.png" -gravity north -geometry +0+${GLOW_Y} -composite \
  "$WORK/with_glow.png"

# Final: icon + text + scale to 640x360
magick "$WORK/with_glow.png" "$WORK/icon_feathered.png" \
  -gravity north -geometry +0+${ICON_Y} -composite "$WORK/with_icon.png"
magick "$WORK/with_icon.png" \
  -font "$PRESS" -pointsize 52 -fill '#c8a84e' -gravity north -annotate +0+${TITLE_Y} 'SAVECRAFT' \
  -font "$CHAKRA" -pointsize 34 -fill '#a0a8cc' -gravity north -annotate +0+${TAG_Y} 'Real empire data for your AI' \
  "$WORK/final.png"

magick "$WORK/final.png" -resize 640x360 -quality 95 "$OUTPUT"

rm -rf "$WORK"
echo "=== Done: $OUTPUT ==="
