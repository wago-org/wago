#!/usr/bin/env bash
# User-local only. Refuses to overwrite an existing toolchain.
set -euo pipefail
root=$(realpath "${1:-.}")
destination=${2:-/home/dev/.local/share/wago-tinygo-investigation}
mkdir -p "$destination"
if [ -e "$destination/tinygo" ]; then echo 'toolchain already exists; inspect provenance instead of overwriting' >&2; exit 1; fi
curl -fL https://github.com/tinygo-org/tinygo/releases/download/v0.42.0/tinygo0.42.0.linux-amd64.tar.gz -o "$destination/tinygo042.tar.gz"
printf '%s  %s\n' b87688fa2e19cee7d813cad7fd7dadb71dff3198e47125aba66ba4af5e490438 "$destination/tinygo042.tar.gz" | sha256sum -c -
tar -xzf "$destination/tinygo042.tar.gz" -C "$destination"
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export GOTOOLCHAIN=local PATH="$destination/tinygo/bin:$GOROOT/bin:$PATH"
bash "$root/.tools/stage3-full-suite/current/scripts/patch-tinygo-042.sh"
tinygo version
mkdir -p "$destination/wrapper"
ln -s "$root/docs/rfwgc-evidence/stage3-tinygo-investigation/tinygo-capture.py" "$destination/wrapper/tinygo"
