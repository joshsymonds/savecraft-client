#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# Savecraft Daemon Installer
# Designed for Linux desktops and Steam Deck (SteamOS).
# ---------------------------------------------------------------------------

readonly VERSION="dev"
readonly BASE_URL="${SAVECRAFT_BASE_URL:-https://github.com/joshsymonds/savecraft.gg/releases/download}"

readonly BIN_DIR="${HOME}/.local/bin"
readonly CONFIG_DIR="${HOME}/.config/savecraft"
readonly CACHE_DIR="${HOME}/.cache/savecraft"
readonly SYSTEMD_DIR="${HOME}/.config/systemd/user"

readonly BINARY_NAME="savecraft-daemon"

# Ed25519 public key used to verify release signatures.
# Replace this placeholder with the real base64-encoded public key before shipping.
readonly ED25519_PUBKEY_B64="REPLACE_WITH_BASE64_PUBKEY"

# Temp files for download — module-level so the EXIT trap can clean them up.
TMP_BINARY=""
TMP_SIG=""

cleanup() {
    [[ -n "${TMP_BINARY}" ]] && rm -f "${TMP_BINARY}"
    [[ -n "${TMP_SIG}" ]] && rm -f "${TMP_SIG}"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info() { printf '  \033[1;34m->\033[0m %s\n' "$*"; }
ok() { printf '  \033[1;32m->\033[0m %s\n' "$*"; }
warn() { printf '  \033[1;33m->\033[0m %s\n' "$*" >&2; }
die() {
    printf '  \033[1;31merror:\033[0m %s\n' "$*" >&2
    exit 1
}

# ---------------------------------------------------------------------------
# detect_arch — map uname -m to Go-style arch names
# ---------------------------------------------------------------------------
detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64 | amd64) echo "amd64" ;;
        aarch64 | arm64) echo "arm64" ;;
        *) die "Unsupported architecture: ${machine}" ;;
    esac
}

# ---------------------------------------------------------------------------
# detect_os — only Linux is supported
# ---------------------------------------------------------------------------
detect_os() {
    local kernel
    kernel="$(uname -s)"
    case "${kernel}" in
        Linux) echo "linux" ;;
        *) die "Unsupported operating system: ${kernel}. Only Linux is supported." ;;
    esac
}

# ---------------------------------------------------------------------------
# verify_signature — Ed25519 signature check via openssl
#   $1  path to the binary
#   $2  path to the .sig file (raw Ed25519 signature)
# Returns 0 on success, 1 on failure.
# ---------------------------------------------------------------------------
verify_signature() {
    local binary_path="$1"
    local sig_path="$2"
    local tmp_pem

    if ! command -v openssl >/dev/null 2>&1; then
        warn "openssl not found — skipping signature verification"
        return 0
    fi

    tmp_pem="$(mktemp)"
    # shellcheck disable=SC2064
    trap "rm -f '${tmp_pem}'" RETURN

    # Build a PEM public key file from the embedded base64 key.
    {
        echo "-----BEGIN PUBLIC KEY-----"
        echo "${ED25519_PUBKEY_B64}"
        echo "-----END PUBLIC KEY-----"
    } >"${tmp_pem}"

    if openssl pkeyutl -verify \
        -pubin -inkey "${tmp_pem}" \
        -sigfile "${sig_path}" \
        -in "${binary_path}" \
        -rawin 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

# ---------------------------------------------------------------------------
# download — fetch a URL to a local path; curl with wget fallback
#   $1  URL
#   $2  output path
# ---------------------------------------------------------------------------
download() {
    local url="$1"
    local out="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 --retry-delay 2 -o "${out}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "${out}" "${url}"
    else
        die "Neither curl nor wget found. Install one and re-run."
    fi
}

# ---------------------------------------------------------------------------
# detect_games — look for known Proton prefixes under Steam compatdata
# ---------------------------------------------------------------------------
detect_games() {
    local steam_compat="${HOME}/.local/share/Steam/steamapps/compatdata"

    if [[ ! -d "${steam_compat}" ]]; then
        info "Steam compatdata directory not found — skipping game detection"
        return
    fi

    info "Scanning for supported games..."

    # Diablo II: Resurrected — Steam app ID 2201080
    local d2r_path="${steam_compat}/2201080"
    if [[ -d "${d2r_path}" ]]; then
        ok "Found Diablo II: Resurrected (app 2201080)"
    fi

    # Add more game checks here as support is added.
}

# ---------------------------------------------------------------------------
# install_systemd_unit — write unit file + enable (+ start if configured)
# ---------------------------------------------------------------------------
install_systemd_unit() {
    mkdir -p "${SYSTEMD_DIR}"

    cat >"${SYSTEMD_DIR}/savecraft.service" <<'UNIT'
[Unit]
Description=Savecraft Daemon
After=network-online.target

[Service]
ExecStart=%h/.local/bin/savecraft-daemon
Restart=always
RestartSec=5
EnvironmentFile=-%h/.config/savecraft/env

ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%h/.config/savecraft %h/.cache/savecraft %h/.local/bin

NoNewPrivileges=yes
PrivateTmp=yes

RestrictAddressFamilies=AF_INET AF_INET6

[Install]
WantedBy=default.target
UNIT

    info "Installed systemd user unit to ${SYSTEMD_DIR}/savecraft.service"

    systemctl --user daemon-reload

    if [[ -f "${CONFIG_DIR}/env" ]] && grep -q '^SAVECRAFT_AUTH_TOKEN=' "${CONFIG_DIR}/env"; then
        systemctl --user enable --now savecraft.service
        ok "Enabled and started savecraft.service"
    else
        systemctl --user enable savecraft.service
        warn "Enabled savecraft.service but NOT starting — set SAVECRAFT_AUTH_TOKEN in ${CONFIG_DIR}/env first"
    fi
}

# ---------------------------------------------------------------------------
# create_env_template — write a commented env file if one doesn't exist
# ---------------------------------------------------------------------------
create_env_template() {
    if [[ -f "${CONFIG_DIR}/env" ]]; then
        info "Env file already exists at ${CONFIG_DIR}/env — not overwriting"
        return
    fi

    {
        echo "# Savecraft Daemon configuration"
        echo ""
        # Write actual values from env vars if provided, otherwise commented template
        if [[ -n "${SAVECRAFT_SERVER_URL:-}" ]]; then
            echo "SAVECRAFT_SERVER_URL=${SAVECRAFT_SERVER_URL}"
        else
            echo "# Server URL (default: https://api.savecraft.gg)"
            echo "# SAVECRAFT_SERVER_URL=https://api.savecraft.gg"
        fi
        echo ""
        if [[ -n "${SAVECRAFT_AUTH_TOKEN:-}" ]]; then
            echo "SAVECRAFT_AUTH_TOKEN=${SAVECRAFT_AUTH_TOKEN}"
        else
            echo "# Authentication token from your savecraft.gg account"
            echo "# SAVECRAFT_AUTH_TOKEN="
        fi
        echo ""
        echo "# Log level: debug, info, warn, error (default: info)"
        echo "# SAVECRAFT_LOG_LEVEL=info"
    } >"${CONFIG_DIR}/env"

    if [[ -n "${SAVECRAFT_AUTH_TOKEN:-}" ]]; then
        ok "Created env file with auth token at ${CONFIG_DIR}/env"
    else
        info "Created env template at ${CONFIG_DIR}/env"
    fi
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
main() {
    local no_systemd=false

    # Parse arguments
    for arg in "$@"; do
        case "${arg}" in
            --no-systemd) no_systemd=true ;;
            --help | -h)
                echo "Usage: install.sh [--no-systemd]"
                echo ""
                echo "Options:"
                echo "  --no-systemd   Skip systemd unit installation (for Docker/testing)"
                exit 0
                ;;
            *) die "Unknown argument: ${arg}" ;;
        esac
    done

    echo ""
    echo "  Savecraft Daemon Installer v${VERSION}"
    echo "  ======================================"
    echo ""

    # Detect platform
    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"
    info "Platform: ${os}/${arch}"

    # Create directories
    mkdir -p "${BIN_DIR}" "${CONFIG_DIR}" "${CACHE_DIR}"
    info "Created directories"

    # Download binary + signature
    local artifact="${BINARY_NAME}-${os}-${arch}"
    local release_base="${BASE_URL}/daemon-v${VERSION}"
    local binary_url="${release_base}/${artifact}"
    local sig_url="${release_base}/${artifact}.sig"

    TMP_BINARY="$(mktemp)"
    TMP_SIG="$(mktemp)"

    info "Downloading ${artifact}..."
    download "${binary_url}" "${TMP_BINARY}"

    info "Downloading ${artifact}.sig..."
    download "${sig_url}" "${TMP_SIG}"

    # Verify signature
    info "Verifying Ed25519 signature..."
    if verify_signature "${TMP_BINARY}" "${TMP_SIG}"; then
        ok "Signature verified"
    else
        die "Signature verification FAILED — aborting install"
    fi

    # Install binary
    cp "${TMP_BINARY}" "${BIN_DIR}/${BINARY_NAME}"
    chmod +x "${BIN_DIR}/${BINARY_NAME}"
    ok "Installed ${BINARY_NAME} to ${BIN_DIR}/${BINARY_NAME}"

    # Pair device or write env file
    local paired=false
    if [[ -n "${SAVECRAFT_AUTH_TOKEN:-}" ]]; then
        # API key flow (headless/automation) — write env directly
        create_env_template
    elif [[ -n "${SAVECRAFT_SERVER_URL:-}" ]]; then
        # Interactive pairing flow
        echo ""
        info "Pairing your device..."
        info "Enter the 6-digit code shown on savecraft.gg"
        echo ""
        if "${BIN_DIR}/${BINARY_NAME}" pair --server "${SAVECRAFT_SERVER_URL}"; then
            ok "Device paired successfully"
            paired=true
        else
            warn "Pairing failed — you can pair later with:"
            warn "  savecraftd pair --server ${SAVECRAFT_SERVER_URL}"
            create_env_template
        fi
    else
        create_env_template
    fi

    # Systemd
    if [[ "${no_systemd}" == "false" ]]; then
        install_systemd_unit
    else
        info "Skipping systemd unit installation (--no-systemd)"
    fi

    # Game detection
    detect_games

    # Summary
    echo ""
    echo "  Installation Summary"
    echo "  --------------------"
    echo "  Binary:   ${BIN_DIR}/${BINARY_NAME}"
    echo "  Config:   ${CONFIG_DIR}/env"
    echo "  Cache:    ${CACHE_DIR}/"
    if [[ "${no_systemd}" == "false" ]]; then
        echo "  Service:  ${SYSTEMD_DIR}/savecraft.service"
    fi
    echo ""

    if [[ "${paired}" == "true" ]]; then
        echo "  Device is paired and ready."
        if [[ "${no_systemd}" == "false" ]]; then
            echo "  Check daemon status: systemctl --user status savecraft"
        fi
    elif [[ "${no_systemd}" == "false" ]]; then
        echo "  Next steps:"
        echo "    1. Pair your device: savecraftd pair --server <your-server-url>"
        echo "    2. Start the daemon: systemctl --user start savecraft"
        echo "    3. Check status:     systemctl --user status savecraft"
    else
        echo "  Next steps:"
        echo "    1. Pair your device: savecraftd pair --server <your-server-url>"
        echo "    2. Run: ${BIN_DIR}/${BINARY_NAME}"
    fi
    echo ""
}

main "$@"
