#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# Savecraft Daemon Installer
# Designed for Linux desktops and Steam Deck (SteamOS).
# ---------------------------------------------------------------------------

# All configuration is injected by the install worker at serve time.
# For local/manual use, set these env vars before running the script.
readonly INSTALLER_VERSION="${SAVECRAFT_INSTALLER_VERSION:?SAVECRAFT_INSTALLER_VERSION must be set}"
readonly INSTALL_URL="${SAVECRAFT_INSTALL_URL:?SAVECRAFT_INSTALL_URL must be set}"
readonly ED25519_PUBKEY_B64="${SAVECRAFT_ED25519_PUBKEY:?SAVECRAFT_ED25519_PUBKEY must be set}"

readonly APP_NAME="${SAVECRAFT_APP_NAME:-savecraft}"
readonly BIN_DIR="${HOME}/.local/bin"
readonly CONFIG_DIR="${HOME}/.config/${APP_NAME}"
readonly CACHE_DIR="${HOME}/.cache/${APP_NAME}"
readonly SYSTEMD_DIR="${HOME}/.config/systemd/user"

readonly BINARY_NAME="${APP_NAME}-daemon"
readonly SERVICE_NAME="${APP_NAME}.service"

# Temp files for download — module-level so the EXIT trap can clean them up.
TMP_BINARY=""
TMP_SIG=""

cleanup() {
    if [[ -n "${TMP_BINARY}" ]]; then rm -f "${TMP_BINARY}"; fi
    if [[ -n "${TMP_SIG}" ]]; then rm -f "${TMP_SIG}"; fi
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
# install_systemd_unit — write unit file + enable (+ start if configured)
# ---------------------------------------------------------------------------
install_systemd_unit() {
    mkdir -p "${SYSTEMD_DIR}"

    cat >"${SYSTEMD_DIR}/${SERVICE_NAME}" <<UNIT
[Unit]
Description=Savecraft Daemon
After=network-online.target

[Service]
ExecStart=%h/.local/bin/${BINARY_NAME}
Restart=always
RestartSec=5
EnvironmentFile=-%h/.config/${APP_NAME}/env

ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%h/.config/${APP_NAME} %h/.cache/${APP_NAME} %h/.local/bin

NoNewPrivileges=yes
PrivateTmp=yes

RestrictAddressFamilies=AF_INET AF_INET6

[Install]
WantedBy=default.target
UNIT

    info "Installed systemd user unit to ${SYSTEMD_DIR}/${SERVICE_NAME}"

    systemctl --user daemon-reload

    if [[ -f "${CONFIG_DIR}/env" ]] && grep -q '^SAVECRAFT_AUTH_TOKEN=' "${CONFIG_DIR}/env"; then
        systemctl --user enable --now "${SERVICE_NAME}"
        ok "Enabled and started ${SERVICE_NAME}"
    else
        systemctl --user enable "${SERVICE_NAME}"
        warn "Enabled ${SERVICE_NAME} but NOT starting — set SAVECRAFT_AUTH_TOKEN in ${CONFIG_DIR}/env first"
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
        if [[ -n "${SAVECRAFT_INSTALL_URL:-}" ]]; then
            echo "SAVECRAFT_INSTALL_URL=${SAVECRAFT_INSTALL_URL}"
        else
            echo "# Install URL for daemon updates (default: https://install.savecraft.gg)"
            echo "# SAVECRAFT_INSTALL_URL=https://install.savecraft.gg"
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
# run_pairing — interactive pairing with retry loop
#   $1  server URL
#   $2  extra flags for pair command (e.g. "--force"), or ""
#   Sets paired=true on success, pairing_failed=true on exhaustion.
# ---------------------------------------------------------------------------
run_pairing() {
    local server_url="$1"
    local extra_flags="$2"
    local max_attempts=3
    local attempt=0
    echo ""
    info "Enter the 6-digit code shown on ${frontend_url}"
    echo ""
    while [[ ${attempt} -lt ${max_attempts} ]]; do
        attempt=$((attempt + 1))
        # shellcheck disable=SC2086
        if "${BIN_DIR}/${BINARY_NAME}" pair --server "${server_url}" ${extra_flags}; then
            ok "Device paired successfully"
            paired=true
            return
        fi
        if [[ ${attempt} -lt ${max_attempts} ]]; then
            warn "Try again (attempt ${attempt}/${max_attempts})"
            echo ""
        fi
    done
    pairing_failed=true
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
    echo "  Savecraft Daemon Installer v${INSTALLER_VERSION}"
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

    # Check if we can skip the download (same binary already installed)
    local artifact="${BINARY_NAME}-${os}-${arch}"
    local binary_url="${INSTALL_URL}/daemon/${artifact}"
    local sig_url="${INSTALL_URL}/daemon/${artifact}.sig"
    local manifest_url="${INSTALL_URL}/daemon/manifest.json"
    local skip_download=false

    if [[ -x "${BIN_DIR}/${BINARY_NAME}" ]] && command -v sha256sum >/dev/null 2>&1; then
        local tmp_manifest
        tmp_manifest="$(mktemp)"
        if download "${manifest_url}" "${tmp_manifest}" 2>/dev/null; then
            # Extract expected sha256 for this platform (simple grep, no jq needed).
            # Manifest format: "linux-amd64": { ... "sha256": "hexstring" ... }
            local expected_hash
            expected_hash="$(grep -A5 "\"${os}-${arch}\"" "${tmp_manifest}" \
                | grep '"sha256"' \
                | head -1 \
                | sed 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([a-f0-9]*\)".*/\1/')"
            if [[ -n "${expected_hash}" ]]; then
                local local_hash
                local_hash="$(sha256sum "${BIN_DIR}/${BINARY_NAME}" | cut -d' ' -f1)"
                if [[ "${local_hash}" == "${expected_hash}" ]]; then
                    skip_download=true
                fi
            fi
        fi
        rm -f "${tmp_manifest}"
    fi

    if [[ "${skip_download}" == "true" ]]; then
        local daemon_version
        daemon_version="$("${BIN_DIR}/${BINARY_NAME}" version 2>&1 || true)"
        ok "Binary up to date (${daemon_version:-${BINARY_NAME}})"
    else
        TMP_BINARY="$(mktemp)"
        TMP_SIG="$(mktemp)"

        info "Downloading ${artifact}..."
        download "${binary_url}" "${TMP_BINARY}" \
            || die "Failed to download ${artifact} from ${binary_url}"

        info "Downloading ${artifact}.sig..."
        download "${sig_url}" "${TMP_SIG}" \
            || die "Failed to download ${artifact}.sig from ${sig_url}"

        # Verify signature
        info "Verifying Ed25519 signature..."
        if verify_signature "${TMP_BINARY}" "${TMP_SIG}"; then
            ok "Signature verified"
        else
            die "Signature verification FAILED — aborting install"
        fi

        # Install binary (mv is atomic and works even if the target is a running
        # executable — the old inode stays alive for the running process).
        chmod +x "${TMP_BINARY}"
        mv -f "${TMP_BINARY}" "${BIN_DIR}/${BINARY_NAME}"
        TMP_BINARY="" # already moved; prevent cleanup trap from deleting it
        local daemon_version
        daemon_version="$("${BIN_DIR}/${BINARY_NAME}" version 2>&1 || true)"
        ok "Installed ${daemon_version:-${BINARY_NAME}}"
    fi

    # Pair device or write env file
    local paired=false
    local pairing_failed=false
    local frontend_url="${SAVECRAFT_FRONTEND_URL:-https://savecraft.gg}"
    if [[ -n "${SAVECRAFT_AUTH_TOKEN:-}" ]]; then
        # API key flow (headless/automation) — write env directly
        create_env_template
    elif [[ -f "${CONFIG_DIR}/env" ]] && grep -q '^SAVECRAFT_AUTH_TOKEN=' "${CONFIG_DIR}/env"; then
        # Token exists in env file — verify it's still valid
        local verify_server="${SAVECRAFT_SERVER_URL:-$(grep '^SAVECRAFT_SERVER_URL=' "${CONFIG_DIR}/env" 2>/dev/null | cut -d= -f2-)}"
        if [[ -n "${verify_server}" ]] \
            && "${BIN_DIR}/${BINARY_NAME}" verify --server "${verify_server}" 2>/dev/null; then
            ok "Device already paired (token verified)"
            paired=true
        elif [[ -n "${SAVECRAFT_SERVER_URL:-}" ]]; then
            # Token invalid — re-pair with --force
            warn "Existing token is invalid — re-pairing..."
            run_pairing "${SAVECRAFT_SERVER_URL}" "--force"
        else
            warn "Existing token could not be verified (no server URL available)"
        fi
    elif [[ -n "${SAVECRAFT_SERVER_URL:-}" ]]; then
        # First-time interactive pairing flow
        info "Pairing your device..."
        run_pairing "${SAVECRAFT_SERVER_URL}" ""
    else
        create_env_template
    fi

    # If pairing was attempted and failed, bail out early.
    # The binary is already installed — the user just re-runs the installer
    # with a new pairing code.
    if [[ "${pairing_failed}" == "true" ]]; then
        echo ""
        warn "Pairing failed. Generate a new code on ${frontend_url} and re-run:"
        warn "  curl -sSL ${INSTALL_URL} | bash"
        echo ""
        exit 1
    fi

    # Systemd
    if [[ "${no_systemd}" == "false" ]]; then
        install_systemd_unit
    else
        info "Skipping systemd unit installation (--no-systemd)"
    fi

    # Summary
    echo ""
    echo "  Installation Summary"
    echo "  --------------------"
    echo "  Binary:   ${BIN_DIR}/${BINARY_NAME}"
    echo "  Config:   ${CONFIG_DIR}/env"
    echo "  Cache:    ${CACHE_DIR}/"
    if [[ "${no_systemd}" == "false" ]]; then
        echo "  Service:  ${SYSTEMD_DIR}/${SERVICE_NAME}"
    fi
    echo ""

    if [[ "${paired}" == "true" ]]; then
        echo "  Device is paired and ready."
        if [[ "${no_systemd}" == "false" ]]; then
            echo "  Check daemon status: systemctl --user status ${APP_NAME}"
        fi
    elif [[ "${no_systemd}" == "false" ]]; then
        echo "  Next steps:"
        echo "    Start the daemon: systemctl --user start ${APP_NAME}"
        echo "    Check status:     systemctl --user status ${APP_NAME}"
    fi
    echo ""
}

main "$@"
