#!/usr/bin/env bash
set -euo pipefail

usage() {
    printf 'usage: release-plugin.sh <game> <version>\n' >&2
}

fail() {
    printf '%s\n' "$1" >&2
    exit 1
}

validate_plugin() {
    local game=$1

    [[ -f plugins/$game/plugin.toml ]] || fail "plugin manifest does not exist: plugins/$game/plugin.toml"
}

validate_version() {
    local version=$1

    [[ $version =~ ^[0-9]+(\.[0-9]+)*$ ]] || fail "version must be dotted numeric: $version"
}

normalize_segment() {
    local segment=$1

    segment=${segment#"${segment%%[!0]*}"}
    printf '%s' "${segment:-0}"
}

version_is_newer() {
    local latest=$1 current=$2 latest_segment current_segment
    local -a latest_parts current_parts
    local i count

    IFS=. read -r -a latest_parts <<<"$latest"
    IFS=. read -r -a current_parts <<<"$current"
    count=${#latest_parts[@]}
    ((${#current_parts[@]} > count)) && count=${#current_parts[@]}

    for ((i = 0; i < count; i++)); do
        latest_segment=$(normalize_segment "${latest_parts[i]:-0}")
        current_segment=$(normalize_segment "${current_parts[i]:-0}")
        if ((${#latest_segment} > ${#current_segment})); then
            return 0
        fi
        if ((${#latest_segment} < ${#current_segment})); then
            return 1
        fi
        if [[ $latest_segment > $current_segment ]]; then
            return 0
        fi
        if [[ $latest_segment < $current_segment ]]; then
            return 1
        fi
    done
    return 1
}

latest_plugin_version() {
    local game=$1 tag version latest=

    while IFS= read -r tag; do
        [[ -n $tag ]] || continue
        version=${tag#"plugin-$game-v"}
        if [[ -z $latest ]] || version_is_newer "$version" "$latest"; then
            latest=$version
        fi
    done < <(git tag -l "plugin-$game-v*")
    printf '%s' "$latest"
}

validate_newer_version() {
    local game=$1 version=$2 latest

    latest=$(latest_plugin_version "$game")
    if [[ -n $latest ]] && ! version_is_newer "$version" "$latest"; then
        fail "version $version must be newer than latest plugin-$game version $latest"
    fi
}

find_workflow_run() {
    local tag=$1 sha=$2 run_id attempt

    for ((attempt = 1; attempt <= 24; attempt++)); do
        run_id=$(gh run list \
            --workflow deploy-plugin.yml \
            --event push \
            --branch "$tag" \
            --json databaseId,headSha,status,conclusion,createdAt \
            --limit 10 \
            --jq ".[] | select(.headSha == \"$sha\") | .databaseId" | head -n 1)
        if [[ -n $run_id ]]; then
            printf '%s' "$run_id"
            return
        fi
        ((attempt < 24)) && sleep 5
    done
    fail "no deploy-plugin.yml push run found for $tag at $sha"
}

main() {
    (($# == 2)) || {
        usage
        exit 1
    }
    local game=$1 version=$2 branch head origin_head tag sha run_id run_url served_version

    branch=$(git branch --show-current)
    [[ $branch == main ]] || fail "must be on main (currently $branch)"
    [[ -z $(git status --porcelain) ]] || fail "working tree must be clean"
    git fetch origin main
    head=$(git rev-parse HEAD)
    origin_head=$(git rev-parse origin/main)
    [[ $head == "$origin_head" ]] || fail "HEAD must equal origin/main"
    validate_plugin "$game"
    validate_version "$version"
    validate_newer_version "$game" "$version"

    tag="plugin-$game-v$version"
    if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
        fail "tag already exists: $tag"
    fi

    sha=$head
    git tag "$tag"
    if ! git push origin "refs/tags/$tag"; then
        git tag -d "$tag" >/dev/null
        fail "push failed; local tag removed — retry"
    fi

    run_id=$(find_workflow_run "$tag" "$sha")
    run_url=$(gh run view "$run_id" --json url --jq .url)
    printf 'deploy run: %s\n' "$run_url"
    gh run watch "$run_id" --exit-status

    MODE=check SERVER_URL=https://api.savecraft.gg PLUGINS_DIR=plugins scripts/build-plugin-aggregate.sh
    served_version=$(curl -sS https://api.savecraft.gg/plugins/manifest.json | jq -r --arg g "$game" '.plugins[$g].version')
    printf 'released %s at %s; served version %s\n' "$tag" "$sha" "$served_version"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    main "$@"
fi
