#!/usr/bin/env bash
set -u
export GOMAXPROCS=4
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$PATH"
out=$ROOTING_EVIDENCE_DIR
scratch=$(mktemp -d /tmp/wago-rooting-cross.XXXXXX)
trap 'rm -rf "$scratch"' EXIT
status=0
for arch in arm64 riscv64; do
 for mode in ordinary wagodebug wago_regalloccheck; do
  flags=(); if [[ $mode != ordinary ]]; then flags=(-tags="$mode"); fi
  for pkg in ${ROOTING_CROSS_PACKAGES:-wago backend runtime}; do
   case $pkg in
    wago) package=./src/wago ;;
    backend) package=./src/core/compiler/backend/railshot/$arch ;;
    runtime) package=./src/core/runtime ;;
   esac
   GOOS=linux GOARCH=$arch CGO_ENABLED=0 go test -p=4 "${flags[@]}" -c "$package" -o "$scratch/$arch-$mode-$pkg.test" > "$out/cross-$arch-$mode-$pkg.txt" 2>&1
   result=$?
   printf '%s %s %s cross-compile exit=%s\n' "$arch" "$mode" "$pkg" "$result" | tee -a "$out/cross-exits.txt"
   if [[ $result != 0 ]]; then status=1; fi
  done
 done
done
exit "$status"
