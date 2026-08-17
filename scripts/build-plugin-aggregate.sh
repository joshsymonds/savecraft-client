#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat >&2 <<'EOF'
usage: SERVER_URL=<url> PLUGINS_DIR=<dir> MODE=publish|check [R2_BUCKET=<bucket>] [DEPLOYED_PLUGIN=<game>] [ALLOW_REMOVE=<game,...>|*] [SIGNING_KEY_PATH=<path>] build-plugin-aggregate.sh
EOF
}

fail() {
    local reason=$1

    if [[ $MODE == check ]]; then
        printf 'manifest check FAIL: %s\n' "$reason" >&2
    else
        printf 'aggregate FAIL: %s\n' "$reason" >&2
    fi
    exit 1
}

require_var() {
    local name=$1

    if [[ -z ${!name:-} ]]; then
        usage
        fail "$name is required"
    fi
}

MODE=${MODE:-}
SERVER_URL=${SERVER_URL:-}
R2_BUCKET=${R2_BUCKET:-}
PLUGINS_DIR=${PLUGINS_DIR:-}
DEPLOYED_PLUGIN=${DEPLOYED_PLUGIN:-}
ALLOW_REMOVE=${ALLOW_REMOVE:-}
SIGNING_KEY_PATH=${SIGNING_KEY_PATH:-}

if [[ $MODE != publish && $MODE != check ]]; then
    usage
    printf 'aggregate FAIL: MODE must be publish or check\n' >&2
    exit 1
fi
require_var SERVER_URL
require_var PLUGINS_DIR
[[ -d $PLUGINS_DIR ]] || fail "PLUGINS_DIR is not a directory: $PLUGINS_DIR"
if [[ $MODE == publish ]]; then
    require_var R2_BUCKET
    require_var SIGNING_KEY_PATH
    [[ -r $SIGNING_KEY_PATH ]] || fail "SIGNING_KEY_PATH is not readable: $SIGNING_KEY_PATH"
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
served_manifest="$tmp_dir/served.json"
served_signature="${served_manifest}.sig"

declare -A served_set=()
declare -A repo_set=()
declare -A allowed_set=()
declare -A built_set=()

read_keys() {
    local manifest=$1
    jq -r '.plugins | keys[]' "$manifest"
}

tolerate_invalid_served() {
    [[ $MODE == publish && $ALLOW_REMOVE == \* ]]
}

fetch_url() {
    local url=$1
    local output=$2
    local code

    if ! code=$(curl -sS -o "$output" -w '%{http_code}' "$url"); then
        fail "network error fetching $url"
    fi
    printf '%s' "$code"
}

fetch_served() {
    local manifest_code signature_code key

    manifest_code=$(fetch_url "${SERVER_URL}/plugins/manifest.json" "$served_manifest")
    case $manifest_code in
        404)
            if [[ $MODE == check ]]; then
                fail "served manifest returned HTTP 404"
            fi
            printf 'warning: served manifest returned HTTP 404; treating served set as empty\n' >&2
            return
            ;;
        200) ;;
        *) fail "served manifest returned HTTP $manifest_code" ;;
    esac

    signature_code=$(fetch_url "${SERVER_URL}/plugins/manifest.json.sig" "$served_signature")
    if [[ $signature_code != 200 ]]; then
        if tolerate_invalid_served; then
            printf 'warning: served signature returned HTTP %s; ALLOW_REMOVE=* treats served set as empty\n' "$signature_code" >&2
            return
        fi
        fail "served signature returned HTTP $signature_code"
    fi
    if ! go run ./cmd/savecraft-verify "$served_manifest" >/dev/null 2>&1; then
        if tolerate_invalid_served; then
            printf 'warning: served signature verification failed; ALLOW_REMOVE=* treats served set as empty\n' >&2
            return
        fi
        fail "served manifest signature verification failed"
    fi
    if ! jq -e '.plugins | type == "object"' "$served_manifest" >/dev/null 2>&1; then
        if tolerate_invalid_served; then
            printf 'warning: served manifest is invalid; ALLOW_REMOVE=* treats served set as empty\n' >&2
            return
        fi
        fail "served manifest must contain object-valued .plugins"
    fi
    while IFS= read -r key; do
        served_set[$key]=1
    done < <(read_keys "$served_manifest")
}

load_repo_set() {
    local descriptor game

    shopt -s nullglob
    for descriptor in "$PLUGINS_DIR"/*/plugin.toml; do
        game=$(basename "$(dirname "$descriptor")")
        repo_set[$game]=1
    done
    shopt -u nullglob
}

load_allow_remove() {
    local game
    local -a entries=()

    [[ -z $ALLOW_REMOVE || $ALLOW_REMOVE == \* ]] && return
    IFS=, read -r -a entries <<<"$ALLOW_REMOVE"
    for game in "${entries[@]}"; do
        [[ -n $game ]] || fail "ALLOW_REMOVE contains an empty game"
        if [[ -n ${repo_set[$game]+set} ]]; then
            fail "ALLOW_REMOVE cannot name a repository plugin: $game"
        fi
        if [[ -n $DEPLOYED_PLUGIN && $game == "$DEPLOYED_PLUGIN" ]]; then
            fail "ALLOW_REMOVE cannot name the deployed plugin: $game"
        fi
        allowed_set[$game]=1
    done
}

is_allowed_remove() {
    local game=$1
    [[ $ALLOW_REMOVE == \* || -n ${allowed_set[$game]+set} ]]
}

join_sorted() {
    if (($# == 0)); then
        return
    fi
    printf '%s\n' "$@" | LC_ALL=C sort -u | paste -sd ' ' -
}

check_mode() {
    local game
    local -a missing=()

    for game in "${!repo_set[@]}"; do
        [[ -n ${served_set[$game]+set} ]] || missing+=("$game")
    done
    if ((${#missing[@]} > 0)); then
        fail "served manifest is missing repository plugins: $(join_sorted "${missing[@]}")"
    fi
    if ! jq -e 'all(.plugins[]; (.version | type == "string" and length > 0))' "$served_manifest" >/dev/null; then
        fail "every served plugin must have a non-empty version"
    fi
    printf 'manifest check OK games=%d\n' "${#served_set[@]}"
}

publish_mode() {
    local game base entry agg manifest_code signature_code attempt
    local -a candidates=() missing_required=() dropped_served=() built_keys=() verified_keys=()

    candidates+=("${!served_set[@]}" "${!repo_set[@]}")
    [[ -n $DEPLOYED_PLUGIN ]] && candidates+=("$DEPLOYED_PLUGIN")
    mapfile -t candidates < <(printf '%s\n' "${candidates[@]}" | sed '/^$/d' | LC_ALL=C sort -u)

    mkdir -p dist
    agg='{"plugins":{}}'
    for game in "${candidates[@]}"; do
        if ! wrangler r2 object get "${R2_BUCKET}/plugins/${game}/client-manifest.json" --file "dist/${game}.json" --remote; then
            if [[ -n ${repo_set[$game]+set} || $game == "$DEPLOYED_PLUGIN" ]] \
                || { [[ -n ${served_set[$game]+set} ]] && ! is_allowed_remove "$game"; }; then
                fail "aggregate would drop $game: client manifest unavailable"
            fi
            printf 'warning: client manifest unavailable for allowed removal %s; skipping\n' "$game" >&2
            continue
        fi
        base="${SERVER_URL}/plugins/${game}"
        if ! entry=$(jq -c --arg base "$base" '
            . + (if .url then {url: ($base + "/parser.wasm")} else {} end)
            + (if (.icon == "icon.png" or .icon == "icon.svg")
                then {icon_url: ($base + "/" + .icon)} else {} end)
        ' "dist/${game}.json"); then
            fail "invalid client manifest for $game"
        fi
        agg=$(jq -c --arg id "$game" --argjson entry "$entry" '.plugins[$id] = $entry' <<<"$agg")
        built_set[$game]=1
    done

    for game in "${!repo_set[@]}"; do
        [[ -n ${built_set[$game]+set} ]] || missing_required+=("$game")
    done
    if [[ -n $DEPLOYED_PLUGIN && -z ${built_set[$DEPLOYED_PLUGIN]+set} ]]; then
        missing_required+=("$DEPLOYED_PLUGIN")
    fi
    ((${#missing_required[@]} == 0)) || fail "built aggregate is missing required plugins: $(join_sorted "${missing_required[@]}")"

    for game in "${!served_set[@]}"; do
        if [[ -z ${built_set[$game]+set} ]] && ! is_allowed_remove "$game"; then
            dropped_served+=("$game")
        fi
    done
    ((${#dropped_served[@]} == 0)) || fail "built aggregate drops served plugins without ALLOW_REMOVE: $(join_sorted "${dropped_served[@]}")"

    printf '%s' "$agg" >dist/plugins-manifest.json
    go run ./cmd/savecraft-sign dist/plugins-manifest.json "$SIGNING_KEY_PATH"
    wrangler r2 object put "${R2_BUCKET}/plugins/manifest.json" --file dist/plugins-manifest.json --remote
    wrangler r2 object put "${R2_BUCKET}/plugins/manifest.json.sig" --file dist/plugins-manifest.json.sig --remote

    mapfile -t built_keys < <(printf '%s\n' "${!built_set[@]}" | LC_ALL=C sort)
    for ((attempt = 1; attempt <= 10; attempt++)); do
        if manifest_code=$(curl -sS -o "$tmp_dir/verify.json" -w '%{http_code}' "${SERVER_URL}/plugins/manifest.json") \
            && signature_code=$(curl -sS -o "$tmp_dir/verify.json.sig" -w '%{http_code}' "${SERVER_URL}/plugins/manifest.json.sig") \
            && [[ $manifest_code == 200 && $signature_code == 200 ]] \
            && cmp -s dist/plugins-manifest.json "$tmp_dir/verify.json" \
            && cmp -s dist/plugins-manifest.json.sig "$tmp_dir/verify.json.sig" \
            && go run ./cmd/savecraft-verify "$tmp_dir/verify.json" >/dev/null 2>&1 \
            && jq -e '.plugins | type == "object"' "$tmp_dir/verify.json" >/dev/null 2>&1; then
            mapfile -t verified_keys < <(read_keys "$tmp_dir/verify.json" | LC_ALL=C sort)
            if [[ $(printf '%s\n' "${verified_keys[@]}") == "$(printf '%s\n' "${built_keys[@]}")" ]]; then
                printf 'aggregate OK env=%s games=%d [%s]\n' "$SERVER_URL" "${#built_keys[@]}" "$(join_sorted "${built_keys[@]}")"
                return
            fi
        fi
        ((attempt < 10)) && sleep 3
    done
    fail "post-upload verification failed after 10 attempts"
}

load_repo_set
load_allow_remove
fetch_served
if [[ $MODE == check ]]; then
    check_mode
else
    publish_mode
fi
