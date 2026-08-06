#!/usr/bin/env bash
set -euo pipefail

# This suite builds scratch git repos in temp dirs. Git hooks (pre-push)
# export a relative GIT_DIR=.git, which after any cd makes every git call
# here — including the scratch `git init` — target the invoking repo
# instead of the scratch one. Sanitize so the suite behaves identically
# whether run directly or from a hook.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
checker="${script_dir}/check-public-boundary.sh"
helper="${script_dir}/read-git-batch.mjs"

product="save""craft"
retired_identity="${product}-com""panion"
archived_identity="joshsymonds/${product}.""gg"
private_module="github.com/joshsymonds/${product}"
public_module="${private_module}-client"
malicious_module="${public_module}-malicious"
public_clone="${public_module}.g""it"
malicious_git_module="${public_clone}-malicious"
mixed_owner="Josh""Symonds"
mixed_product="Save""Craft"
mixed_private_module="github.com/${mixed_owner}/${mixed_product}"
mixed_public_module="${mixed_private_module}-Client"
mixed_public_clone="${mixed_public_module}.G""iT"
mixed_retired_identity="${mixed_product}-Com""panion"
mixed_archived_identity="${mixed_owner}/${mixed_product}.""GG"
private_root="work""er"
private_site="si""te"
private_pob="cmd/pob""-server"
private_roots=(
    "$private_root"
    "$private_site"
    "w""eb"
    "ev""als"
    "refer""ence"
    "sha""red"
    "n""ix"
    "$private_pob"
)

if [[ ! -x "$checker" ]]; then
    echo "FAIL: boundary checker is missing or not executable: $checker" >&2
    exit 1
fi

tmp_root=$(mktemp -d)
trap 'rm -rf "$tmp_root"' EXIT
real_git=$(command -v git)

tests_run=0

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    tests_run=$((tests_run + 1))
    echo "PASS: $1"
}

new_repo() {
    local name=$1
    REPLY="${tmp_root}/${name}"
    mkdir -p "$REPLY"
    git -C "$REPLY" init -q -b main
    git -C "$REPLY" config user.name "Boundary Fixture"
    git -C "$REPLY" config user.email "boundary-fixture@example.invalid"
}

commit_all() {
    local repo=$1
    local message=$2
    git -C "$repo" add -A
    git -C "$repo" commit -q -m "$message"
}

run_check() {
    local repo=$1
    local since=$2
    set +e
    if [[ -n ${CHECK_GIT_PATH-} ]]; then
        CHECK_OUTPUT=$(
            BOUNDARY_REAL_GIT="$real_git" BOUNDARY_CAT_FILE="${BOUNDARY_CAT_FILE-}" \
                BOUNDARY_CAT_REQUESTS="${BOUNDARY_CAT_REQUESTS-}" \
                BOUNDARY_FAIL_REV_LIST="${BOUNDARY_FAIL_REV_LIST-}" \
                BOUNDARY_MALFORM_LS_TREE="${BOUNDARY_MALFORM_LS_TREE-}" \
                BOUNDARY_MALFORM_DIFF_TREE="${BOUNDARY_MALFORM_DIFF_TREE-}" \
                BOUNDARY_LS_TREE_OBJECT="${BOUNDARY_LS_TREE_OBJECT-}" \
                BOUNDARY_MALFORM_BATCH="${BOUNDARY_MALFORM_BATCH-}" \
                BOUNDARY_CHUNK_LOG="${BOUNDARY_CHUNK_LOG-}" \
                PATH="$CHECK_GIT_PATH" \
                bash "$checker" --since "$since" "$repo" 2>&1
        )
    else
        CHECK_OUTPUT=$(bash "$checker" --since "$since" "$repo" 2>&1)
    fi
    CHECK_STATUS=$?
    set -e
}

expect_clean() {
    local name=$1
    local repo=$2
    local since=$3
    run_check "$repo" "$since"
    if ((CHECK_STATUS != 0)); then
        fail "$name: expected success, got status $CHECK_STATUS: $CHECK_OUTPUT"
    fi
    [[ "$CHECK_OUTPUT" == *"PASS:"* ]] || fail "$name: missing PASS diagnostic: $CHECK_OUTPUT"
    pass "$name"
}

expect_violation() {
    local name=$1
    local repo=$2
    local since=$3
    local category=$4
    local path_fragment=${5-}
    run_check "$repo" "$since"
    if ((CHECK_STATUS != 1)); then
        fail "$name: expected violation status 1, got $CHECK_STATUS: $CHECK_OUTPUT"
    fi
    [[ "$CHECK_OUTPUT" == *"category=${category}"* ]] \
        || fail "$name: missing category ${category}: $CHECK_OUTPUT"
    if [[ -n "$path_fragment" && "$CHECK_OUTPUT" != *"$path_fragment"* ]]; then
        fail "$name: missing path fragment ${path_fragment}: $CHECK_OUTPUT"
    fi
    pass "$name"
}

expect_closed() {
    local name=$1
    local repo=$2
    local since=$3
    local diagnostic=$4
    run_check "$repo" "$since"
    if ((CHECK_STATUS != 2)); then
        fail "$name: expected closed status 2, got $CHECK_STATUS: $CHECK_OUTPUT"
    fi
    [[ "$CHECK_OUTPUT" == *"ERROR: ${diagnostic}"* ]] \
        || fail "$name: missing ERROR diagnostic ${diagnostic}: $CHECK_OUTPUT"
    pass "$name"
}

expect_helper_closed() {
    local name=$1
    local mode=$2
    local diagnostic=$3
    local object=1111111111111111111111111111111111111111
    local requests="${tmp_root}/helper-${mode}.requests"
    local output="${tmp_root}/helper-${mode}.output"
    printf '%s\n' "$object" >"$requests"
    mkdir -p "$output"
    set +e
    CHECK_OUTPUT=$(
        BOUNDARY_MALFORM_BATCH="$mode" BOUNDARY_REAL_GIT="$real_git" \
            PATH="$git_wrapper_dir:${PATH}" timeout 10s \
            node "$helper" "$batch_probe_repo" "$requests" "$output" 2>&1
    )
    CHECK_STATUS=$?
    set -e
    ((CHECK_STATUS == 1)) || fail "$name: expected helper status 1, got $CHECK_STATUS: $CHECK_OUTPUT"
    [[ "$CHECK_OUTPUT" == *"$diagnostic"* ]] \
        || fail "$name: missing helper diagnostic ${diagnostic}: $CHECK_OUTPUT"
    [[ "$CHECK_OUTPUT" != *"Error:"* && "$CHECK_OUTPUT" != *"Unhandled"* ]] \
        || fail "$name: helper emitted an unhandled stack/rejection: $CHECK_OUTPUT"
    pass "$name"
}

# Clean public surfaces, hosted product domains, and the installer edge Worker are allowed.
new_repo clean
clean_repo=$REPLY
mkdir -p "$clean_repo/install/${private_root}"
printf '%s\n' \
    "module ${public_module}" \
    "repository https://${public_module}" \
    "service https://api.${product}.gg" \
    "support help@${product}.gg" >"$clean_repo/README.md"
printf '%s\n' "export default { host: 'install.${product}.gg' };" \
    >"$clean_repo/install/${private_root}/index.js"
commit_all "$clean_repo" "clean anchor"
clean_anchor=$(git -C "$clean_repo" rev-parse HEAD)
printf '%s\n' "public client remains clean" >"$clean_repo/client.txt"
commit_all "$clean_repo" "clean current tree"
expect_clean "clean public tree" "$clean_repo" "$clean_anchor"

# A private surface before a clean split anchor is intentionally out of range.
new_repo pre_anchor
pre_repo=$REPLY
mkdir -p "$pre_repo/$private_root"
printf '%s\n' "$retired_identity" >"$pre_repo/$private_root/legacy.txt"
commit_all "$pre_repo" "legacy private tree"
git -C "$pre_repo" rm -q -r "$private_root"
printf '%s\n' "clean split" >"$pre_repo/README.md"
commit_all "$pre_repo" "clean anchor"
pre_anchor=$(git -C "$pre_repo" rev-parse HEAD)
printf '%s\n' "later public change" >>"$pre_repo/README.md"
commit_all "$pre_repo" "clean current tree"
expect_clean "pre-anchor private content" "$pre_repo" "$pre_anchor"

# The anchor tree itself is in scope.
new_repo dirty_anchor
dirty_anchor_repo=$REPLY
mkdir -p "$dirty_anchor_repo/$private_site"
printf '%s\n' "secret" >"$dirty_anchor_repo/$private_site/index.txt"
commit_all "$dirty_anchor_repo" "dirty anchor"
dirty_anchor=$(git -C "$dirty_anchor_repo" rev-parse HEAD)
expect_violation "private root at anchor" "$dirty_anchor_repo" "$dirty_anchor" \
    "private-root" "${private_site}/index.txt"

# Every exact and nested prohibited top-level root is rejected.
root_case=0
for root in "${private_roots[@]}"; do
    root_case=$((root_case + 1))

    new_repo "private_root_${root_case}_exact"
    root_repo=$REPLY
    mkdir -p "$(dirname -- "$root_repo/$root")"
    printf '%s\n' "secret" >"$root_repo/$root"
    commit_all "$root_repo" "exact private root"
    root_anchor=$(git -C "$root_repo" rev-parse HEAD)
    expect_violation "exact private root ${root}" "$root_repo" "$root_anchor" \
        "private-root" "$root"

    new_repo "private_root_${root_case}_nested"
    root_repo=$REPLY
    mkdir -p "$root_repo/$root"
    printf '%s\n' "secret" >"$root_repo/$root/secret.txt"
    commit_all "$root_repo" "nested private root"
    root_anchor=$(git -C "$root_repo" rev-parse HEAD)
    expect_violation "nested private root ${root}" "$root_repo" "$root_anchor" \
        "private-root" "${root}/secret.txt"
done

# Unstaged tracked content must be scanned independently from the index.
new_repo unstaged
unstaged_repo=$REPLY
printf '%s\n' "clean" >"$unstaged_repo/notes.txt"
commit_all "$unstaged_repo" "clean anchor"
unstaged_anchor=$(git -C "$unstaged_repo" rev-parse HEAD)
printf '%s\n' "$retired_identity" >"$unstaged_repo/notes.txt"
expect_violation "unstaged retired identity" "$unstaged_repo" "$unstaged_anchor" \
    "retired-repository" "notes.txt"

# Every prohibited repository identity gets an explicit content assertion.
new_repo archived
archived_repo=$REPLY
printf '%s\n' "clean" >"$archived_repo/notes.txt"
commit_all "$archived_repo" "clean anchor"
archived_anchor=$(git -C "$archived_repo" rev-parse HEAD)
printf '%s\n' "https://api.github.com/repos/${archived_identity}" >"$archived_repo/notes.txt"
expect_violation "archived repository identity" "$archived_repo" "$archived_anchor" \
    "archived-repository" "notes.txt"

new_repo canonical
canonical_repo=$REPLY
printf '%s\n' "clean" >"$canonical_repo/notes.txt"
commit_all "$canonical_repo" "clean anchor"
canonical_anchor=$(git -C "$canonical_repo" rev-parse HEAD)
printf '%s\n' "module ${private_module}" >"$canonical_repo/notes.txt"
expect_violation "canonical private module" "$canonical_repo" "$canonical_anchor" \
    "private-module" "notes.txt"

# The exact public module and its Go subpackages are intentional public identities.
new_repo public_module_tokens
public_tokens_repo=$REPLY
printf '%s\n' "clean" >"$public_tokens_repo/notes.txt"
commit_all "$public_tokens_repo" "clean anchor"
public_tokens_anchor=$(git -C "$public_tokens_repo" rev-parse HEAD)
printf '%s\n' \
    "module ${public_module}" \
    "import ${public_module}/pkg/client" \
    "module ${mixed_public_module}" \
    "import ${mixed_public_module}/pkg/client" >"$public_tokens_repo/notes.txt"
expect_clean "exact public module tokens" "$public_tokens_repo" "$public_tokens_anchor"

# Exact public clone URLs allow case-insensitive .git suffixes and URL delimiters.
new_repo public_clone
public_clone_repo=$REPLY
printf '%s\n' "clean" >"$public_clone_repo/notes.txt"
commit_all "$public_clone_repo" "clean anchor"
public_clone_anchor=$(git -C "$public_clone_repo" rev-parse HEAD)
printf '%s\n' \
    "repository https://${public_clone}" \
    "repository https://${public_clone}?ref=main" \
    "repository https://${public_clone}#readme" >"$public_clone_repo/notes.txt"
expect_clean "lowercase public .git clone URL" "$public_clone_repo" "$public_clone_anchor"

new_repo mixed_public_clone
mixed_public_clone_repo=$REPLY
printf '%s\n' "clean" >"$mixed_public_clone_repo/notes.txt"
commit_all "$mixed_public_clone_repo" "clean anchor"
mixed_public_clone_anchor=$(git -C "$mixed_public_clone_repo" rev-parse HEAD)
printf '%s\n' "repository https://${mixed_public_clone}" >"$mixed_public_clone_repo/notes.txt"
expect_clean "mixed-case public .git clone URL" "$mixed_public_clone_repo" \
    "$mixed_public_clone_anchor"

# GitHub owner and repository matching is case-insensitive.
new_repo mixed_archived
mixed_archived_repo=$REPLY
printf '%s\n' "clean" >"$mixed_archived_repo/notes.txt"
commit_all "$mixed_archived_repo" "clean anchor"
mixed_archived_anchor=$(git -C "$mixed_archived_repo" rev-parse HEAD)
printf '%s\n' "repository ${mixed_archived_identity}" >"$mixed_archived_repo/notes.txt"
expect_violation "mixed-case archived repository identity" "$mixed_archived_repo" \
    "$mixed_archived_anchor" "archived-repository" "notes.txt"

new_repo mixed_retired
mixed_retired_repo=$REPLY
printf '%s\n' "clean" >"$mixed_retired_repo/notes.txt"
commit_all "$mixed_retired_repo" "clean anchor"
mixed_retired_anchor=$(git -C "$mixed_retired_repo" rev-parse HEAD)
printf '%s\n' "repository ${mixed_retired_identity}" >"$mixed_retired_repo/notes.txt"
expect_violation "mixed-case retired repository identity" "$mixed_retired_repo" \
    "$mixed_retired_anchor" "retired-repository" "notes.txt"

new_repo mixed_canonical
mixed_canonical_repo=$REPLY
printf '%s\n' "clean" >"$mixed_canonical_repo/notes.txt"
commit_all "$mixed_canonical_repo" "clean anchor"
mixed_canonical_anchor=$(git -C "$mixed_canonical_repo" rev-parse HEAD)
printf '%s\n' "module ${mixed_private_module}" >"$mixed_canonical_repo/notes.txt"
expect_violation "mixed-case canonical private module" "$mixed_canonical_repo" \
    "$mixed_canonical_anchor" "private-module" "notes.txt"

# A repository name that merely extends the public identity remains private.
new_repo malicious_continuation
malicious_repo=$REPLY
printf '%s\n' "clean" >"$malicious_repo/notes.txt"
commit_all "$malicious_repo" "clean anchor"
malicious_anchor=$(git -C "$malicious_repo" rev-parse HEAD)
printf '%s\n' "module ${malicious_module}" >"$malicious_repo/notes.txt"
expect_violation "public-name malicious continuation" "$malicious_repo" "$malicious_anchor" \
    "private-module" "notes.txt"

new_repo encoded_dot_git_continuation
encoded_dot_git_repo=$REPLY
printf '%s\n' "clean" >"$encoded_dot_git_repo/notes.txt"
commit_all "$encoded_dot_git_repo" "clean anchor"
encoded_dot_git_anchor=$(git -C "$encoded_dot_git_repo" rev-parse HEAD)
printf '%s\n' "module ${public_module}%2Egit-malicious" \
    >"$encoded_dot_git_repo/notes.txt"
expect_violation "percent-encoded dot-git continuation" "$encoded_dot_git_repo" \
    "$encoded_dot_git_anchor" "private-module" "notes.txt"

new_repo encoded_dash_continuation
encoded_dash_repo=$REPLY
printf '%s\n' "clean" >"$encoded_dash_repo/notes.txt"
commit_all "$encoded_dash_repo" "clean anchor"
encoded_dash_anchor=$(git -C "$encoded_dash_repo" rev-parse HEAD)
printf '%s\n' "module ${public_module}%2Dmalicious" >"$encoded_dash_repo/notes.txt"
expect_violation "percent-encoded dash continuation" "$encoded_dash_repo" \
    "$encoded_dash_anchor" "private-module" "notes.txt"

new_repo malicious_git_continuation
malicious_git_repo=$REPLY
printf '%s\n' "clean" >"$malicious_git_repo/notes.txt"
commit_all "$malicious_git_repo" "clean anchor"
malicious_git_anchor=$(git -C "$malicious_git_repo" rev-parse HEAD)
printf '%s\n' "repository https://${malicious_git_module}" >"$malicious_git_repo/notes.txt"
expect_violation "public .git malicious continuation" "$malicious_git_repo" \
    "$malicious_git_anchor" "private-module" "notes.txt"

new_repo encoded_git_dash_continuation
encoded_git_dash_repo=$REPLY
printf '%s\n' "clean" >"$encoded_git_dash_repo/notes.txt"
commit_all "$encoded_git_dash_repo" "clean anchor"
encoded_git_dash_anchor=$(git -C "$encoded_git_dash_repo" rev-parse HEAD)
printf '%s\n' "repository https://${public_clone}%2Dmalicious" \
    >"$encoded_git_dash_repo/notes.txt"
expect_violation "public .git percent-encoded dash continuation" \
    "$encoded_git_dash_repo" "$encoded_git_dash_anchor" "private-module" "notes.txt"

# Index content must remain visible after the working copy is restored.
new_repo staged_content
staged_repo=$REPLY
printf '%s\n' "clean" >"$staged_repo/notes.txt"
commit_all "$staged_repo" "clean anchor"
staged_anchor=$(git -C "$staged_repo" rev-parse HEAD)
printf '%s\n' "$retired_identity" >"$staged_repo/notes.txt"
git -C "$staged_repo" add notes.txt
git -C "$staged_repo" show HEAD:notes.txt >"$staged_repo/notes.txt"
expect_violation "staged content with clean worktree" "$staged_repo" "$staged_anchor" \
    "retired-repository" "notes.txt"
[[ $(<"$staged_repo/notes.txt") == "clean" ]] || fail "working copy was not restored"

# A new top-level private path exists only in the index before commit.
new_repo staged_path
staged_path_repo=$REPLY
printf '%s\n' "clean" >"$staged_path_repo/README.md"
commit_all "$staged_path_repo" "clean anchor"
staged_path_anchor=$(git -C "$staged_path_repo" rev-parse HEAD)
mkdir -p "$staged_path_repo/$private_root"
printf '%s\n' "new staged file" >"$staged_path_repo/$private_root/new.txt"
git -C "$staged_path_repo" add "$private_root/new.txt"
expect_violation "staged private root" "$staged_path_repo" "$staged_path_anchor" \
    "private-root" "${private_root}/new.txt"

# Deleted post-anchor paths remain violations because complete trees are scanned.
new_repo deleted_path
deleted_path_repo=$REPLY
printf '%s\n' "clean" >"$deleted_path_repo/README.md"
commit_all "$deleted_path_repo" "clean anchor"
deleted_path_anchor=$(git -C "$deleted_path_repo" rev-parse HEAD)
mkdir -p "$deleted_path_repo/$private_pob"
printf '%s\n' "secret" >"$deleted_path_repo/$private_pob/main.go"
commit_all "$deleted_path_repo" "introduce private path"
git -C "$deleted_path_repo" rm -q -r "$private_pob"
commit_all "$deleted_path_repo" "delete private path"
expect_violation "deleted historical private root" "$deleted_path_repo" "$deleted_path_anchor" \
    "private-root" "${private_pob}/main.go"

# Deleted post-anchor content remains visible in its introducing commit.
new_repo deleted_identity
deleted_identity_repo=$REPLY
printf '%s\n' "clean" >"$deleted_identity_repo/notes.txt"
commit_all "$deleted_identity_repo" "clean anchor"
deleted_identity_anchor=$(git -C "$deleted_identity_repo" rev-parse HEAD)
printf '%s\n' "$archived_identity" >"$deleted_identity_repo/notes.txt"
commit_all "$deleted_identity_repo" "introduce private identity"
printf '%s\n' "clean again" >"$deleted_identity_repo/notes.txt"
commit_all "$deleted_identity_repo" "delete private identity"
expect_violation "deleted historical private identity" "$deleted_identity_repo" \
    "$deleted_identity_anchor" "archived-repository" "notes.txt"

# A cleaned no-fast-forward merge still makes the violating side commit reachable.
new_repo side_branch
side_repo=$REPLY
printf '%s\n' "clean" >"$side_repo/README.md"
commit_all "$side_repo" "clean anchor"
side_anchor=$(git -C "$side_repo" rev-parse HEAD)
git -C "$side_repo" checkout -q -b private-side
mkdir -p "$side_repo/$private_site"
printf '%s\n' "side secret" >"$side_repo/$private_site/secret.txt"
commit_all "$side_repo" "side violation"
git -C "$side_repo" checkout -q main
printf '%s\n' "main progress" >"$side_repo/main.txt"
commit_all "$side_repo" "main progress"
git -C "$side_repo" merge -q --no-ff --no-commit private-side
rm "$side_repo/$private_site/secret.txt"
rmdir "$side_repo/$private_site"
git -C "$side_repo" add -A
git -C "$side_repo" commit -q -m "clean merge"
expect_violation "reachable side-branch violation" "$side_repo" "$side_anchor" \
    "private-root" "${private_site}/secret.txt"

# Bad anchors must fail closed.
new_repo bad_anchor
bad_anchor_repo=$REPLY
printf '%s\n' "clean" >"$bad_anchor_repo/README.md"
commit_all "$bad_anchor_repo" "clean head"
expect_closed "unresolvable anchor" "$bad_anchor_repo" "missing-anchor" \
    "--since does not resolve to a commit: missing-anchor"
empty_tree=$(git -C "$bad_anchor_repo" hash-object -t tree /dev/null)
unrelated=$(printf '%s\n' "unrelated" | git -C "$bad_anchor_repo" commit-tree "$empty_tree")
expect_closed "non-ancestral anchor" "$bad_anchor_repo" "$unrelated" \
    "--since commit is not an ancestor of HEAD: ${unrelated}"

# Unexpected Git failures are distinguished from boundary violations and fail closed.
git_wrapper_dir="$tmp_root/git-wrapper"
mkdir -p "$git_wrapper_dir"
cat >"$git_wrapper_dir/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
args=("$@")
command_index=0
while ((command_index < ${#args[@]})); do
    case ${args[command_index]} in
    -C|-c|--git-dir|--work-tree|--namespace)
        command_index=$((command_index + 2))
        ;;
    --*)
        command_index=$((command_index + 1))
        ;;
    *)
        break
        ;;
    esac
done
if [[ ${args[command_index]-} == cat-file && -n ${BOUNDARY_CAT_FILE-} ]]; then
    printf "%s\n" "${args[*]}" >>"$BOUNDARY_CAT_FILE"
fi
if [[ ${args[command_index]-} == cat-file && -n ${BOUNDARY_CAT_REQUESTS-} ]]; then
    tee -a "$BOUNDARY_CAT_REQUESTS" | "$BOUNDARY_REAL_GIT" "$@"
    exit ${PIPESTATUS[1]}
fi
if [[ ${args[command_index]-} == ls-tree && -n ${BOUNDARY_MALFORM_LS_TREE-} ]]; then
    case $BOUNDARY_MALFORM_LS_TREE in
    empty-path) printf "%b" "100644 blob 0000000000000000000000000000000000000000\t\0" ;;
    zero-mode) printf "%b" "000000 blob ${BOUNDARY_LS_TREE_OBJECT}\tREADME.md\0" ;;
    *) printf "%b" "100644 commit 0000000000000000000000000000000000000000\tREADME.md\0" ;;
    esac
    exit 0
fi
if [[ ${args[command_index]-} == cat-file && -n ${BOUNDARY_MALFORM_BATCH-} ]]; then
    if [[ $BOUNDARY_MALFORM_BATCH == early-parser-failure-backpressure ]]; then
        printf '%s\n' 'not-a-batch-header'
        exec sleep 60
    fi
    if [[ $BOUNDARY_MALFORM_BATCH == nonzero-exit ]]; then
        "$BOUNDARY_REAL_GIT" "$@"
        exit 73
    fi
    while IFS= read -r object; do
        case $BOUNDARY_MALFORM_BATCH in
        malformed-header) printf '%s\n' 'not-a-batch-header' ;;
        malformed-object) printf '%s blob 1 extra\nx\n' "$object" ;;
        mismatched-object)
            other=$(printf '%*s' "${#object}" '' | tr ' ' 2)
            [[ $other == "$object" ]] && other=$(printf '%*s' "${#object}" '' | tr ' ' 3)
            printf '%s blob 0\n\n' "$other"
            ;;
        non-blob) printf '%s commit 0\n\n' "$object" ;;
        invalid-size) printf '%s blob nope\n' "$object" ;;
        overflow-size) printf '%s blob 18446744073709551616\n\n' "$object" ;;
        nul-header) printf '%s\0 blob 0\n\n' "$object" ;;
        truncated-body) printf '%s blob 4\nab\n' "$object" ;;
        bad-terminator) printf '%s blob 2\nabX' "$object" ;;
        nul-terminator) printf '%s blob 0\n\0\n' "$object" ;;
        extra-output)
            printf '%s blob 0\n\nX' "$object"
            exec sleep 60
            ;;
        esac
        cat >/dev/null
        break
    done
    exit 0
fi
if [[ ${args[command_index]-} == diff-tree && -n ${BOUNDARY_MALFORM_DIFF_TREE-} ]]; then
    zero_object=$(printf "%040d" 0)
    case $BOUNDARY_MALFORM_DIFF_TREE in
    null-object) printf "%b" ":100644 100644 0000000000000000000000000000000000000000 0000000000000000000000000000000000000000 M\0README.md\0" ;;
    empty-path) printf "%b" ":000000 000000 0000000000000000000000000000000000000000 0000000000000000000000000000000000000000 A\0\0" ;;
    unsupported-status) printf "%b" ":000000 000000 $zero_object $zero_object R100\0README.md\0" ;;
    *) printf "%b" "100644 100644 0000000000000000000000000000000000000000 0000000000000000000000000000000000000000 M\0README.md\0" ;;
    esac
    exit 0
fi
if [[ ${args[command_index]-} == rev-list && -n ${BOUNDARY_FAIL_REV_LIST-} ]]; then
    echo "forced rev-list failure" >&2
    exit 73
fi
exec "$BOUNDARY_REAL_GIT" "$@"
EOF
chmod +x "$git_wrapper_dir/git"
BOUNDARY_FAIL_REV_LIST=1 CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "rev-list failure" "$bad_anchor_repo" "HEAD" "Git failed while enumerating history"

# Successful Git output must still be validated.  A malformed ls-tree record
# cannot be treated as a harmless empty tree entry, and a raw diff-tree record
# must carry its required leading colon.
BOUNDARY_MALFORM_LS_TREE=1 CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "malformed successful ls-tree output" "$clean_repo" "$clean_anchor" \
    "malformed tree mode/type/object metadata in anchor commit"
zero_mode_oid=$(git -C "$clean_repo" rev-parse "$clean_anchor:README.md")
BOUNDARY_MALFORM_LS_TREE=zero-mode BOUNDARY_LS_TREE_OBJECT="$zero_mode_oid" \
    CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "zero mode successful ls-tree output" "$clean_repo" "$clean_anchor" \
    "malformed tree mode/type/object metadata in anchor commit"
BOUNDARY_MALFORM_DIFF_TREE=1 CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "malformed successful diff-tree output" "$clean_repo" "$clean_anchor" \
    "malformed diff metadata in commit"
BOUNDARY_MALFORM_LS_TREE=empty-path CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "empty successful ls-tree path" "$clean_repo" "$clean_anchor" \
    "malformed tree path in anchor commit"
BOUNDARY_MALFORM_DIFF_TREE=null-object CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "nonzero-mode null diff object" "$clean_repo" "$clean_anchor" \
    "inconsistent old null object in commit"
BOUNDARY_MALFORM_DIFF_TREE=empty-path CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "empty successful diff-tree path" "$clean_repo" "$clean_anchor" \
    "malformed diff path in commit"
BOUNDARY_MALFORM_DIFF_TREE=unsupported-status CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "unsupported diff status" "$clean_repo" "$clean_anchor" \
    "unsupported diff status in commit"

# Quoted diagnostics preserve spaces and embedded newlines in NUL-delimited paths.
new_repo unusual_paths
unusual_repo=$REPLY
printf '%s\n' "clean" >"$unusual_repo/README.md"
commit_all "$unusual_repo" "clean anchor"
unusual_anchor=$(git -C "$unusual_repo" rev-parse HEAD)
mkdir -p "$unusual_repo/$private_root"
space_path="${private_root}/secret file.txt"
printf '%s\n' "secret" >"$unusual_repo/$space_path"
git -C "$unusual_repo" add "$space_path"
expect_violation "path with spaces" "$unusual_repo" "$unusual_anchor" \
    "private-root" "${private_root}/secret\\ file.txt"
git -C "$unusual_repo" reset -q --hard HEAD
mkdir -p "$unusual_repo/$private_root"
newline_path="${private_root}/line"$'\n'"break.txt"
printf '%s\n' "secret" >"$unusual_repo/$newline_path"
git -C "$unusual_repo" add "$newline_path"
expect_violation "path with newline" "$unusual_repo" "$unusual_anchor" \
    "private-root" "\\nbreak.txt"

# Ignored/untracked files and tracked binary blobs are outside text-content scans.
new_repo exclusions
exclusions_repo=$REPLY
printf '%s\n' "ignored/" >"$exclusions_repo/.gitignore"
printf '%s\0%s\n' "$retired_identity" "$archived_identity" >"$exclusions_repo/binary.dat"
printf '%s\n' "clean" >"$exclusions_repo/README.md"
commit_all "$exclusions_repo" "clean anchor with binary"
exclusions_anchor=$(git -C "$exclusions_repo" rev-parse HEAD)
mkdir -p "$exclusions_repo/ignored"
printf '%s\n' "$retired_identity" >"$exclusions_repo/ignored/private.txt"
printf '%s\n' "$private_module" >"$exclusions_repo/untracked.txt"
expect_clean "ignored untracked and binary content" "$exclusions_repo" "$exclusions_anchor"

# Enforcement sources cannot carry the literals they are designed to reject.
for needle in "$retired_identity" "$archived_identity" "$private_module"; do
    if grep -Fq -- "$needle" "$checker" "${BASH_SOURCE[0]}"; then
        fail "enforcement source contains a contiguous prohibited fixture literal"
    fi
done
pass "enforcement source literals are fragmented"

# The shared gate must run fixtures before the live repository scan, and check must include it.
justfile="${script_dir}/../Justfile"
boundary_recipe=$(awk '
    /^public-boundary:$/ { found = 1; next }
    found && /^[^[:space:]#].*:$/ { exit }
    found { print }
' "$justfile")
[[ "$boundary_recipe" == *"bash scripts/check-public-boundary.test.sh"* ]] \
    || fail "public-boundary recipe does not run fixtures"
[[ "$boundary_recipe" == *"bash scripts/check-public-boundary.sh"* ]] \
    || fail "public-boundary recipe does not run the live checker"
fixture_line=$(grep -nF "bash scripts/check-public-boundary.test.sh" "$justfile" | cut -d: -f1)
live_line=$(grep -nF "bash scripts/check-public-boundary.sh" "$justfile" | cut -d: -f1)
((fixture_line < live_line)) || fail "public-boundary recipe does not run fixtures first"
check_recipe=$(awk '
    /^check:$/ { found = 1; next }
    found && /^[^[:space:]#].*:$/ { exit }
    found { print }
' "$justfile")
check_boundary_invocations=$(grep -Fc -- "just public-boundary" <<<"$check_recipe" || true)
((check_boundary_invocations == 1)) \
    || fail "check recipe invokes public-boundary ${check_boundary_invocations} times"
for recipe in lint-sh fmt-sh fmt-sh-check; do
    recipe_body=$(awk -v header="${recipe}:" '
        $0 == header { found = 1; next }
        found && /^[^[:space:]#].*:$/ { exit }
        found { print }
    ' "$justfile")
    for script in scripts/check-public-boundary.sh scripts/check-public-boundary.test.sh; do
        [[ "$recipe_body" == *"$script"* ]] || fail "${recipe} omits ${script}"
    done
done
pass "Justfile boundary wiring"

# The dedicated workflow is always on and exercises the full gate once.
workflow="${script_dir}/../.github/workflows/boundary.yml"
[[ -f "$workflow" ]] || fail "boundary workflow is missing"
grep -Eq '^  pull_request:([[:space:]]*\{\})?$' "$workflow" || fail "workflow omits pull requests"
grep -Eq '^  push:$' "$workflow" || fail "workflow omits pushes"
grep -Eq '^      - main$' "$workflow" || fail "workflow push does not target main"
grep -Eq '^  workflow_dispatch:([[:space:]]*\{\})?$' "$workflow" || fail "workflow omits manual dispatch"
if grep -Eq '^[[:space:]]+paths(-ignore)?:' "$workflow"; then
    fail "workflow uses a path filter"
fi
grep -Fq 'permissions:' "$workflow" || fail "workflow omits permissions"
grep -Fq 'contents: read' "$workflow" || fail "workflow permissions are not read-only"
grep -Fq 'fetch-depth: 0' "$workflow" || fail "workflow checkout is shallow"
grep -Fq 'cachix/install-nix-action@v31' "$workflow" || fail "workflow omits Nix setup"
grep -Fq 'node-version: 22' "$workflow" || fail "workflow does not use Node 22"
grep -Fq 'cache: npm' "$workflow" || fail "workflow does not use the npm cache"
lock_path="install/${private_root}/package-lock.json"
grep -Fq "cache-dependency-path: ${lock_path}" "$workflow" || fail "workflow npm cache has the wrong key"
grep -Fq 'run: npm ci' "$workflow" || fail "workflow omits npm ci"
grep -Fq "working-directory: install/${private_root}" "$workflow" || fail "npm ci uses the wrong directory"
just_action='taiki-e/install-action@a3324fb0eb94b8230ec968c3389c1b7929fc2f3b'
grep -Fq "uses: ${just_action}" "$workflow" || fail "workflow omits pinned just provisioning"
grep -Eq '^[[:space:]]+tool: just$' "$workflow" || fail "workflow just action omits the just tool"
just_action_line=$(awk -v needle="$just_action" 'index($0, needle) { print NR; exit }' "$workflow")
first_just_line=$(awk '/run:.*[[:space:]]just([[:space:]]|$)/ { print NR; exit }' "$workflow")
((just_action_line < first_just_line)) || fail "workflow invokes just before provisioning it"
if grep -Fq 'run: just public-boundary' "$workflow"; then
    fail "workflow invokes the boundary gate redundantly"
fi
grep -Fq 'run: nix develop path:. --no-pure-eval --command just check' "$workflow" \
    || fail "workflow omits the full Nix gate"
workflow_check_invocations=$(grep -Fc 'run: nix develop path:. --no-pure-eval --command just check' "$workflow")
((workflow_check_invocations == 1)) \
    || fail "workflow invokes the full gate ${workflow_check_invocations} times"
pass "always-on boundary workflow"

# Committed post-anchor paths remain in scope after the worktree is clean,
# including spaces, embedded newlines, and a rename into/out of a private root.
new_repo committed_history_paths
committed_history_repo=$REPLY
printf '%s\n' "clean" >"$committed_history_repo/README.md"
commit_all "$committed_history_repo" "clean anchor"
committed_history_anchor=$(git -C "$committed_history_repo" rev-parse HEAD)
mkdir -p "$committed_history_repo/$private_root"
history_space_path="${private_root}/committed secret.txt"
printf '%s\n' "secret" >"$committed_history_repo/$history_space_path"
commit_all "$committed_history_repo" "post-anchor path with spaces"
git -C "$committed_history_repo" rm -q -- "$history_space_path"
commit_all "$committed_history_repo" "remove path with spaces"
mkdir -p "$committed_history_repo/$private_root"
history_newline_path="${private_root}/committed line"$'\n'"break.txt"
printf '%s\n' "secret" >"$committed_history_repo/$history_newline_path"
commit_all "$committed_history_repo" "post-anchor path with newline"
git -C "$committed_history_repo" rm -q -- "$history_newline_path"
commit_all "$committed_history_repo" "remove path with newline"
printf '%s\n' "rename source" >"$committed_history_repo/public-source.txt"
commit_all "$committed_history_repo" "rename source"
mkdir -p "$committed_history_repo/$private_root"
git -C "$committed_history_repo" mv public-source.txt "$private_root/renamed-secret.txt"
commit_all "$committed_history_repo" "rename into private root"
git -C "$committed_history_repo" mv "$private_root/renamed-secret.txt" public-destination.txt
commit_all "$committed_history_repo" "rename out of private root"
[[ -z $(git -C "$committed_history_repo" status --porcelain) ]] \
    || fail "committed history fixture worktree is not clean"
expect_violation "committed historical path with spaces" "$committed_history_repo" \
    "$committed_history_anchor" "private-root" "committed\\ secret.txt"
expect_violation "committed historical path with newline" "$committed_history_repo" \
    "$committed_history_anchor" "private-root" "committed line"
expect_violation "committed rename into and out of private root" "$committed_history_repo" \
    "$committed_history_anchor" "private-root" "renamed-secret.txt"

# History scans must use one batch cat-file process and deduplicate repeated blob objects.
new_repo batch_probe
batch_probe_repo=$REPLY
printf '%s\n' "clean" >"$batch_probe_repo/anchor.txt"
: >"$batch_probe_repo/empty.txt"
commit_all "$batch_probe_repo" "clean anchor"
batch_probe_anchor=$(git -C "$batch_probe_repo" rev-parse HEAD)
for batch_index in 1 2 3; do
    printf '%s\n' "same blob" >"$batch_probe_repo/file-${batch_index}.txt"
    commit_all "$batch_probe_repo" "repeated blob ${batch_index}"
done
dd if=/dev/zero of="$batch_probe_repo/large.bin" bs=65536 count=1 status=none
printf '\177' >>"$batch_probe_repo/large.bin"
commit_all "$batch_probe_repo" "large binary blob"
printf '%s\n' "following distinct blob" >"$batch_probe_repo/following.txt"
commit_all "$batch_probe_repo" "following distinct blob"
batch_log="$tmp_root/batch-cat-file.log"
batch_requests="$tmp_root/batch-cat-file-requests.log"
: >"$batch_log"
: >"$batch_requests"
CHECK_GIT_PATH="$git_wrapper_dir:${PATH}" BOUNDARY_CAT_FILE="$batch_log" \
    BOUNDARY_CAT_REQUESTS="$batch_requests" \
    run_check "$batch_probe_repo" "$batch_probe_anchor"
((CHECK_STATUS == 0)) || fail "batch scanner probe failed: $CHECK_OUTPUT"
batch_invocations=$(wc -l <"$batch_log")
((batch_invocations == 1)) || fail "expected one cat-file process, got ${batch_invocations}: $(<"$batch_log")"
grep -Fq -- 'cat-file --batch' "$batch_log" \
    || fail "boundary scanner did not use cat-file --batch"
if grep -Fq -- 'cat-file blob' "$batch_log"; then
    fail "boundary scanner still launches one cat-file process per blob"
fi
repeat_oid=$(git -C "$batch_probe_repo" hash-object "$batch_probe_repo/file-1.txt")
repeat_requests=$(grep -Fxc -- "$repeat_oid" "$batch_requests" || true)
((repeat_requests == 1)) \
    || fail "expected repeated blob OID ${repeat_oid} exactly once, got ${repeat_requests}: $(<"$batch_requests")"
pass "single-pass deduplicated cat-file history scan"

# Batch protocol failures fail closed, while normal requests remain delegated to Git.
for batch_failure in malformed-header malformed-object mismatched-object non-blob invalid-size \
    overflow-size nul-header truncated-body bad-terminator nul-terminator nonzero-exit; do
    batch_diagnostic="Git returned a malformed batch response"
    case $batch_failure in
        mismatched-object) batch_diagnostic="Git returned a malformed batch object response" ;;
        non-blob) batch_diagnostic="Git returned non-blob object" ;;
        invalid-size) batch_diagnostic="Git returned malformed blob size" ;;
        overflow-size | truncated-body) batch_diagnostic="Git returned truncated batch object" ;;
        nul-terminator | bad-terminator) batch_diagnostic="Git returned malformed batch object terminator" ;;
        nonzero-exit) batch_diagnostic="Git failed while reading batch objects" ;;
    esac
    BOUNDARY_MALFORM_BATCH="$batch_failure" CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" \
        expect_closed "batch ${batch_failure}" "$batch_probe_repo" "$batch_probe_anchor" \
        "$batch_diagnostic"
done

# Parser-specific malformed fixtures use one explicit request and complete
# remainder bytes, so removing the target validation cannot produce a false pass.
expect_helper_closed "helper mismatched object" mismatched-object \
    "Git returned a malformed batch object response"
expect_helper_closed "helper malformed header" malformed-header \
    "Git returned a malformed batch response"
expect_helper_closed "helper malformed extra-field response" malformed-object \
    "Git returned a malformed batch response"
expect_helper_closed "helper NUL header" nul-header \
    "Git returned a malformed batch response"
expect_helper_closed "helper non-blob response" non-blob \
    "Git returned non-blob object"
expect_helper_closed "helper invalid-size response" invalid-size \
    "Git returned malformed blob size"
expect_helper_closed "helper overflow size" overflow-size \
    "Git returned truncated batch object"
expect_helper_closed "helper truncated body" truncated-body \
    "Git returned truncated batch object"
expect_helper_closed "helper printable terminator" bad-terminator \
    "Git returned malformed batch object terminator"
expect_helper_closed "helper NUL terminator" nul-terminator \
    "Git returned malformed batch object terminator"

# A retained byte must be rejected before waiting for another child pull, even
# when the child remains alive after writing the complete empty response.
expect_helper_closed "helper buffered extra output" extra-output \
    "Git returned unexpected extra batch output"

# Parser rejection must remain structured while the writer is blocked by a
# child that never consumes its large request stream.
backpressure_requests="$tmp_root/helper-early-parser-backpressure.requests"
for backpressure_index in $(seq 1 200000); do
    printf '%040d\n' "$backpressure_index" >>"$backpressure_requests"
done
set +e
CHECK_OUTPUT=$(
    BOUNDARY_MALFORM_BATCH=early-parser-failure-backpressure \
        BOUNDARY_REAL_GIT="$real_git" PATH="$git_wrapper_dir:${PATH}" timeout 10s \
        node "$helper" "$batch_probe_repo" "$backpressure_requests" \
        "$tmp_root/helper-early-parser-backpressure.output" 2>&1
)
CHECK_STATUS=$?
set -e
((CHECK_STATUS == 1)) || fail "helper early parser backpressure: expected status 1, got $CHECK_STATUS: $CHECK_OUTPUT"
[[ "$CHECK_OUTPUT" == *"Git returned a malformed batch response"* ]] \
    || fail "helper early parser backpressure: missing malformed response diagnostic: $CHECK_OUTPUT"
[[ "$CHECK_OUTPUT" != *"Error:"* && "$CHECK_OUTPUT" != *"Unhandled"* && "$CHECK_OUTPUT" != *"timeout"* ]] \
    || fail "helper early parser backpressure: unhandled rejection, timeout, or stack: $CHECK_OUTPUT"
pass "helper early parser failure under writer backpressure"

# Payload reads are bounded to 64 KiB chunks and preserve the following record.
chunk_log="$tmp_root/batch-chunks.log"
: >"$chunk_log"
BOUNDARY_CHUNK_LOG="$chunk_log" CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" \
    run_check "$batch_probe_repo" "$batch_probe_anchor"
((CHECK_STATUS == 0)) || fail "bounded payload probe failed: $CHECK_OUTPUT"
grep -Eq -- '(^| )65536($| )' "$chunk_log" \
    || fail "missing 65536-byte batch chunk: $(<"$chunk_log")"
grep -Eq -- '(^| )1($| )' "$chunk_log" \
    || fail "missing one-byte batch remainder: $(<"$chunk_log")"
if awk '$NF > 65536 { found = 1 } END { exit !found }' "$chunk_log"; then
    fail "batch payload reader exceeded 65536-byte chunks: $(<"$chunk_log")"
fi
large_oid=$(git -C "$batch_probe_repo" hash-object "$batch_probe_repo/large.bin")
following_oid=$(git -C "$batch_probe_repo" hash-object "$batch_probe_repo/following.txt")
grep -Fqx -- "$large_oid" "$batch_requests" \
    || fail "large binary blob was not requested"
grep -Fqx -- "$following_oid" "$batch_requests" \
    || fail "following blob was not requested"
pass "bounded exact-length batch payload reads"

# The helper itself must preserve record framing when a 65,537-byte object is
# followed by another request in an explicitly ordered request file.
ordered_requests="$tmp_root/ordered-batch.requests"
ordered_output="$tmp_root/ordered-batch.output"
ordered_chunk_log="$tmp_root/ordered-batch.chunks"
ordered_request_log="$tmp_root/ordered-batch.requests.log"
printf '%s\n%s\n' "$large_oid" "$following_oid" >"$ordered_requests"
: >"$ordered_chunk_log"
: >"$ordered_request_log"
set +e
CHECK_OUTPUT=$(
    BOUNDARY_CAT_REQUESTS="$ordered_request_log" \
        BOUNDARY_CHUNK_LOG="$ordered_chunk_log" BOUNDARY_REAL_GIT="$real_git" \
        PATH="$git_wrapper_dir:${PATH}" node "$helper" "$batch_probe_repo" \
        "$ordered_requests" "$ordered_output" 2>&1
)
CHECK_STATUS=$?
set -e
((CHECK_STATUS == 0)) || fail "ordered helper framing probe failed: $CHECK_OUTPUT"
[[ "$(sed -n '1p' "$ordered_request_log")" == "$large_oid" &&
"$(sed -n '2p' "$ordered_request_log")" == "$following_oid" ]] \
    || fail "helper did not receive large then following request order: $(<"$ordered_request_log")"
grep -Fqx -- "$large_oid 65536" "$ordered_chunk_log" \
    || fail "missing large-object 65536-byte chunk: $(<"$ordered_chunk_log")"
grep -Fqx -- "$large_oid 1" "$ordered_chunk_log" \
    || fail "missing large-object one-byte remainder: $(<"$ordered_chunk_log")"
if awk '$2 > 65536 { found = 1 } END { exit !found }' "$ordered_chunk_log"; then
    fail "ordered helper emitted a chunk over 65536 bytes: $(<"$ordered_chunk_log")"
fi
cmp -s "$batch_probe_repo/large.bin" "$ordered_output/$large_oid" \
    || fail "ordered helper large payload differs from source"
cmp -s "$batch_probe_repo/following.txt" "$ordered_output/$following_oid" \
    || fail "ordered helper following payload was not extracted after large object"
pass "ordered large-then-following batch framing"

# Opening an output path that is already a directory must map to a controlled
# batch-object diagnostic rather than an unhandled stream error.
write_failure_requests="$tmp_root/write-failure.requests"
write_failure_output="$tmp_root/write-failure.output"
mkdir -p "$write_failure_output/$following_oid"
printf '%s\n' "$following_oid" >"$write_failure_requests"
set +e
CHECK_OUTPUT=$(
    BOUNDARY_REAL_GIT="$real_git" PATH="$git_wrapper_dir:${PATH}" timeout 10s \
        node "$helper" "$batch_probe_repo" "$write_failure_requests" \
        "$write_failure_output" 2>&1
)
CHECK_STATUS=$?
set -e
((CHECK_STATUS == 1)) || fail "helper write failure: expected status 1, got $CHECK_STATUS: $CHECK_OUTPUT"
[[ "$CHECK_OUTPUT" == *"Git failed while reading batch object"* ]] \
    || fail "helper write failure: missing controlled diagnostic: $CHECK_OUTPUT"
[[ "$CHECK_OUTPUT" != *"Error:"* && "$CHECK_OUTPUT" != *"Unhandled"* ]] \
    || fail "helper write failure: unhandled stack/rejection: $CHECK_OUTPUT"
pass "controlled batch output-file failure"

echo "PASS: ${tests_run} public-boundary fixture cases"
