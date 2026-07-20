#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/dt-task-smoke.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

bin="$tmp_dir/dt-task"
export GOCACHE="$tmp_dir/go-cache"
go build -o "$bin" "$root_dir/cmd/dt-task"
mkdir -p "$tmp_dir/project" "$tmp_dir/home"

export DT_TASK_HOME="$tmp_dir/state"
export HOME="$tmp_dir/home"
export CODEX_HOME="$tmp_dir/home/codex"
cd "$tmp_dir/project"

"$bin" init --alias smoke >/dev/null
"$bin" capture "smoke draft" >/dev/null
"$bin" task create --title "smoke task" --problem p --outcome o --acceptance a --estimate 5 >/dev/null
"$bin" task status 2 planned >/dev/null
"$bin" day start >/dev/null
"$bin" day status >/dev/null
"$bin" analytics --all-time >/dev/null
"$bin" doctor >/dev/null
"$bin" skill >/dev/null
"$bin" skill --status >/dev/null
"$bin" version >/dev/null
