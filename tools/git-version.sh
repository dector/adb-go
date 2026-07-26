#!/usr/bin/env sh
set -eu

repo="${1:-.}"

git_cmd() {
	git -C "$repo" "$@"
}

is_dirty() {
	[ -n "$(git_cmd status --porcelain)" ]
}

snapshot_suffix=""
if is_dirty; then
	snapshot_suffix="-snapshot"
fi

tag="$(git_cmd describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --abbrev=0 2>/dev/null || true)"
if [ -z "$tag" ]; then
	echo "dev${snapshot_suffix}"
	exit 0
fi

commits_after_tag="$(git_cmd rev-list "${tag}..HEAD" --count)"
if [ "$commits_after_tag" = "0" ]; then
	echo "${tag}${snapshot_suffix}"
	exit 0
fi

version_without_v="${tag#v}"
major="${version_without_v%%.*}"
minor_and_patch="${version_without_v#*.}"
minor="${minor_and_patch%%.*}"
patch="${minor_and_patch#*.}"
patch="${patch%%[-+]*}"

next_patch=$((patch + 1))
padded_count="$(printf '%03d' "$commits_after_tag")"
echo "v${major}.${minor}.${next_patch}-${padded_count}${snapshot_suffix}"
