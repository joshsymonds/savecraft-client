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
readonly BINARY_NAME="${APP_NAME}-daemon"
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
# install_service — register and start the daemon as an OS service
# Delegates to the daemon binary, which writes the platform-native
# service config (systemd unit with security hardening, etc.).
# ---------------------------------------------------------------------------
install_service() {
    info "Registering daemon as OS service..."
    "${BIN_DIR}/${BINARY_NAME}" install \
        || die "Failed to register daemon as OS service"
    ok "Daemon registered as OS service"

    info "Starting daemon..."
    "${BIN_DIR}/${BINARY_NAME}" start \
        || die "Failed to start daemon"
    ok "Daemon started"
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
        echo "# The daemon registers automatically on first start and writes"
        echo "# SAVECRAFT_AUTH_TOKEN and SAVECRAFT_DEVICE_UUID to this file."
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
        echo "# Log level: debug, info, warn, error (default: info)"
        echo "# SAVECRAFT_LOG_LEVEL=info"
    } >"${CONFIG_DIR}/env"

    info "Created env template at ${CONFIG_DIR}/env"
}

# ---------------------------------------------------------------------------
# is_token_expired — check if an RFC3339 expiresAt timestamp is in the past
#   $1  expiresAt string (e.g. "2026-03-03T12:20:00Z")
#   Returns 0 if expired or unparseable, 1 if still valid.
# ---------------------------------------------------------------------------
is_token_expired() {
    local expires_at="$1"
    if [[ -z "${expires_at}" ]]; then
        return 0
    fi

    local expires_epoch now_epoch
    expires_epoch="$(date -d "${expires_at}" +%s 2>/dev/null || true)"
    if [[ -z "${expires_epoch}" ]]; then
        # Can't parse — treat as expired to be safe.
        return 0
    fi
    now_epoch="$(date +%s)"
    if [[ ${now_epoch} -ge ${expires_epoch} ]]; then
        return 0
    fi
    return 1
}

# ---------------------------------------------------------------------------
# repair_link — call POST /repair to get a fresh pairing token
#   $1  base URL (e.g. http://localhost:9182)
#   Prints the new link URL on success, empty string on failure.
# ---------------------------------------------------------------------------
repair_link() {
    local base_url="$1"
    local response
    response="$(curl -sf -X POST "${base_url}/repair" 2>/dev/null || true)"
    if [[ -n "${response}" ]]; then
        local link_url
        link_url="$(echo "${response}" | sed -n 's/.*"linkUrl"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        if [[ -n "${link_url}" ]]; then
            echo "${link_url}"
        fi
    fi
}

# ---------------------------------------------------------------------------
# wait_for_link — poll the daemon's /link endpoint for the link URL
#   $1  base URL (e.g. http://localhost:9182)
#   Prints "paired" if already linked, the link URL on success, or empty
#   string on timeout/failure.
# ---------------------------------------------------------------------------
wait_for_link() {
    local base_url="$1"
    local max_wait=15
    local waited=0

    info "Waiting for daemon to register..." >&2
    while [[ ${waited} -lt ${max_wait} ]]; do
        local http_code response
        # Capture both body and HTTP status code (-s without -f so we get error bodies).
        response="$(curl -s -w '\n%{http_code}' "${base_url}/link" 2>/dev/null || true)"
        if [[ -n "${response}" ]]; then
            http_code="$(echo "${response}" | tail -1)"
            local body
            body="$(echo "${response}" | sed '$d')"

            # 404 = already paired, no link code needed
            if [[ "${http_code}" == "404" ]]; then
                echo "paired"
                return
            fi

            if [[ "${http_code}" == "200" ]] && [[ -n "${body}" ]]; then
                local link_url expires_at
                link_url="$(echo "${body}" | sed -n 's/.*"linkUrl"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
                expires_at="$(echo "${body}" | sed -n 's/.*"expiresAt"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

                if [[ -n "${link_url}" ]]; then
                    # Check if the token is stale and needs refreshing.
                    if is_token_expired "${expires_at}"; then
                        info "Pairing token expired, requesting a fresh one..." >&2
                        local fresh_url
                        fresh_url="$(repair_link "${base_url}")"
                        if [[ -n "${fresh_url}" ]]; then
                            echo "${fresh_url}"
                            return
                        fi
                        # Repair failed — fall through to keep polling.
                    else
                        echo "${link_url}"
                        return
                    fi
                fi
            fi
        fi
        sleep 1
        waited=$((waited + 1))
    done
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
main() {
    local no_service=false

    # Parse arguments
    for arg in "$@"; do
        case "${arg}" in
            --no-service | --no-systemd) no_service=true ;;
            --help | -h)
                echo "Usage: install.sh [--no-service]"
                echo ""
                echo "Options:"
                echo "  --no-service   Skip OS service registration (for Docker/testing)"
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

    # Write env file — daemon self-registers on first boot
    create_env_template

    # Service registration
    if [[ "${no_service}" == "false" ]]; then
        install_service
    else
        info "Skipping OS service registration (--no-service)"
    fi

    # Summary
    echo ""
    echo "  Installation Summary"
    echo "  --------------------"
    echo "  Daemon:   ${BIN_DIR}/${BINARY_NAME}"
    echo "  Config:   ${CONFIG_DIR}/env"
    echo "  Cache:    ${CACHE_DIR}/"
    echo ""

    # Wait for the daemon to register and retrieve the link URL.
    if [[ "${no_service}" == "false" ]]; then
        local status_port="${SAVECRAFT_STATUS_PORT:-9182}"
        local link_url=""
        link_url="$(wait_for_link "http://localhost:${status_port}")"

        if [[ "${link_url}" == "paired" ]]; then
            echo ""
            ok "Device is already linked to your account."
            echo ""
        elif [[ -n "${link_url}" ]]; then
            echo ""
            ok "Device registered. Link it to your account:"
            echo ""
            echo "    ${link_url}"
            echo ""
        else
            echo ""
            warn "Something went wrong — the daemon did not respond in time."
            echo ""
            echo "  Try reinstalling:"
            echo "    curl -sSL ${INSTALL_URL} | bash"
            echo ""
            echo "  If the problem persists, check the logs and bring them to"
            echo "  our Discord (https://discord.gg/YnC8stpEmF) or paste them"
            echo "  into your local LLM for debugging:"
            echo ""
            echo "    journalctl --user -u ${BINARY_NAME} --no-pager -n 50"
            echo ""
        fi
    fi
    echo ""
}

main "$@"
