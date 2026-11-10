#!/usr/bin/env bash
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

set -euo pipefail

# The Azure mirror has stalled CI for over 40 minutes. Use the signed Ubuntu
# archive over HTTPS and bound failed network transfers instead.
sources=(
  /etc/apt/sources.list
  /etc/apt/sources.list.d/*.list
  /etc/apt/sources.list.d/*.sources
  /etc/apt/*mirrors*.txt
)
for source in "${sources[@]}"; do
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
sudo timeout --kill-after=15s 5m apt-get "${apt_options[@]}" update
sudo timeout --kill-after=15s 10m apt-get "${apt_options[@]}" install -y ocaml ocaml-dune menhir
