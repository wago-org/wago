#!/usr/bin/env bash
# User-local native dependencies; never installs system packages or edits fixtures.
set -euo pipefail
export OPAMROOT=${WAGO_OCAML_ROOT:-"$HOME/.local/share/wago-rfwgc-opam"}
opam_bin=${OPAM_BIN:-"$HOME/.local/share/mise/installs/opam/2.6.1/opam"}
[ "$("$opam_bin" --version)" = 2.6.1 ]
export OPAMJOBS=2
if [ ! -f "$OPAMROOT/config" ]; then
  "$opam_bin" init --bare --disable-sandboxing --no-setup -y
fi
recipe_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# Full export includes the exact package recipes and upstream source checksums.
"$opam_bin" switch import --switch=rfwgc --jobs=2 -y "$recipe_dir/toolchain.export"
printf 'export PATH=%q:$PATH\n' "$OPAMROOT/rfwgc/bin"
