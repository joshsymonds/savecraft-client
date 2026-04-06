#!/bin/bash
# Generates the mod portal images for the Factorio mod:
#   - thumbnail.png (144x144) — in-game mod browser icon
#   - gallery.png (640x360) — portal gallery image
#
# Requires:
#   nix-shell -p imagemagick -p google-fonts --run 'bash plugins/factorio/scripts/generate-portal-images.sh'

set -e

BASE="$(cd "$(dirname "$0")/../../.." && pwd)"
ICON="$BASE/web/static/icon-512.png"
MOD="$BASE/plugins/factorio/mod"
WORK="$(mktemp -d)"

FONT_DIR=$(find /nix/store -maxdepth 1 -name "*google-fonts*" -not -name "*adobeBlank*" -not -name "*.drv" 2>/dev/null | head -1)/share/fonts/truetype
PRESS="$FONT_DIR/PressStart2P-Regular.ttf"
CHAKRA="$FONT_DIR/ChakraPetch-SemiBold.ttf"

if [ ! -f "$ICON" ]; then echo "Missing $ICON"; exit 1; fi
if [ ! -f "$PRESS" ]; then echo "Missing Press Start 2P font — run via: nix-shell -p imagemagick -p google-fonts"; exit 1; fi

GOLD="#c8a84e"
SILVER="#a0a8cc"

# ─── Thumbnail (144x144) ─────────────────────────────────────────────────────

echo "=== Generating thumbnail.png (144x144) ==="

THUMB=144
THUMB_ICON=72

# Background with vignette + scanlines
magick -size ${THUMB}x${THUMB} xc:'#05071a' "$WORK/thumb_base.png"
magick -size ${THUMB}x${THUMB} radial-gradient:'#0e1540-#05071a' "$WORK/thumb_vig.png"
magick "$WORK/thumb_base.png" "$WORK/thumb_vig.png" -compose Over -composite "$WORK/thumb_bg.png"

python3 - "$THUMB" "$THUMB" "$WORK" "thumb_scan.png" << 'PYEOF'
import struct, zlib, sys
def make_png(w, h, rows):
    def chunk(t, d):
        c = t + d
        return struct.pack('>I', len(d)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)
    hdr = struct.pack('>IIBBBBB', w, h, 8, 4, 0, 0, 0)
    raw = b''
    for r in rows: raw += b'\x00' + bytes(r)
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', hdr) + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b'')
w, h = int(sys.argv[1]), int(sys.argv[2])
rows = []
for y in range(h):
    row = []
    for x in range(w):
        row.extend([0, 40] if y % 2 == 0 else [0, 0])
    rows.append(row)
with open(f'{sys.argv[3]}/{sys.argv[4]}', 'wb') as f: f.write(make_png(w, h, rows))
PYEOF

magick "$WORK/thumb_bg.png" "$WORK/thumb_scan.png" -compose Over -composite "$WORK/thumb_bgf.png"

# Feathered icon
magick -size ${THUMB_ICON}x${THUMB_ICON} radial-gradient:'white-black' \
  -level 0%,60% "$WORK/thumb_feather.png"
magick "$ICON" -resize ${THUMB_ICON}x${THUMB_ICON} \
  "$WORK/thumb_feather.png" -compose CopyOpacity -composite "$WORK/thumb_icon.png"

# Gold glow behind shield
magick "$WORK/thumb_icon.png" -channel RGB -fill "$GOLD" -colorize 100 +channel \
  -blur 0x10 -evaluate multiply 0.25 "$WORK/thumb_iglow.png"

# Centered layout: title (10px) + 3px gap + icon (72px) = 85px
# (144 - 85) / 2 = ~30px top offset
THUMB_TEXT_Y=30
THUMB_ICON_Y=43

# Text glow
magick -size ${THUMB}x${THUMB} xc:none \
  -font "$PRESS" -pointsize 10 -fill black \
  -gravity north -annotate +0+${THUMB_TEXT_Y} 'SAVECRAFT' \
  -blur 0x4 "$WORK/tg.png"

# FF6 frame (scaled down)
T_OUTER=4
T_INNER=8
T_CORNER=3

magick -size ${THUMB}x${THUMB} xc:none \
  -fill none \
  -stroke "#7a8aed" -strokewidth 1 \
  -draw "rectangle ${T_OUTER},${T_OUTER} $((THUMB-T_OUTER)),$((THUMB-T_OUTER))" \
  -stroke "#4a5aad" -strokewidth 1 \
  -draw "rectangle ${T_INNER},${T_INNER} $((THUMB-T_INNER)),$((THUMB-T_INNER))" \
  -fill "#7a8aed" -stroke none \
  -draw "rectangle $((T_OUTER-1)),$((T_OUTER-1)) $((T_OUTER+T_CORNER)),$((T_OUTER+T_CORNER))" \
  -draw "rectangle $((THUMB-T_OUTER-T_CORNER)),$((T_OUTER-1)) $((THUMB-T_OUTER+1)),$((T_OUTER+T_CORNER))" \
  -draw "rectangle $((T_OUTER-1)),$((THUMB-T_OUTER-T_CORNER)) $((T_OUTER+T_CORNER)),$((THUMB-T_OUTER+1))" \
  -draw "rectangle $((THUMB-T_OUTER-T_CORNER)),$((THUMB-T_OUTER-T_CORNER)) $((THUMB-T_OUTER+1)),$((THUMB-T_OUTER+1))" \
  "$WORK/thumb_frame.png"

# Compose all
magick "$WORK/thumb_bgf.png" \
  "$WORK/thumb_iglow.png" -gravity north -geometry +0+$((THUMB_ICON_Y-5)) -composite \
  "$WORK/tg.png" -composite \
  "$WORK/tg.png" -composite \
  "$WORK/tg.png" -composite \
  "$WORK/thumb_icon.png" -gravity north -geometry +0+${THUMB_ICON_Y} -composite \
  "$WORK/thumb_frame.png" -composite \
  -font "$PRESS" -pointsize 10 -fill "$GOLD" \
  -gravity north -annotate +0+${THUMB_TEXT_Y} 'SAVECRAFT' \
  "$MOD/thumbnail.png"

echo "  -> $MOD/thumbnail.png"

# ─── Gallery image (640x360) ─────────────────────────────────────────────────

echo "=== Generating gallery.png (640x360) ==="

GW=640; GH=360
G_ICON=140
G_TITLE_SIZE=32
G_TAG_SIZE=18

# Vertically centered layout:
# Title (32px) + 12px gap + icon (140px) + 12px gap + tagline (18px) = ~214px
# (360 - 214) / 2 = 73px top offset
G_TITLE_Y=73
G_ICON_Y=117
G_TAG_Y=272

# --- Background: deep Savecraft blue with radial vignette ---

magick -size ${GW}x${GH} xc:'#05071a' "$WORK/bg_base.png"

magick -size ${GW}x${GH} radial-gradient:'#0e1540-#05071a' \
  "$WORK/bg_vignette.png"

magick "$WORK/bg_base.png" "$WORK/bg_vignette.png" \
  -compose Over -composite "$WORK/bg_grad.png"

# --- CRT scanlines (every 2nd pixel row, visible but subtle) ---

python3 - "$GW" "$GH" "$WORK" << 'PYEOF'
import struct, zlib, sys

def make_png(width, height, rows_data):
    def chunk(chunk_type, data):
        c = chunk_type + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)
    header = struct.pack('>IIBBBBB', width, height, 8, 4, 0, 0, 0)
    raw = b''
    for row in rows_data:
        raw += b'\x00' + bytes(row)
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', header) + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b'')

w, h = int(sys.argv[1]), int(sys.argv[2])
work = sys.argv[3]

rows = []
for y in range(h):
    row = []
    for x in range(w):
        if y % 2 == 0:
            row.extend([0, 40])  # Dark scanline, alpha 40/255 ~16%
        else:
            row.extend([0, 0])
    rows.append(row)

with open(f'{work}/scanlines.png', 'wb') as f:
    f.write(make_png(w, h, rows))
PYEOF

magick "$WORK/bg_grad.png" "$WORK/scanlines.png" \
  -compose Over -composite "$WORK/bg_final.png"

# --- Feathered icon ---

magick -size ${G_ICON}x${G_ICON} radial-gradient:'white-black' \
  -level 0%,55% "$WORK/g_feather.png"
magick "$ICON" -resize ${G_ICON}x${G_ICON} \
  "$WORK/g_feather.png" -compose CopyOpacity -composite "$WORK/g_icon.png"

# Gold glow behind shield
magick "$WORK/g_icon.png" -channel RGB -fill "$GOLD" -colorize 100 +channel \
  -blur 0x20 -evaluate multiply 0.25 "$WORK/g_icon_glow.png"

# --- Text glow halos ---

magick -size ${GW}x${GH} xc:none \
  -font "$PRESS" -pointsize $G_TITLE_SIZE -fill black \
  -gravity north -annotate +0+${G_TITLE_Y} 'SAVECRAFT' \
  -font "$CHAKRA" -pointsize $G_TAG_SIZE -fill black \
  -gravity north -annotate +0+${G_TAG_Y} 'Real factory data for your AI' \
  -blur 0x14 "$WORK/glow_w.png"

magick -size ${GW}x${GH} xc:none \
  -font "$PRESS" -pointsize $G_TITLE_SIZE -fill black \
  -gravity north -annotate +0+${G_TITLE_Y} 'SAVECRAFT' \
  -font "$CHAKRA" -pointsize $G_TAG_SIZE -fill black \
  -gravity north -annotate +0+${G_TAG_Y} 'Real factory data for your AI' \
  -blur 0x6 "$WORK/glow_t.png"

# --- FF6-style double-border window frame ---
# Outer border + inner border with 4px gap, corner squares where lines meet

OUTER=12
INNER=20
BCOLOR_OUTER="#7a8aed"
BCOLOR_INNER="#4a5aad"
CORNER_SIZE=6

magick -size ${GW}x${GH} xc:none \
  -fill none \
  -stroke "$BCOLOR_OUTER" -strokewidth 2 \
  -draw "rectangle ${OUTER},${OUTER} $((GW-OUTER)),$((GH-OUTER))" \
  -stroke "$BCOLOR_INNER" -strokewidth 2 \
  -draw "rectangle ${INNER},${INNER} $((GW-INNER)),$((GH-INNER))" \
  -fill "$BCOLOR_OUTER" -stroke none \
  -draw "rectangle $((OUTER-1)),$((OUTER-1)) $((OUTER+CORNER_SIZE)),$((OUTER+CORNER_SIZE))" \
  -draw "rectangle $((GW-OUTER-CORNER_SIZE)),$((OUTER-1)) $((GW-OUTER+1)),$((OUTER+CORNER_SIZE))" \
  -draw "rectangle $((OUTER-1)),$((GH-OUTER-CORNER_SIZE)) $((OUTER+CORNER_SIZE)),$((GH-OUTER+1))" \
  -draw "rectangle $((GW-OUTER-CORNER_SIZE)),$((GH-OUTER-CORNER_SIZE)) $((GW-OUTER+1)),$((GH-OUTER+1))" \
  "$WORK/frame.png"

# --- Final composite ---

magick "$WORK/bg_final.png" \
  "$WORK/g_icon_glow.png" -gravity north -geometry +0+$((G_ICON_Y-10)) -composite \
  "$WORK/glow_w.png" -composite \
  "$WORK/glow_w.png" -composite \
  "$WORK/glow_w.png" -composite \
  "$WORK/glow_w.png" -composite \
  "$WORK/glow_t.png" -composite \
  "$WORK/glow_t.png" -composite \
  "$WORK/glow_t.png" -composite \
  "$WORK/g_icon.png" -gravity north -geometry +0+${G_ICON_Y} -composite \
  "$WORK/frame.png" -composite \
  -font "$PRESS" -pointsize $G_TITLE_SIZE -fill "$GOLD" \
  -gravity north -annotate +0+${G_TITLE_Y} 'SAVECRAFT' \
  -font "$CHAKRA" -pointsize $G_TAG_SIZE -fill "$SILVER" \
  -gravity north -annotate +0+${G_TAG_Y} 'Real factory data for your AI' \
  "$MOD/gallery.png"

echo "  -> $MOD/gallery.png"

rm -rf "$WORK"
echo "=== Done ==="
