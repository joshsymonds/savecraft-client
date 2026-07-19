#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
checker="${script_dir}/check-public-boundary.sh"

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
            BOUNDARY_REAL_GIT="$real_git" PATH="$CHECK_GIT_PATH" \
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
printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -euo pipefail' \
    'args=("$@")' \
    'command_index=0' \
    "while ((command_index < \${#args[@]})); do" \
    "    case \${args[command_index]} in" \
    '    -C|-c|--git-dir|--work-tree|--namespace)' \
    "        command_index=\$((command_index + 2))" \
    '        ;;' \
    '    --*)' \
    "        command_index=\$((command_index + 1))" \
    '        ;;' \
    '    *)' \
    '        break' \
    '        ;;' \
    '    esac' \
    'done' \
    "if [[ \${args[command_index]-} == rev-list ]]; then" \
    '    echo "forced rev-list failure" >&2' \
    '    exit 73' \
    'fi' \
    "exec \"\$BOUNDARY_REAL_GIT\" \"\$@\"" >"$git_wrapper_dir/git"
chmod +x "$git_wrapper_dir/git"
CHECK_GIT_PATH="${git_wrapper_dir}:${PATH}" expect_closed \
    "rev-list failure" "$bad_anchor_repo" "HEAD" "Git failed while enumerating history"

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
[[ "$check_recipe" == *"just public-boundary"* ]] || fail "check recipe omits public-boundary"
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

# The dedicated workflow is always on and exercises both the focused and full gates.
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
grep -Fq 'run: just public-boundary' "$workflow" || fail "workflow omits the explicit boundary gate"
grep -Fq 'run: nix develop path:. --no-pure-eval --command just check' "$workflow" \
    || fail "workflow omits the full Nix gate"
pass "always-on boundary workflow"

echo "PASS: ${tests_run} public-boundary fixture cases"
