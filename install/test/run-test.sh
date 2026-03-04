#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# Savecraft Installer Test Harness
#
# Expects to run inside the Docker container built from Dockerfile.
# Fixtures are pre-copied to ~/fixtures/ by the Dockerfile.
# ---------------------------------------------------------------------------

readonly FIXTURES="${HOME}/fixtures"
readonly HTTP_PORT=8888
readonly HTTP_PID_FILE="/tmp/http-server.pid"

TEST_PUBKEY=""

PASSED=0
FAILED=0

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info() { printf '  \033[1;34mTEST\033[0m %s\n' "$*"; }
pass() {
    printf '  \033[1;32mPASS\033[0m %s\n' "$*"
    PASSED=$((PASSED + 1))
}
fail() {
    printf '  \033[1;31mFAIL\033[0m %s\n' "$*"
    FAILED=$((FAILED + 1))
}

# shellcheck disable=SC2317,SC2329 # invoked via trap
cleanup() {
    if [[ -f "${HTTP_PID_FILE}" ]]; then
        kill "$(cat "${HTTP_PID_FILE}")" 2>/dev/null || true
        rm -f "${HTTP_PID_FILE}"
    fi
    # Restore daemon fixtures if hidden by a test
    if [[ -d "${FIXTURES}/daemon-hidden" ]]; then
        mv "${FIXTURES}/daemon-hidden" "${FIXTURES}/daemon"
    fi
}
trap cleanup EXIT

assert_file_exists() {
    local path="$1"
    local label="${2:-${path}}"
    if [[ -f "${path}" ]]; then
        pass "${label} exists"
    else
        fail "${label} does not exist (expected at ${path})"
    fi
}

assert_executable() {
    local path="$1"
    local label="${2:-${path}}"
    if [[ -x "${path}" ]]; then
        pass "${label} is executable"
    else
        fail "${label} is not executable (${path})"
    fi
}

assert_file_contains() {
    local path="$1"
    local pattern="$2"
    # shellcheck disable=SC2016 # single quotes intentional in default label
    local label="${3:-${path} contains '${pattern}'}"
    if grep -q "${pattern}" "${path}"; then
        pass "${label}"
    else
        fail "${label} — pattern '${pattern}' not found in ${path}"
    fi
}

# Run installer, capture output regardless of exit code.
#   $1  path to install.sh
#   $@  additional arguments (caller passes --no-systemd if needed)
#   Sets: CAPTURED_OUTPUT, CAPTURED_EXIT_CODE
run_installer_capture() {
    local script="$1"
    shift
    set_installer_env
    CAPTURED_EXIT_CODE=0
    CAPTURED_OUTPUT="$(bash "${script}" "$@" 2>&1)" || CAPTURED_EXIT_CODE=$?
}

# Assert that CAPTURED_OUTPUT contains a pattern.
#   $1  grep -iE pattern
#   $2  human label
assert_output_contains() {
    local pattern="$1"
    local label="$2"
    if echo "${CAPTURED_OUTPUT}" | grep -qiE "${pattern}"; then
        pass "${label}"
    else
        fail "${label} — expected '${pattern}' in output, got:"
        echo "${CAPTURED_OUTPUT}" | tail -5 | sed 's/^/      /'
    fi
}

# Write a manifest.json for the daemon fixtures directory.
#   $1  path to binary to hash
generate_manifest() {
    local binary_path="$1"
    local hash
    hash="$(sha256sum "${binary_path}" | cut -d' ' -f1)"
    cat >"${FIXTURES}/daemon/manifest.json" <<EOF
{
  "linux-amd64": { "sha256": "${hash}" },
  "linux-arm64": { "sha256": "${hash}" }
}
EOF
}

# Remove installed files between tests so each starts clean.
clean_install_dirs() {
    rm -rf "${HOME}/.local/bin/savecraft-daemon"
    rm -rf "${HOME}/.config/savecraft"
    rm -rf "${HOME}/.cache/savecraft"
    rm -rf "${HOME}/.config/systemd"
}

# Set the env vars that the install worker would normally prepend.
set_installer_env() {
    export SAVECRAFT_INSTALL_URL="http://localhost:${HTTP_PORT}"
    export SAVECRAFT_SERVER_URL="https://api.savecraft.gg"
    export SAVECRAFT_INSTALLER_VERSION="0.0.0-test"
    export SAVECRAFT_ED25519_PUBKEY="${TEST_PUBKEY}"
}

# Run installer, expecting success.
#   $1  path to install.sh
run_installer_ok() {
    local script="$1"
    info "Running ${script##*/} --no-systemd (expect success)"
    set_installer_env
    bash "${script}" --no-systemd
}

# Run installer, expecting failure with a specific pattern in output.
#   $1  path to install.sh
#   $2  grep -iE pattern to match in combined stdout+stderr
#   $3  human label for pass/fail
run_installer_expect_failure() {
    local script="$1"
    local pattern="$2"
    local label="$3"
    info "Running ${script##*/} --no-systemd (expect failure: ${label})"
    set_installer_env

    local output exit_code
    exit_code=0
    output="$(bash "${script}" --no-systemd 2>&1)" || exit_code=$?

    if [[ "${exit_code}" -eq 0 ]]; then
        fail "${label}: installer succeeded but should have failed"
        return
    fi

    if echo "${output}" | grep -qiE "${pattern}"; then
        pass "${label}"
    else
        fail "${label} — expected '${pattern}' in output, got:"
        echo "${output}" | tail -5 | sed 's/^/      /'
    fi
}

# ---------------------------------------------------------------------------
# Generate ephemeral test fixtures (keypair, dummy binaries, signatures).
# No pre-built fixtures needed — everything is created at runtime.
# ---------------------------------------------------------------------------
generate_fixtures() {
    info "Generating ephemeral test fixtures..."

    local key_dir
    key_dir="$(mktemp -d)"

    # Generate ephemeral Ed25519 keypair
    openssl genpkey -algorithm Ed25519 -out "${key_dir}/private.pem" 2>/dev/null
    openssl pkey -in "${key_dir}/private.pem" -pubout -out "${key_dir}/public.pem" 2>/dev/null

    # Create dummy daemon binary (shell script that handles 'version')
    mkdir -p "${FIXTURES}/daemon"
    for arch in amd64 arm64; do
        local artifact="savecraft-daemon-linux-${arch}"
        cat >"${FIXTURES}/daemon/${artifact}" <<'SCRIPT'
#!/bin/bash
case "$1" in
    version) echo "savecraft-daemon 0.0.0-test" ;;
    *) echo "unknown command: $1" >&2; exit 1 ;;
esac
SCRIPT
        chmod +x "${FIXTURES}/daemon/${artifact}"

        # Sign binary with ephemeral key
        openssl pkeyutl -sign \
            -inkey "${key_dir}/private.pem" \
            -rawin \
            -in "${FIXTURES}/daemon/${artifact}" \
            -out "${FIXTURES}/daemon/${artifact}.sig"
    done

    # Extract base64 DER public key for the test harness
    openssl pkey -in "${key_dir}/private.pem" -pubout -outform DER 2>/dev/null \
        | base64 -w0 >"${FIXTURES}/pubkey.b64"

    # Set the global pubkey variable
    TEST_PUBKEY="$(cat "${FIXTURES}/pubkey.b64")"

    # Clean up private key
    rm -rf "${key_dir}"

    info "Fixtures ready"
}

# ---------------------------------------------------------------------------
# Start fixture HTTP server
#
# The server serves ~/fixtures/ which has:
#   daemon/savecraft-daemon-linux-amd64      (binary)
#   daemon/savecraft-daemon-linux-amd64.sig  (signature)
#   daemon/savecraft-daemon-linux-arm64      (binary)
#   daemon/savecraft-daemon-linux-arm64.sig  (signature)
# ---------------------------------------------------------------------------
start_http_server() {
    info "Starting HTTP server on port ${HTTP_PORT} serving ${FIXTURES}/"
    python3 -m http.server "${HTTP_PORT}" --directory "${FIXTURES}" --bind 127.0.0.1 &
    echo $! >"${HTTP_PID_FILE}"

    # Wait for the server to be ready
    local retries=20
    while ! curl -sf "http://localhost:${HTTP_PORT}/" >/dev/null 2>&1; do
        retries=$((retries - 1))
        if [[ "${retries}" -le 0 ]]; then
            fail "HTTP server failed to start"
            exit 1
        fi
        sleep 0.1
    done
    info "HTTP server ready (PID $(cat "${HTTP_PID_FILE}"))"
}

# ---------------------------------------------------------------------------
# Test: happy path — signed binary + correct pubkey → installs OK
# ---------------------------------------------------------------------------
test_happy_path() {
    info "=== Test: happy path ==="
    clean_install_dirs

    run_installer_ok "${FIXTURES}/install.sh"

    # Binary exists and is executable
    assert_file_exists "${HOME}/.local/bin/savecraft-daemon" "daemon binary"
    assert_executable "${HOME}/.local/bin/savecraft-daemon" "daemon binary"

    # --no-systemd means no unit file written
    if [[ -f "${HOME}/.config/systemd/user/savecraft.service" ]]; then
        fail "systemd unit should not exist with --no-systemd"
    else
        pass "systemd unit correctly absent with --no-systemd"
    fi

    # Env file exists and contains expected values
    assert_file_exists "${HOME}/.config/savecraft/env" "env file"
    assert_file_contains "${HOME}/.config/savecraft/env" \
        "SAVECRAFT_SERVER_URL=https://api.savecraft.gg" \
        "env file contains server URL"
    assert_file_contains "${HOME}/.config/savecraft/env" \
        "SAVECRAFT_INSTALL_URL=http://localhost:${HTTP_PORT}" \
        "env file contains install URL"
    # Auth token is no longer in env file — daemon self-registers on first boot
}

# ---------------------------------------------------------------------------
# Test: download 404 — binary missing from server → friendly error
# ---------------------------------------------------------------------------
test_download_failure() {
    info "=== Test: download failure (binary missing) ==="
    clean_install_dirs

    # Hide daemon binaries so HTTP server returns 404
    mv "${FIXTURES}/daemon" "${FIXTURES}/daemon-hidden"

    run_installer_expect_failure \
        "${FIXTURES}/install.sh" \
        "failed to download" \
        "download 404 gives friendly error"

    # Restore
    mv "${FIXTURES}/daemon-hidden" "${FIXTURES}/daemon"
}

# ---------------------------------------------------------------------------
# Test: missing pubkey — SAVECRAFT_ED25519_PUBKEY not set
# ---------------------------------------------------------------------------
test_missing_pubkey() {
    info "=== Test: missing public key ==="
    clean_install_dirs

    info "Running install.sh --no-systemd (expect failure: missing pubkey detected)"
    set_installer_env
    # Unset pubkey AFTER set_installer_env to simulate missing configuration
    unset SAVECRAFT_ED25519_PUBKEY

    local output exit_code
    exit_code=0
    output="$(bash "${FIXTURES}/install.sh" --no-systemd 2>&1)" || exit_code=$?

    if [[ "${exit_code}" -eq 0 ]]; then
        fail "missing pubkey detected: installer succeeded but should have failed"
        return
    fi

    if echo "${output}" | grep -qiE "SAVECRAFT_ED25519_PUBKEY"; then
        pass "missing pubkey detected"
    else
        fail "missing pubkey detected — expected 'SAVECRAFT_ED25519_PUBKEY' in output, got:"
        echo "${output}" | tail -5 | sed 's/^/      /'
    fi

    # Restore for subsequent tests
    export SAVECRAFT_ED25519_PUBKEY="${TEST_PUBKEY}"
}

# ---------------------------------------------------------------------------
# Test: bad signature — valid-but-wrong pubkey → verification fails
# ---------------------------------------------------------------------------
test_bad_signature() {
    info "=== Test: bad signature ==="
    clean_install_dirs

    info "Running install.sh --no-systemd (expect failure: wrong pubkey)"
    set_installer_env
    # A valid Ed25519 public key that doesn't match the signing key.
    # Generated from: openssl genpkey -algorithm Ed25519 | openssl pkey -pubout -outform DER | base64
    export SAVECRAFT_ED25519_PUBKEY="MCowBQYDK2VwAyEAGb1gauf3MIWivXGClBQyTnOmXMkGuBM4MKc+bVfxYgo="

    local output exit_code
    exit_code=0
    output="$(bash "${FIXTURES}/install.sh" --no-systemd 2>&1)" || exit_code=$?

    if [[ "${exit_code}" -eq 0 ]]; then
        fail "bad signature: installer succeeded but should have failed"
    elif echo "${output}" | grep -qiE "signature verification failed"; then
        pass "bad signature detected"
    else
        fail "bad signature — expected 'Signature verification FAILED' in output, got:"
        echo "${output}" | tail -5 | sed 's/^/      /'
    fi

    # Restore correct pubkey
    export SAVECRAFT_ED25519_PUBKEY="${TEST_PUBKEY}"
}

# ---------------------------------------------------------------------------
# Test: SHA256 dedup — same binary → skip re-download
# ---------------------------------------------------------------------------
test_sha256_dedup_skip() {
    info "=== Test: SHA256 dedup skip ==="
    clean_install_dirs

    # First install — normal download
    run_installer_ok "${FIXTURES}/install.sh"

    # Generate manifest matching the installed binary
    generate_manifest "${HOME}/.local/bin/savecraft-daemon"

    # Second install — should detect matching hash and skip download
    run_installer_capture "${FIXTURES}/install.sh" --no-systemd
    assert_output_contains "up to date" "SHA256 dedup skips re-download"

    rm -f "${FIXTURES}/daemon/manifest.json"
}

# ---------------------------------------------------------------------------
# Test: SHA256 dedup mismatch — different hash → re-download
# ---------------------------------------------------------------------------
test_sha256_dedup_mismatch() {
    info "=== Test: SHA256 dedup mismatch ==="
    clean_install_dirs

    # First install
    run_installer_ok "${FIXTURES}/install.sh"

    # Write manifest with a bogus hash
    cat >"${FIXTURES}/daemon/manifest.json" <<'EOF'
{
  "linux-amd64": { "sha256": "0000000000000000000000000000000000000000000000000000000000000000" },
  "linux-arm64": { "sha256": "0000000000000000000000000000000000000000000000000000000000000000" }
}
EOF

    # Second install — hash mismatch, should re-download
    run_installer_capture "${FIXTURES}/install.sh" --no-systemd
    assert_output_contains "downloading|installed" "SHA256 mismatch triggers re-download"

    rm -f "${FIXTURES}/daemon/manifest.json"
}

# ---------------------------------------------------------------------------
# Test: --help flag — exits 0, prints usage
# ---------------------------------------------------------------------------
test_help_flag() {
    info "=== Test: --help flag ==="
    clean_install_dirs

    run_installer_capture "${FIXTURES}/install.sh" --help

    if [[ "${CAPTURED_EXIT_CODE}" -eq 0 ]]; then
        pass "--help exits 0"
    else
        fail "--help exited ${CAPTURED_EXIT_CODE} (expected 0)"
    fi
    assert_output_contains "usage" "--help prints usage"
}

# ---------------------------------------------------------------------------
# Test: unknown argument — exits non-zero, prints error
# ---------------------------------------------------------------------------
test_unknown_argument() {
    info "=== Test: unknown argument ==="
    clean_install_dirs

    run_installer_capture "${FIXTURES}/install.sh" --bogus

    if [[ "${CAPTURED_EXIT_CODE}" -ne 0 ]]; then
        pass "--bogus exits non-zero"
    else
        fail "--bogus exited 0 (expected non-zero)"
    fi
    assert_output_contains "unknown argument" "--bogus prints error message"
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
print_summary() {
    echo ""
    echo "  =============================="
    echo "  Results: ${PASSED} passed, ${FAILED} failed"
    echo "  =============================="
    echo ""

    if [[ "${FAILED}" -gt 0 ]]; then
        echo "  FAIL"
        exit 1
    else
        echo "  PASS"
        exit 0
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    echo ""
    echo "  Savecraft Installer Test Suite"
    echo "  =============================="
    echo ""

    generate_fixtures
    start_http_server
    test_happy_path
    test_download_failure
    test_missing_pubkey
    test_bad_signature
    test_sha256_dedup_skip
    test_sha256_dedup_mismatch
    test_help_flag
    test_unknown_argument
    print_summary
}

main "$@"
