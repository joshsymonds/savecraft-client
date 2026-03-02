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
# Run installer
# ---------------------------------------------------------------------------
run_installer() {
    info "Running install.sh --no-systemd"

    export SAVECRAFT_INSTALL_URL="http://localhost:${HTTP_PORT}"
    export SAVECRAFT_SERVER_URL="https://api.savecraft.gg"

    bash "${FIXTURES}/install.sh" --no-systemd
}

# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------
run_assertions() {
    info "Running assertions..."

    # Binary exists and is executable
    assert_file_exists "${HOME}/.local/bin/savecraft-daemon" "daemon binary"
    assert_executable "${HOME}/.local/bin/savecraft-daemon" "daemon binary"

    # Systemd unit file exists and contains expected content
    assert_file_exists "${HOME}/.config/systemd/user/savecraft.service" "systemd unit"
    assert_file_contains "${HOME}/.config/systemd/user/savecraft.service" \
        "ProtectSystem=strict" \
        "systemd unit contains ProtectSystem=strict"

    # Env file exists and contains expected URLs
    assert_file_exists "${HOME}/.config/savecraft/env" "env file"
    assert_file_contains "${HOME}/.config/savecraft/env" \
        "SAVECRAFT_SERVER_URL=https://api.savecraft.gg" \
        "env file contains server URL"
    assert_file_contains "${HOME}/.config/savecraft/env" \
        "SAVECRAFT_INSTALL_URL=http://localhost:${HTTP_PORT}" \
        "env file contains install URL"

    # Binary runs and outputs a version string
    local version_output
    if version_output="$("${HOME}/.local/bin/savecraft-daemon" --version 2>&1)"; then
        if [[ -n "${version_output}" ]]; then
            pass "daemon --version outputs: ${version_output}"
        else
            fail "daemon --version produced empty output"
        fi
    else
        # The fixture binary is a shell script stub, so even a non-zero exit
        # with version output is acceptable for this test.
        version_output="$("${HOME}/.local/bin/savecraft-daemon" --version 2>&1 || true)"
        if [[ -n "${version_output}" ]]; then
            pass "daemon --version outputs: ${version_output}"
        else
            fail "daemon --version failed with no output"
        fi
    fi
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

    start_http_server
    run_installer
    run_assertions
    print_summary
}

main "$@"
