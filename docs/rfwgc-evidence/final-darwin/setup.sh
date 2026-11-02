#!/bin/bash
# Executed only in an ephemeral native Actions source checkout, never the developer tree.
set -euo pipefail
[[ $(uname -s) == Darwin && $(uname -m) == arm64 ]]
[[ $(sysctl -in sysctl.proc_translated 2>/dev/null || true) != 1 ]]
source_root=$(cd "$1" && pwd)
receipts=$(cd "$2" && pwd)
[[ $(git -C "$source_root" rev-parse HEAD) == "$QUALIFICATION_SOURCE" ]]
[[ -z $(git -C "$source_root" status --porcelain --untracked-files=all) ]]
git -C "$source_root" submodule status --recursive | tee "$receipts/submodules.txt"
! grep -E '^[-+U]' "$receipts/submodules.txt"
[[ $(go version) == 'go version go1.27.1 darwin/arm64' ]]
export GOROOT="$(go env GOROOT)"
printf 'GOROOT=%s\n' "$GOROOT" >> "$GITHUB_ENV"
[[ -n $GOROOT && $(go env GOROOT) == "$GOROOT" ]]
[[ $(command -v go) == "$GOROOT/bin/go" ]]
go env -json GOROOT GOVERSION GOHOSTOS GOHOSTARCH > "$receipts/go-toolchain.json"
[[ $(just --version) == 'just 1.58.0' ]]
xcode-select -p
clang --version
python3 --version
sw_vers
printf 'ImageOS=%s ImageVersion=%s\n' "${ImageOS:-unknown}" "${ImageVersion:-unknown}"
[[ $(go env CGO_ENABLED) == 1 ]]
# The runner's OS/Xcode/Python/Homebrew are recorded infrastructure, not hermetic pins.
tool_root="$RUNNER_TEMP/rfwgc-tools"
mkdir -p "$tool_root/node" "$tool_root/powershell"
fetch() {
  curl --fail --location --retry 3 "$1" -o "$tool_root/$2"
  printf '%s  %s\n' "$3" "$tool_root/$2" | shasum -a 256 -c -
}
fetch https://nodejs.org/dist/v24.22.2/node-v24.22.2-darwin-arm64.tar.gz node.tar.gz aa9549349e47dffaa42dcf95d6e06ade8c4bdafc980979883b88767670df385a
tar -xzf "$tool_root/node.tar.gz" --strip-components=1 -C "$tool_root/node"
fetch https://github.com/PowerShell/PowerShell/releases/download/v7.6.8/powershell-7.6.8-osx-arm64.tar.gz pwsh.tar.gz c16a3f433966c6fd5a9c8a660a95c92cb5dbc4b1813416501d1ba30ca160da32
tar -xzf "$tool_root/pwsh.tar.gz" -C "$tool_root/powershell"
chmod +x "$tool_root/powershell/pwsh"
export PATH="$tool_root/node/bin:$tool_root/powershell:$PATH"
[[ $(node --version) == v24.22.2 ]]
[[ $(node -p process.arch) == arm64 ]]
[[ $(pwsh -NoLogo -NoProfile -Command '$PSVersionTable.PSVersion.ToString()') == 7.6.8 ]]
printf '%s\n' "$tool_root/node/bin" "$tool_root/powershell" >> "$GITHUB_PATH"
# Pin compiler/parser versions; Homebrew is only the recorded opam bootstrap.
brew install opam
opam --version
export OPAMROOT="$tool_root/opam"
opam init --bare --disable-sandboxing --no-setup -y
opam switch create darwin ocaml-base-compiler.4.14.3 -y
opam install --switch=darwin dune.3.22.1 menhir.20260209 -y
eval "$(opam env --switch=darwin --set-switch)"
opam list --installed --columns=name,version > "$receipts/opam-packages.txt"
opam switch export "$receipts/opam-switch.export"
printf '%s\n' "$OPAMROOT/darwin/bin" >> "$GITHUB_PATH"
cd "$source_root"
scripts/bootstrap-wabt.sh --verify
scripts/bootstrap-spec-interpreter.sh --verify
wabt=$(scripts/bootstrap-wabt.sh --print-path)
interpreter=$(scripts/bootstrap-spec-interpreter.sh --print-path)
printf '%s\n' "$(dirname "$wabt")" >> "$GITHUB_PATH"
printf 'WAGO_WAST2JSON=%s\nWAGO_SPEC_INTERPRETER=%s\nWAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa\n' "$wabt" "$interpreter" >> "$GITHUB_ENV"
shasum -a 256 "$(command -v go)" "$(command -v just)" "$(command -v node)" "$(command -v pwsh)" "$wabt" "$interpreter" > "$receipts/tool-hashes.txt"
cp .tools/spec-interpreter-*/build-provenance "$receipts/spec-build-provenance.txt"
cp .tools/wabt-*/.wago-provenance "$receipts/wabt-provenance.txt"
[[ -z $(git status --porcelain --untracked-files=all) ]]
