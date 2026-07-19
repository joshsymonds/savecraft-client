#!/usr/bin/env bash
set -euo pipefail

readonly default_since="341b1650afb53023d2fff2dac3242eab88c58f3f"

usage() {
    echo "usage: $0 [--since <commit>] [repository-root]" >&2
}

die_usage() {
    echo "ERROR: $*" >&2
    usage
    exit 2
}

die_scan() {
    echo "ERROR: $*" >&2
    exit 2
}

since=$default_since
root_arg=.
since_seen=0
root_seen=0

while (($# > 0)); do
    case $1 in
        --since)
            ((since_seen == 0)) || die_usage "--since may be specified only once"
            (($# >= 2)) || die_usage "--since requires a commit"
            since=$2
            since_seen=1
            shift 2
            ;;
        -*)
            die_usage "unknown option: $1"
            ;;
        *)
            ((root_seen == 0)) || die_usage "unexpected argument: $1"
            root_arg=$1
            root_seen=1
            shift
            ;;
    esac
done

if ! repo=$(git -C "$root_arg" rev-parse --show-toplevel 2>/dev/null); then
    die_usage "not a Git working tree: $root_arg"
fi

if ! head=$(git -C "$repo" rev-parse --verify 'HEAD^{commit}' 2>/dev/null); then
    die_scan "HEAD does not resolve to a commit"
fi

if ! anchor=$(git -C "$repo" rev-parse --verify "${since}^{commit}" 2>/dev/null); then
    die_scan "--since does not resolve to a commit: $since"
fi

if git -C "$repo" merge-base --is-ancestor "$anchor" "$head"; then
    :
else
    status=$?
    if ((status == 1)); then
        die_scan "--since commit is not an ancestor of HEAD: $since"
    fi
    die_scan "Git failed while checking ancestry for: $since"
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
paths_file="$tmp_dir/paths"
index_file="$tmp_dir/index"
tree_file="$tmp_dir/tree"
commits_file="$tmp_dir/commits"
blob_file="$tmp_dir/blob"
link_file="$tmp_dir/link"

product="save""craft"
retired_identity="${product}-com""panion"
archived_identity="joshsymonds/${product}.""gg"
private_module="github.com/joshsymonds/${product}"
public_suffix="-client"
private_roots=("work""er" "si""te" "w""eb" "ev""als" "refer""ence" "sha""red" "n""ix")
private_pob="cmd/pob""-server"

violations=0

quote_path() {
    printf -v REPLY '%q' "$1"
}

report_violation() {
    local surface=$1
    local category=$2
    local path=$3
    quote_path "$path"
    printf 'VIOLATION surface=%s category=%s path=%s\n' "$surface" "$category" "$REPLY" >&2
    violations=$((violations + 1))
}

scan_path() {
    local surface=$1
    local path=$2
    local root

    for root in "${private_roots[@]}" "$private_pob"; do
        if [[ $path == "$root" || $path == "$root/"* ]]; then
            report_violation "$surface" "private-root" "$path"
            return
        fi
    done
}

contains_private_module() {
    local file=$1
    local line remainder after public_after clone_after
    local LC_ALL=C

    while IFS= read -r line || [[ -n $line ]]; do
        remainder=${line,,}
        while [[ $remainder == *"$private_module"* ]]; do
            after=${remainder#*"$private_module"}
            if [[ $after == "$public_suffix"* ]]; then
                public_after=${after#"$public_suffix"}
                if [[ $public_after == .git* ]]; then
                    clone_after=${public_after:4}
                    if [[ -z $clone_after ||
                        ${clone_after:0:1} != [[:alnum:]._~/%-] ]]; then
                        remainder=$clone_after
                        continue
                    fi
                elif [[ -z $public_after || $public_after == /* ||
                    ${public_after:0:1} != [[:alnum:]._~%-] ]]; then
                    remainder=$public_after
                    continue
                fi
            fi
            return 0
        done
    done <"$file"
    return 1
}

scan_fixed_identity() {
    local surface=$1
    local path=$2
    local file=$3
    local needle=$4
    local category=$5
    local status

    if LC_ALL=C grep -Fiq -- "$needle" "$file"; then
        report_violation "$surface" "$category" "$path"
        return
    else
        status=$?
    fi
    ((status == 1)) || die_scan "failed to scan content for $surface"
}

scan_content() {
    local surface=$1
    local path=$2
    local file=$3
    local status

    [[ -r $file ]] || die_scan "cannot read content for $surface"
    if LC_ALL=C grep -Iq . "$file"; then
        :
    else
        status=$?
        ((status == 1)) && return
        die_scan "failed to classify content for $surface"
    fi

    scan_fixed_identity "$surface" "$path" "$file" "$retired_identity" "retired-repository"
    scan_fixed_identity "$surface" "$path" "$file" "$archived_identity" "archived-repository"
    if contains_private_module "$file"; then
        report_violation "$surface" "private-module" "$path"
    fi
}

if ! git -C "$repo" ls-files -z >"$paths_file"; then
    die_scan "Git failed while listing tracked paths"
fi

while IFS= read -r -d '' path; do
    scan_path "working-tree" "$path"
    full_path="$repo/$path"
    if [[ -L $full_path ]]; then
        if ! readlink -- "$full_path" >"$link_file"; then
            die_scan "failed to read tracked symlink"
        fi
        scan_content "working-tree" "$path" "$link_file"
    elif [[ -f $full_path ]]; then
        scan_content "working-tree" "$path" "$full_path"
    fi
done <"$paths_file"

if ! git -C "$repo" ls-files --stage -z >"$index_file"; then
    die_scan "Git failed while listing index entries"
fi

while IFS= read -r -d '' entry; do
    [[ $entry == *$'\t'* ]] || die_scan "malformed index entry"
    metadata=${entry%%$'\t'*}
    path=${entry#*$'\t'}
    read -r mode object stage extra <<<"$metadata"
    [[ -n $mode && -n $object && -n $stage && -z ${extra-} ]] || die_scan "malformed index metadata"
    [[ $stage == 0 ]] || die_scan "unmerged index entry prevents a complete scan"
    scan_path "index" "$path"
    if [[ $mode != 160000 ]]; then
        if ! git -C "$repo" cat-file blob "$object" >"$blob_file"; then
            die_scan "Git failed while reading index object: $object"
        fi
        scan_content "index" "$path" "$blob_file"
    fi
done <"$index_file"

printf '%s\n' "$anchor" >"$commits_file"
if ! git -C "$repo" rev-list "${anchor}..${head}" >>"$commits_file"; then
    die_scan "Git failed while enumerating history"
fi

while IFS= read -r commit; do
    [[ $commit =~ ^[0-9a-fA-F]{40,64}$ ]] || die_scan "Git returned a malformed commit identifier"
    if ! git -C "$repo" ls-tree -r -z --full-tree "$commit" >"$tree_file"; then
        die_scan "Git failed while listing commit tree: $commit"
    fi
    while IFS= read -r -d '' entry; do
        [[ $entry == *$'\t'* ]] || die_scan "malformed tree entry in commit $commit"
        metadata=${entry%%$'\t'*}
        path=${entry#*$'\t'}
        read -r mode type object extra <<<"$metadata"
        [[ -n $mode && -n $type && -n $object && -z ${extra-} ]] \
            || die_scan "malformed tree metadata in commit $commit"
        surface="history commit=$commit"
        scan_path "$surface" "$path"
        if [[ $type == blob ]]; then
            if ! git -C "$repo" cat-file blob "$object" >"$blob_file"; then
                die_scan "Git failed while reading object $object from commit $commit"
            fi
            scan_content "$surface" "$path" "$blob_file"
        elif [[ $type != commit ]]; then
            die_scan "unexpected tree object type $type in commit $commit"
        fi
    done <"$tree_file"
done <"$commits_file"

if ((violations > 0)); then
    echo "FAIL: public boundary found $violations violation(s)" >&2
    exit 1
fi

echo "PASS: public boundary is clean from $anchor through $head"
