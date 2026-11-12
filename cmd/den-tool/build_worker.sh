#!/usr/bin/env bash
set -euo pipefail

mode=$1
root=$2
repo=$3
source_repo=$4
ref=$5
jobs=$6
min_free_gib=$7
required_os=$8
shift 8

export CARGO_BUILD_JOBS="$jobs" CMAKE_BUILD_PARALLEL_LEVEL="$jobs"
export RUST_TEST_THREADS="$jobs" DOTNET_PROCESSOR_COUNT="$jobs"
unset DISPLAY WAYLAND_DISPLAY

fail() {
    printf 'den-build: %s\n' "$*" >&2
    exit 1
}

check_disk() {
    local directory=$root available
    while [[ ! -d "$directory" ]]; do directory=$(dirname -- "$directory"); done
    [[ -w "$directory" ]] || fail "build directory $directory is not writable"
    available=$(df --output=avail -B1 -- "$directory" | tail -n 1)
    ((available >= min_free_gib * 1024 * 1024 * 1024)) ||
        fail "$directory has less than ${min_free_gib} GiB free"
}

if [[ "$mode" == probe ]]; then
    type -P git flock df >/dev/null || fail "git, flock and df are required"
    if [[ -n "$required_os" ]]; then
        . /etc/os-release
        [[ "$ID" == "$required_os" ]] || fail "lane requires $required_os (found $ID)"
    fi
    check_disk
    if [[ -f "$root/.lock" ]]; then
        flock -n 9 9<"$root/.lock" || fail "another routed build holds $root"
    fi
    if [[ "$source_repo" == /* ]]; then
        [[ "$(git -C "$source_repo" cat-file -t "$ref" 2>/dev/null)" == commit ]] ||
            fail "commit $ref is not available from $source_repo"
    fi
    while (($#)); do
        if ! output=$(bash -euo pipefail -c "$2" 2>&1); then
            fail "toolchain $1 is unavailable${output:+: $output}"
        fi
        shift 2
    done
    exit 0
fi

run_build() {
    local origin=$1 run_id=$2 keep_count=$3
    shift 3
    local checkout="$root/$repo/checkout" i
    local -a keep=()
    for ((i = 0; i < keep_count; i++)); do
        keep+=(-e "/$1/")
        shift
    done

    if [[ ! -e "$checkout" ]]; then
        git init -q -- "$checkout"
        git -C "$checkout" config --local den-build.managed true
    fi
    cd -- "$checkout"
    # A bad build_root must never let git clean erase a regular checkout.
    [[ -d .git && "$(git config --local --get den-build.managed)" == true ]] ||
        fail "$checkout is not a den-build checkout"
    if [[ -n "$origin" ]]; then
        git config --local remote.origin.url "$origin"
    fi
    if ! git cat-file -e "$ref" 2>/dev/null; then
        GIT_TERMINAL_PROMPT=0 GIT_SSH_COMMAND="${GIT_SSH_COMMAND:-ssh -o BatchMode=yes}" \
            git fetch --no-tags -- "$source_repo" "$ref"
    fi
    [[ "$(git cat-file -t "$ref")" == commit ]] || fail "$ref is not a commit SHA"
    git checkout --detach --force -q "$ref"
    git clean -q -ffdx "${keep[@]}"

    export DEN_BUILD_OUTPUT_DIR="$root/$repo/outputs/$run_id"
    printf 'den-build: %s at %s\n' "$checkout" "$ref" >&2
    exec "$@"
}

# The supervisor holds the lock. Compiler servers can outlive their lane,
# so they must not inherit the descriptor.
mkdir -p -- "$root"
exec 9>"$root/.lock"
flock -n 9 || fail "another routed build holds $root"
check_disk

# SSH keeps stdin open while the CLI is alive. On disconnect, stop the whole
# build process group so it cannot keep compiling after the ledger checks out.
set -m
run_build "$@" </dev/null 9>&- &
job=$!
(read -r _ || kill -TERM -- "-$job" 2>/dev/null || true) <&0 9>&- &
watcher=$!
trap 'kill "$watcher" 2>/dev/null || true; wait "$watcher" 2>/dev/null || true' EXIT
wait "$job"
