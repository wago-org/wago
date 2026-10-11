#!/bin/sh
set -u
root=experiments/arm64-parity
status=0
go test -count=1 -run '^TestDecodeBuildGoJSONContextClosesInheritedStdoutOnCancel$' ./cli/manager/internal/plugin/build > "$root/final-pr-go-command-control.txt" 2>&1 || status=1
go test -count=1 -run '^TestBuildPublishesThroughRelativeOutputSymlinkChain$' ./cli/runtime/commands/build > "$root/final-pr-xattr-control.txt" 2>&1 || status=1
go test -c -o /tmp/wago-pr-standalone.test ./cli/manager/internal/standalone || exit 1
compat_go=/Users/work/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.8.darwin-arm64
PATH="$compat_go/bin:$PATH" GOROOT="$compat_go" GOTOOLCHAIN=local /tmp/wago-pr-standalone.test -test.run '^TestBuildTinyGo(EmbedsArtifactWithoutCompiler|StripsByDefault)$' -test.v > "$root/final-pr-tinygo-compatible.txt" 2>&1 || status=1
printf 'Focused CLI control status: %s\n' "$status"
exit "$status"
