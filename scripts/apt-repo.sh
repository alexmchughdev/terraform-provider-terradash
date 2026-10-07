#!/usr/bin/env bash
# Adds .deb packages to a signed apt repository and regenerates its indexes.
#   scripts/apt-repo.sh <repo-dir> <gpg-key-id> <package.deb>...
set -euo pipefail

[ $# -ge 3 ] || { echo "usage: $0 <repo-dir> <gpg-key-id> <package.deb>..." >&2; exit 2; }
repo=$1
key=$2
shift 2

suite=stable
component=main
architectures="amd64 arm64"

mkdir -p "$repo/pool/$component"
cp "$@" "$repo/pool/$component/"
cd "$repo"

for arch in $architectures; do
  dir="dists/$suite/$component/binary-$arch"
  mkdir -p "$dir"
  apt-ftparchive --arch "$arch" packages "pool/$component" >"$dir/Packages"
  gzip -9fk "$dir/Packages"
done

apt-ftparchive \
  -o APT::FTPArchive::Release::Origin=terradash \
  -o APT::FTPArchive::Release::Label=terradash \
  -o APT::FTPArchive::Release::Suite="$suite" \
  -o APT::FTPArchive::Release::Codename="$suite" \
  -o APT::FTPArchive::Release::Architectures="$architectures" \
  -o APT::FTPArchive::Release::Components="$component" \
  release "dists/$suite" >"dists/$suite/Release"

gpg --batch --yes --local-user "$key" --clearsign --output "dists/$suite/InRelease" "dists/$suite/Release"
gpg --batch --yes --local-user "$key" --armor --detach-sign --output "dists/$suite/Release.gpg" "dists/$suite/Release"
gpg --batch --yes --armor --export "$key" >key.gpg
touch .nojekyll
