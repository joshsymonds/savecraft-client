#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# Savecraft Daemon Installer
# Designed for Linux desktops and Steam Deck (SteamOS).
# ---------------------------------------------------------------------------

readonly VERSION="0.1.0"
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

info()  { printf '  \033[1;34m->\033[0m %s\n' "$*"; }
ok()    { printf '  \033[1;32m->\033[0m %s\n' "$*"; }
warn()  { printf '  \033[1;33m->\033[0m %s\n' "$*" >&2; }
die()   { printf '  \033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# detect_arch — map uname -m to Go-style arch names
# ---------------------------------------------------------------------------
detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             die "Unsupported architecture: ${machine}" ;;
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
        *)     die "Unsupported operating system: ${kernel}. Only Linux is supported." ;;
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
    } > "${tmp_pem}"

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
# install_systemd_unit — write the unit file via heredoc
# ---------------------------------------------------------------------------
install_systemd_unit() {
    mkdir -p "${SYSTEMD_DIR}"

    cat > "${SYSTEMD_DIR}/savecraft.service" << 'UNIT'
[Unit]
Description=Savecraft Daemon
After=network-online.target

[Service]
ExecStart=%h/.local/bin/savecraft-daemon
Restart=on-failure
RestartSec=5
EnvironmentFile=-%h/.config/savecraft/env

ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%h/.config/savecraft %h/.cache/savecraft

NoNewPrivileges=yes
PrivateTmp=yes

RestrictAddressFamilies=AF_INET AF_INET6

[Install]
WantedBy=default.target
UNIT

    info "Installed systemd user unit to ${SYSTEMD_DIR}/savecraft.service"

    systemctl --user daemon-reload

    # Only enable (and potentially start) if the auth token is configured.
    if [[ -f "${CONFIG_DIR}/env" ]] && grep -q '^SAVECRAFT_AUTH_TOKEN=' "${CONFIG_DIR}/env"; then
        systemctl --user enable savecraft.service
        ok "Enabled savecraft.service (auth token found)"
    else
        systemctl --user enable savecraft.service
        warn "Enabled savecraft.service but NOT starting — set SAVECRAFT_AUTH_TOKEN in ${CONFIG_DIR}/env first"
    fi
}

# ---------------------------------------------------------------------------
# write_service_file — write the systemd unit without activating it
# ---------------------------------------------------------------------------
write_service_file() {
    mkdir -p "${SYSTEMD_DIR}"

    cat > "${SYSTEMD_DIR}/savecraft.service" << 'UNIT'
[Unit]
Description=Savecraft Daemon
After=network-online.target

[Service]
ExecStart=%h/.local/bin/savecraft-daemon
Restart=on-failure
RestartSec=5
EnvironmentFile=-%h/.config/savecraft/env

ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%h/.config/savecraft %h/.cache/savecraft

NoNewPrivileges=yes
PrivateTmp=yes

RestrictAddressFamilies=AF_INET AF_INET6

[Install]
WantedBy=default.target
UNIT

    info "Wrote systemd unit file to ${SYSTEMD_DIR}/savecraft.service (not activated)"
}

# ---------------------------------------------------------------------------
# create_env_template — write a commented env file if one doesn't exist
# ---------------------------------------------------------------------------
create_env_template() {
    if [[ -f "${CONFIG_DIR}/env" ]]; then
        info "Env file already exists at ${CONFIG_DIR}/env — not overwriting"
        return
    fi

    cat > "${CONFIG_DIR}/env" << 'ENV'
# Savecraft Daemon configuration
# Uncomment and fill in the values below.

# Server URL (default: wss://api.savecraft.gg)
# SAVECRAFT_SERVER_URL=wss://api.savecraft.gg

# Authentication token from your savecraft.gg account
# SAVECRAFT_AUTH_TOKEN=

# Log level: debug, info, warn, error (default: info)
# SAVECRAFT_LOG_LEVEL=info
ENV

    info "Created env template at ${CONFIG_DIR}/env"
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
            --help|-h)
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
    local release_base="${BASE_URL}/v${VERSION}"
    local artifact="${BINARY_NAME}-${os}-${arch}"
    TMP_BINARY="$(mktemp)"
    TMP_SIG="$(mktemp)"

    info "Downloading ${artifact}..."
    download "${release_base}/${artifact}" "${TMP_BINARY}"

    info "Downloading ${artifact}.sig..."
    download "${release_base}/${artifact}.sig" "${TMP_SIG}"

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

    # Env file
    create_env_template

    # Systemd
    if [[ "${no_systemd}" == "false" ]]; then
        install_systemd_unit
    else
        info "Skipping systemd unit installation (--no-systemd)"
        write_service_file
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
    echo "  Service:  ${SYSTEMD_DIR}/savecraft.service"
    echo ""

    if [[ "${no_systemd}" == "false" ]]; then
        echo "  Next steps:"
        echo "    1. Edit ${CONFIG_DIR}/env and set SAVECRAFT_AUTH_TOKEN"
        echo "    2. Start the daemon: systemctl --user start savecraft"
        echo "    3. Check status:     systemctl --user status savecraft"
    else
        echo "  Next steps:"
        echo "    1. Edit ${CONFIG_DIR}/env and set SAVECRAFT_AUTH_TOKEN"
        echo "    2. Run: ${BIN_DIR}/${BINARY_NAME}"
    fi
    echo ""
}

main "$@"
