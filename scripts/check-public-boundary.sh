#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
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
link_file="$tmp_dir/link"
diff_file="$tmp_dir/diff"
batch_requests_file="$tmp_dir/batch-requests"
batch_output_dir="$tmp_dir/batch-output"
batch_error_file="$tmp_dir/batch-error"

declare -A blob_surface=()
declare -A blob_path=()

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

queue_blob() {
    local surface=$1
    local path=$2
    local object=$3
    local key

    [[ $object =~ ^[0-9a-fA-F]{40}$ || $object =~ ^[0-9a-fA-F]{64}$ ]] \
        || die_scan "malformed Git object identifier: $object"
    [[ -n ${object//0/} ]] || die_scan "zero Git object identifier for blob: $path"
    key=${object,,}
    if [[ -z ${blob_surface[$key]+seen} ]]; then
        blob_surface[$key]=$surface
        blob_path[$key]=$path
    fi
}

valid_object_id() {
    [[ $1 =~ ^[0-9a-fA-F]{40}$ || $1 =~ ^[0-9a-fA-F]{64}$ ]]
}

zero_object_id() {
    [[ -z ${1//0/} ]]
}

valid_tree_mode() {
    case $1 in
        000000 | 100644 | 100755 | 120000 | 160000) return 0 ;;
        *) return 1 ;;
    esac
}

valid_anchor_tree_mode() {
    case $1 in
        100644 | 100755 | 120000 | 160000) return 0 ;;
        *) return 1 ;;
    esac
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
        queue_blob "index" "$path" "$object"
    fi
done <"$index_file"

printf '%s\n' "$anchor" >"$commits_file"
if ! git -C "$repo" rev-list "${anchor}..${head}" >>"$commits_file"; then
    die_scan "Git failed while enumerating history"
fi

# Scan the anchor tree completely, then inspect only changed paths and objects
# in each reachable commit. This retains deleted paths and side-branch history
# without rescanning unchanged trees.
if ! git -C "$repo" ls-tree -r -z --full-tree "$anchor" >"$tree_file"; then
    die_scan "Git failed while listing anchor tree: $anchor"
fi
while IFS= read -r -d '' entry; do
    [[ $entry == *$'\t'* ]] || die_scan "malformed tree entry in anchor commit"
    metadata=${entry%%$'\t'*}
    path=${entry#*$'\t'}
    read -r mode type object extra <<<"$metadata"
    [[ -n $mode && -n $type && -n $object && -z ${extra-} ]] \
        || die_scan "malformed tree mode/type/object metadata in anchor commit"
    [[ -n $path ]] || die_scan "malformed tree path in anchor commit"
    valid_anchor_tree_mode "$mode" \
        || die_scan "malformed tree mode/type/object metadata in anchor commit"
    valid_object_id "$object" \
        || die_scan "malformed tree mode/type/object metadata in anchor commit"
    scan_path "history commit=$anchor" "$path"
    if [[ $type == blob && $mode != 160000 ]]; then
        zero_object_id "$object" \
            && die_scan "malformed tree mode/type/object metadata in anchor commit"
        queue_blob "history commit=$anchor" "$path" "$object"
    elif [[ $type == commit && $mode == 160000 ]]; then
        zero_object_id "$object" \
            && die_scan "malformed tree mode/type/object metadata in anchor commit"
    else
        die_scan "malformed tree mode/type/object metadata in anchor commit"
    fi
done <"$tree_file"

while IFS= read -r commit; do
    [[ $commit =~ ^[0-9a-fA-F]{40}$ || $commit =~ ^[0-9a-fA-F]{64}$ ]] \
        || die_scan "Git returned a malformed commit identifier"
    [[ $commit == "$anchor" ]] && continue
    if ! git -C "$repo" diff-tree --root -r -m --no-renames -z --no-commit-id "$commit" -- >"$diff_file"; then
        die_scan "Git failed while listing changed paths: $commit"
    fi
    exec 4<"$diff_file"
    while IFS= read -r -d '' metadata <&4; do
        IFS= read -r -d '' path <&4 || die_scan "malformed diff path in commit $commit"
        [[ ${metadata:0:1} == : ]] || die_scan "malformed diff metadata in commit $commit"
        read -r old_mode new_mode old_object new_object status extra <<<"${metadata:1}"
        [[ -n $old_mode && -n $new_mode && -n $old_object && -n $new_object &&
            -n $status && -z ${extra-} ]] \
            || die_scan "malformed diff metadata in commit $commit"
        [[ -n $path ]] || die_scan "malformed diff path in commit $commit"
        if ! valid_tree_mode "$old_mode" || ! valid_tree_mode "$new_mode"; then
            die_scan "malformed diff mode in commit $commit"
        fi
        valid_object_id "$old_object" || die_scan "malformed old object in commit $commit"
        valid_object_id "$new_object" || die_scan "malformed new object in commit $commit"
        [[ ${#old_object} -eq ${#new_object} ]] \
            || die_scan "mismatched object identifier width in commit $commit"
        old_null=0
        new_null=0
        zero_object_id "$old_object" && old_null=1
        zero_object_id "$new_object" && new_null=1
        (((old_mode == 0 && old_null == 1) || (old_mode != 0 && old_null == 0))) \
            || die_scan "inconsistent old null object in commit $commit"
        (((new_mode == 0 && new_null == 1) || (new_mode != 0 && new_null == 0))) \
            || die_scan "inconsistent new null object in commit $commit"
        case $status in
            A) ((old_null == 1 && new_null == 0)) || die_scan "invalid add status in commit $commit" ;;
            D) ((old_null == 0 && new_null == 1)) || die_scan "invalid delete status in commit $commit" ;;
            M | T | U | X | B) ((old_null == 0 && new_null == 0)) || die_scan "invalid $status status in commit $commit" ;;
            *) die_scan "unsupported diff status in commit $commit: $status" ;;
        esac
        surface="history commit=$commit"
        scan_path "$surface" "$path"
        if [[ $old_mode != 000000 && $old_mode != 160000 ]]; then
            queue_blob "$surface" "$path" "$old_object"
        fi
        if [[ $new_mode != 000000 && $new_mode != 160000 ]]; then
            queue_blob "$surface" "$path" "$new_object"
        fi
    done
    exec 4<&-
done <"$commits_file"

if ((${#blob_surface[@]} > 0)); then
    printf '%s\n' "${!blob_surface[@]}" >"$batch_requests_file"
    if ! node "$script_dir/read-git-batch.mjs" "$repo" "$batch_requests_file" \
        "$batch_output_dir" 2>"$batch_error_file"; then
        batch_error=$(<"$batch_error_file")
        [[ -n $batch_error ]] || batch_error="Git failed while reading batch objects"
        die_scan "$batch_error"
    fi
    while IFS= read -r object; do
        key=${object,,}
        blob_file="$batch_output_dir/$key"
        scan_content "${blob_surface[$key]}" "${blob_path[$key]}" "$blob_file"
    done <"$batch_requests_file"
fi

if ((violations > 0)); then
    echo "FAIL: public boundary found $violations violation(s)" >&2
    exit 1
fi

echo "PASS: public boundary is clean from $anchor through $head"
