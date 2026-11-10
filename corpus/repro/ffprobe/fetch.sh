#!/usr/bin/env bash
set -euo pipefail

output_dir="$(cd "$(dirname "$0")" && pwd)"
curl -fsSL 'https://github.com/holzschu/a-Shell-commands/releases/download/0.1/ffprobe.wasm' -o "$output_dir/ffprobe.wasm"
got="$(shasum -a 256 "$output_dir/ffprobe.wasm" | cut -d ' ' -f 1)"
want=43143986a2cd207f809c82917c8ccc81e32708d26be9e1121509bef5ec89a342
if [[ "$got" != "$want" ]]; then
  echo "ffprobe.wasm SHA-256 $got, want $want" >&2
  exit 1
fi
