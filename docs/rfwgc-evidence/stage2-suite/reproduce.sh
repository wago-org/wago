#!/usr/bin/env bash
# Run from parent Wago checkout. Evidence destination may be supplied as $1.
set -eu
revision=1ad799605c7018457b4b0cef4190c90132e337ac
out=$(realpath "${1:-docs/rfwgc-evidence/stage2-suite}")
qualification_tmp=$(mktemp -d /tmp/wago-stage2-suite.XXXXXX)
trap 'rm -rf -- "$qualification_tmp"; printf "removed %s\n" "$qualification_tmp" >> "$out/cleanup.txt"' EXIT
mkdir -p "$qualification_tmp/source" "$qualification_tmp/cache" "$qualification_tmp/tmp"
git archive "$revision" | tar -x -C "$qualification_tmp/source"
git ls-tree -rz --full-tree "$revision" > "$qualification_tmp/manifest"
cat > "$qualification_tmp/verify.py" <<'PY'
import hashlib, pathlib, sys
root=pathlib.Path(sys.argv[1]); count=0
for entry in pathlib.Path(sys.argv[2]).read_bytes().split(b'\0'):
    if not entry: continue
    meta,name=entry.split(b'\t',1); mode,kind,oid=meta.split()
    if kind != b'blob': continue
    p=root/name.decode()
    data=str(p.readlink()).encode() if p.is_symlink() else p.read_bytes()
    actual=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
    assert actual == oid.decode(), str(p)
    count+=1
print('Verified tracked blob content:',count)
PY
python3 "$qualification_tmp/verify.py" "$qualification_tmp/source" "$qualification_tmp/manifest" > "$out/identity-before.txt"
cd "$qualification_tmp/source"
unset GOWORK WAGO_SPEC_INTERPRETER
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 GOMAXPROCS=8 GOFLAGS=-p=8
export MISE_TRUSTED_CONFIG_PATHS="$PWD" GOCACHE="$qualification_tmp/cache" GOTMPDIR="$qualification_tmp/tmp" TMPDIR="$qualification_tmp/tmp"
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$PATH"
ulimit -c 0
{
 date -u; uname -a; go version; go env GOROOT GOWORK GOCACHE GOTMPDIR GOFLAGS; env | sort | rg '^(GOMAXPROCS|DOTNET_SYSTEM_GLOBALIZATION_INVARIANT|MISE_TRUSTED_CONFIG_PATHS)='; taskset -pc $$
 for tool in tinygo pwsh dune ocaml ocamlc menhir; do command -v "$tool" || true; done
 tinygo version; pwsh -NoProfile -Command '$PSVersionTable.PSVersion.ToString()'
} > "$out/environment.txt" 2>&1
for fixture in spec-v1 spec-v2 spec-v3; do
 case "$fixture" in
 spec-v1) pin=a8bcbafe6d2fb191ce0188de0e18fdc107fa2598; url=https://github.com/WebAssembly/testsuite ;;
 spec-v2) pin=05ca4182176763112561ae20153975c12bd689e4; url=https://github.com/WebAssembly/spec ;;
 spec-v3) pin=9d36019973201a19f9c9ebb0f10828b2fe2374aa; url=https://github.com/WebAssembly/spec ;;
 esac
 { git init "tests/conformance/$fixture"; timeout --kill-after=5s 180s git -C "tests/conformance/$fixture" fetch --depth=1 "$url" "$pin"; git -C "tests/conformance/$fixture" checkout --detach "$pin"; } > "$out/fixture-$fixture.txt" 2>&1
 done
run() {
 local name=$1; shift
 local started=$SECONDS status=0
 local command_text
 printf -v command_text '%q ' "$@"; printf '%s\n' "${command_text% }" > "$out/$name.command.txt"
 "$@" > "$out/$name.log" 2>&1 || status=$?
 printf '%s exit=%s elapsed_seconds=%s\n' "$name" "$status" "$((SECONDS-started))" | tee -a "$out/results.txt"
}
: > "$out/results.txt"
run wabt-bootstrap timeout --kill-after=5s 180s sh scripts/bootstrap-wabt.sh --verify
export PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH"
{ command -v wast2json; wast2json --version; cat .tools/wabt-1.0.41-linux-x64/.wago-provenance; } > "$out/wabt-provenance.txt" 2>&1
run interpreter-bootstrap timeout --kill-after=5s 120s sh scripts/bootstrap-spec-interpreter.sh --verify
run ordinary timeout --kill-after=15s 1800s go test -count=1 -timeout=15m ./...
run regalloccheck timeout --kill-after=15s 1800s go test -count=1 -timeout=15m -tags=wago_regalloccheck ./...
# Full roots already exercise non-staged packages; isolate only the failing runtime
# package when its failures are staged tooling failures, to prove the remainder.
if rg -q -- '^--- FAIL: TestStaged' "$out/ordinary.log"; then
 run ordinary-available-runtime timeout --kill-after=15s 1200s go test -count=1 -timeout=15m -skip '^TestStaged' ./src/wago
fi
if rg -q -- '^--- FAIL: TestStaged' "$out/regalloccheck.log"; then
 run regalloccheck-available-runtime timeout --kill-after=15s 1200s go test -count=1 -timeout=15m -tags=wago_regalloccheck -skip '^TestStaged' ./src/wago
fi
run cli-runtime timeout --kill-after=15s 1800s go test -count=1 -timeout=15m -tags=wago_runtime ./cli/...
python3 "$qualification_tmp/verify.py" "$qualification_tmp/source" "$qualification_tmp/manifest" > "$out/identity-after.txt"
