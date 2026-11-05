#!/usr/bin/env bash
set -euo pipefail

# The Azure mirror has stalled CI for over 40 minutes. Use the signed Ubuntu
# archive over HTTPS and bound failed network transfers instead.
for source in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
  [[ -f "$source" ]] || continue
  sudo sed -E -i \
    's|https?://azure\.archive\.ubuntu\.com/ubuntu|https://archive.ubuntu.com/ubuntu|g' \
    "$source"
done

apt_options=(
  -o Acquire::Retries=3
  -o Acquire::http::Timeout=30
  -o Acquire::https::Timeout=30
)
sudo apt-get "${apt_options[@]}" update
sudo apt-get "${apt_options[@]}" install -y ocaml-nox ocaml-dune menhir
