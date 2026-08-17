#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 022

readonly PROGRAM="codex-account"
readonly MODULE="nyashachiroro.com/codex-account"
readonly DEFAULT_ARCHES="amd64 arm64"

usage() {
  cat <<'EOF'
Usage: scripts/release-linux.sh VERSION

Build reproducible Linux release archives and a SHA-256 checksum manifest.

  scripts/release-linux.sh v1.2.3
  ARCHES="amd64 arm64" scripts/release-linux.sh v1.2.3

Environment:
  ARCHES             Space-separated Go architectures (default: amd64 arm64)
  DIST_DIR           Output directory (default: dist)
  ALLOW_DIRTY=1      Permit a release from a dirty working tree
  SKIP_TESTS=1       Skip the pre-release test suite
  COMMIT             Commit hash to embed (default: current Git HEAD)
  SOURCE_DATE_EPOCH  Unix timestamp for reproducible build/archive metadata
                     (default: the selected commit's timestamp)
EOF
}

die() {
  printf 'release-linux: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
  usage
  exit 0
fi

[[ $# -eq 1 ]] || {
  usage >&2
  exit 2
}

version=$1
if [[ ! $version =~ ^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
  die "VERSION must be a semantic version such as v1.2.3 or v1.2.3-rc.1"
fi
archive_version=${version#v}

version_without_build=${archive_version%%+*}
if [[ $version_without_build == *-* ]]; then
  prerelease=${version_without_build#*-}
  IFS='.' read -r -a prerelease_identifiers <<< "$prerelease"
  for identifier in "${prerelease_identifiers[@]}"; do
    if [[ $identifier =~ ^0[0-9]+$ ]]; then
      die "numeric prerelease identifiers must not contain leading zeroes: $identifier"
    fi
  done
fi

for command_name in date git go gzip mktemp sha256sum tar; do
  require_command "$command_name"
done

[[ $(uname -s) == "Linux" ]] || die "this release script must run on Linux"
tar --version | head -n 1 | grep -q 'GNU tar' || die "GNU tar is required"

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/.." && pwd -P)
cd -- "$repo_root"
[[ -f go.mod ]] || die "go.mod was not found in $repo_root"

head_commit=""
if git rev-parse --is-inside-work-tree >/dev/null 2>&1 && git rev-parse --verify HEAD >/dev/null 2>&1; then
  head_commit=$(git rev-parse HEAD)
fi

commit=${COMMIT:-$head_commit}
[[ -n $commit ]] || die "no Git commit is available; set COMMIT and SOURCE_DATE_EPOCH for an exported source tree"
[[ $commit =~ ^[0-9A-Fa-f]{7,64}$ ]] || die "COMMIT must be a 7-64 character hexadecimal commit hash"

if [[ ${ALLOW_DIRTY:-0} != "1" && -n $head_commit ]]; then
  [[ $commit == "$head_commit" ]] || \
    die "COMMIT does not match the checked-out HEAD; check out that commit or set ALLOW_DIRTY=1"
  [[ -z $(git status --porcelain --untracked-files=normal) ]] || \
    die "working tree is dirty; commit the release contents or set ALLOW_DIRTY=1"
fi

source_epoch=${SOURCE_DATE_EPOCH:-}
if [[ -z $source_epoch ]]; then
  if git cat-file -e "${commit}^{commit}" 2>/dev/null; then
    source_epoch=$(git show -s --format=%ct "$commit")
  else
    die "SOURCE_DATE_EPOCH is required when COMMIT is not available in this Git repository"
  fi
fi
[[ $source_epoch =~ ^[0-9]+$ ]] || die "SOURCE_DATE_EPOCH must be a non-negative Unix timestamp"

build_time=$(date -u -d "@$source_epoch" '+%Y-%m-%dT%H:%M:%SZ') || \
  die "SOURCE_DATE_EPOCH is outside the supported date range"

IFS=' ' read -r -a arches <<< "${ARCHES:-$DEFAULT_ARCHES}"
[[ ${#arches[@]} -gt 0 ]] || die "ARCHES must contain at least one architecture"
declare -A seen_arches=()
for arch in "${arches[@]}"; do
  case "$arch" in
    amd64 | arm64) ;;
    *) die "unsupported architecture '$arch' (supported: amd64 arm64)" ;;
  esac
  [[ -z ${seen_arches[$arch]:-} ]] || die "duplicate architecture in ARCHES: $arch"
  seen_arches[$arch]=1
done

dist_dir=${DIST_DIR:-dist}
mkdir -p -- "$dist_dir"

archive_names=()
for arch in "${arches[@]}"; do
  archive_names+=("${PROGRAM}_${archive_version}_linux_${arch}.tar.gz")
done
checksum_name="${PROGRAM}_${archive_version}_SHA256SUMS"

for output_name in "${archive_names[@]}" "$checksum_name"; do
  [[ ! -e "$dist_dir/$output_name" && ! -L "$dist_dir/$output_name" ]] || \
    die "refusing to overwrite $dist_dir/$output_name"
done

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/${PROGRAM}-release.XXXXXXXX")
cleanup() {
  rm -rf -- "$tmp_dir"
}
trap cleanup EXIT INT TERM HUP

ldflags="-s -w -X ${MODULE}/internal/version.Version=${version} -X ${MODULE}/internal/version.Commit=${commit} -X ${MODULE}/internal/version.BuildTime=${build_time}"

if [[ ${SKIP_TESTS:-0} != "1" ]]; then
  printf 'Verifying modules...\n'
  GOENV=off GOWORK=off GOFLAGS= go mod verify
  printf 'Running tests...\n'
  GOENV=off GOWORK=off GOFLAGS= go test -mod=readonly ./...
fi

for index in "${!arches[@]}"; do
  arch=${arches[$index]}
  archive_name=${archive_names[$index]}
  package_name="${PROGRAM}_${archive_version}_linux_${arch}"
  package_dir="$tmp_dir/stage/$package_name"
  archive_tmp="$tmp_dir/$archive_name"

  mkdir -p -- "$package_dir"
  printf 'Building linux/%s...\n' "$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOAMD64=v1 GOARM64=v8.0 \
    GOENV=off GOWORK=off GOFLAGS= \
    go build \
      -mod=readonly \
      -trimpath \
      -buildvcs=false \
      -ldflags="$ldflags" \
      -o "$package_dir/$PROGRAM" \
      ./cmd/codex-account
  chmod 0755 "$package_dir/$PROGRAM"

  LC_ALL=C TAR_OPTIONS= tar \
    --sort=name \
    --format=posix \
    --pax-option='exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime' \
    --clamp-mtime \
    --mtime="@$source_epoch" \
    --numeric-owner \
    --owner=0 \
    --group=0 \
    --mode='go+u,go-w' \
    -C "$tmp_dir/stage" \
    -cf - \
    "$package_name" | GZIP= gzip -n -9 > "$archive_tmp"

  tar -tzf "$archive_tmp" >/dev/null
  rm -rf -- "$package_dir"
done

(
  cd -- "$tmp_dir"
  sha256sum "${archive_names[@]}"
) > "$tmp_dir/$checksum_name"

for output_name in "${archive_names[@]}" "$checksum_name"; do
  mv -- "$tmp_dir/$output_name" "$dist_dir/$output_name"
done

printf '\nRelease artifacts:\n'
for output_name in "${archive_names[@]}" "$checksum_name"; do
  printf '  %s\n' "$dist_dir/$output_name"
done
