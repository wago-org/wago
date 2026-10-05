#!/usr/bin/env bash
# Inspect unstripped production binaries, rather than inferring DCE from size.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_dir"
check_dir="$(mktemp -d)"
trap 'rm -rf -- "$check_dir"' EXIT
for target_os in linux darwin windows; do
  for arch in amd64 arm64; do
    for flavor in manager runtime minimal embed; do
      build_tags=""
      build_package=./cli/wago
      case "$flavor" in
        runtime) build_tags=wago_runtime ;;
        minimal) build_tags=wago_runtime,wago_minimal ;;
        embed) build_package=./scripts/testdata/diagnostic-dce ;;
      esac
      CGO_ENABLED=0 GOOS="$target_os" GOARCH="$arch" go build -tags="$build_tags" -o "$check_dir/wago-$arch" "$build_package"
      go tool nm "$check_dir/wago-$arch" > "$check_dir/symbols-$arch"
      # Type equality functions and API no-op stubs may remain. These expressions
      # identify executable collection, journaling, attribution, and report bodies.
      scan_status=0
      grep -E -e ' T .*((internal/jitprofile\.(New|\(\*Session\)))|internal/(profcapture|profilecmd)|RemapNativeCodeSites|recordProfileCodeSite|recordProfileFPressure|profileCodeSites|CodeSites|RemapNativeUnwind|collectProfileUnwind|collectProfileAdapterUnwind|recordSharedAdapterUnwind|recordSharedAdapterTailUnwind|RemapNativeSources|collectProfileSources|rememberProfileNode|enterProfileNode|enterProfileInstruction|recordProfileTrap|profileTrapOrigin|switchProfileOrigin|rewindProfileEmission|profileEmissionRanges|SourceEmission|OverlayNativeSources|enterProfileInlineAdd|leaveProfileInlineAdd|enterProfileInline|leaveProfileInline|profileTrapCaller|profileInlineFrames|compiledProfile|attachProfile|registerProfileThunk|retireProfileImage|beginProfileBoundary|beginProfileActivation|beginProfileInvocation|beginProfileHostCallback|beginProfileInstantiation|beginProfileInitialization|beginProfileLifecycle|bindProfileInstance|callProfiledHost|profileReentryContext|installCodeProfile|recordProfileRegions|railshotGCNativeCodeTelemetry|\(\*CodegenStats\)\.(report|peep|call|add)|\(\*Telemetry\)\.(begin|end|noteObjectScan)|writeJIT|profile\.JITDump)' -e ' T github.com/wago-org/wago/(profile|internal/jitprofile|internal/regalloccheck)\.' -e ' T github.com/wago-org/wago/src/core/compiler/backend/railshot/(amd64|arm64)\.(\(\*fn\)\.)?check(BeginFlush|EndFlush|Use|Inputs|FoldedUse|Occupy|BeginSlots|BeginRegMoves|Immutable|ReleaseImmutable|CallClobber|EndLifetimes|TerminalGPWrites|RestoreGPWrites|RegMovesWindow|Size|Reg)' -e ' T github.com/wago-org/wago/src/core/compiler/backend/railshot/(shared|amd64|arm64)\..*(scalarGraph|ScalarGraph|checkGraph|checkSource|SourceLeaf|sourceLeaf|leafOperator|leafReg)' -e ' T github.com/wago-org/wago/src/core/compiler/codegen\.(SourceOptions|AttachSourceContext|SourceContextFor|ValidatedSourceContext|CloseSourceContext|SetSourceReporter|RecordSourceResult)' -e ' T github.com/wago-org/wago/src/core/compiler/backend/railshot/shared\.(PrepareSourceWorker|SetSourceContext|SetSourceWorkerContext|BeginSourceLeaf|BeginSourceAttempt|AttachSourceLedger|EndSourceAttempt|finishSourceWorker)' -e ' T github.com/wago-org/wago/src/core/compiler/backend/railshot/(amd64|arm64)\.compileSourceModuleWith' -e ' T github.com/wago-org/wago/src/wago\.railshotCompileValidatedModuleWith' -e ' T github.com/wago-org/wago/src/core/encoder/(amd64|arm64)\.(\(\*Asm\)\.)?regalloc' "$check_dir/symbols-$arch" || scan_status=$?
      if [[ "$scan_status" == 0 ]]; then
        echo "diagnostic implementation retained in $target_os/$arch $flavor" >&2
        exit 1
      elif [[ "$scan_status" != 1 ]]; then
        echo "diagnostic symbol scan failed for $target_os/$arch $flavor (status $scan_status)" >&2
        exit "$scan_status"
      fi
      echo "$target_os/$arch $flavor: diagnostic implementation symbols absent"
    done
  done
done
