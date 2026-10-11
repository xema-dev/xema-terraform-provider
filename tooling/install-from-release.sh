#!/usr/bin/env bash
# Install a released xema Terraform provider into a filesystem mirror.
#
#   tooling/install-from-release.sh <version> <mirror-dir> [config-file]
#
# Downloads terraform-provider-xema_<version>_<os>_<arch>.zip for each platform
# plus the release SHA256SUMS, verifies them once with sha256sum -c, lays them
# out as a Terraform filesystem mirror under <mirror-dir>, and writes a CLI
# config (default <mirror-dir>/terraform.rc) whose provider_installation block
# uses the mirror for xema-dev/xema only. Then:
#
#   export TF_CLI_CONFIG_FILE=<config-file>
#   terraform providers lock -fs-mirror=<mirror-dir> \
#     -platform=linux_amd64 -platform=linux_arm64 -platform=darwin_arm64
#
# and commit .terraform.lock.hcl: its hashes are the integrity check afterwards.
# Env: XEMA_PROVIDER_RELEASE_BASE overrides the download base URL
# (default https://github.com/xema-dev/xema-terraform-provider/releases/download/v<version>).
set -euo pipefail

ver="${1:?usage: $0 <version> <mirror-dir> [config-file]}"
dir="${2:?usage: $0 <version> <mirror-dir> [config-file]}"
ver="${ver#v}"
mkdir -p "$dir"
dir="$(cd "$dir" && pwd)"
cfg="${3:-$dir/terraform.rc}"
base="${XEMA_PROVIDER_RELEASE_BASE:-https://github.com/xema-dev/xema-terraform-provider/releases/download/v${ver}}"
platforms="linux_amd64 linux_arm64 darwin_amd64 darwin_arm64"
dest="$dir/registry.terraform.io/xema-dev/xema"

if command -v sha256sum >/dev/null 2>&1; then sumcheck=(sha256sum -c); else sumcheck=(shasum -a 256 -c); fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
sums="terraform-provider-xema_${ver}_SHA256SUMS"
curl -fsSL -o "$work/$sums" "$base/$sums"
for p in $platforms; do
  f="terraform-provider-xema_${ver}_${p}.zip"
  curl -fsSL -o "$work/$f" "$base/$f"
done
( cd "$work" && grep -E "_(${platforms// /|})\.zip\$" "$sums" | "${sumcheck[@]}" )

mkdir -p "$dest"
for p in $platforms; do
  cp "$work/terraform-provider-xema_${ver}_${p}.zip" "$dest/"
done

cat > "$cfg" <<EOF
provider_installation {
  filesystem_mirror {
    path    = "$dir"
    include = ["registry.terraform.io/xema-dev/xema"]
  }
  direct {
    exclude = ["registry.terraform.io/xema-dev/xema"]
  }
}
EOF
echo "mirror: $dest"
echo "cli config: $cfg  (export TF_CLI_CONFIG_FILE=$cfg)"
