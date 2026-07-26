#!/usr/bin/env sh
set -eu

repo="${1:-.}"

git_cmd() {
	git -C "$repo" "$@"
}

tag="$(git_cmd describe --tags --match 'v[0-9]*' --abbrev=0 2>/dev/null || true)"
if [ -z "$tag" ]; then
	echo "dev"
	exit 0
fi

if [ -n "$(git_cmd status --porcelain)" ]; then
	echo "${tag}-000"
	exit 0
fi

commits_after_tag="$(git_cmd rev-list "${tag}..HEAD" --count)"
if [ "$commits_after_tag" != "0" ]; then
	echo "${tag}-00"
	exit 0
fi

echo "$tag"
