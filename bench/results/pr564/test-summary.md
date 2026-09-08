# Test command results

This table includes retained failed attempts, not just successful reruns.
See REPORT.md for source pins and the final qualification limits. Exit zero
means this command passed; a focused command does not qualify a full platform.

| Capture | Exit | Wrapper seconds |
|---|---:|---:|
| test-arm64-backend-final.status.json | 0 | 1.088 |
| test-arm64-backend.status.json | 1 | 0.17 |
| test-arm64-build-guard.status.json | 0 | 5.875 |
| test-arm64-build-runtime.status.json | 0 | 5.932 |
| test-arm64-guard.status.json | 0 | 0.28 |
| test-arm64-runtime.status.json | 0 | 0.285 |
| test-benchtests.status.json | 0 | 12.544 |
| test-cli-isolated.status.json | 0 | 2.657 |
| test-corpusguard.status.json | 0 | 9.546 |
| test-focused.status.json | 0 | 9.839 |
| test-full-pinned.status.json | 1 | 88.59 |
| test-full.status.json | 1 | 80.75 |
| test-guard.status.json | 0 | 12.575 |
| test-micro.status.json | 0 | 18.264 |
| test-native-guard-final.status.json | 0 | 9.987 |
| test-native-runtime.status.json | 0 | 15.639 |
| test-pinned-standalone.status.json | 0 | 93.53 |
| test-race.status.json | 0 | 67.499 |
| test-self-clean-env.status.json | 0 | 0.521 |
| test-spec2.status.json | 0 | 2.076 |
| test-wine-bootstrap-curl.status.json | 1 | 5.969 |

The final full pinned-tool root run fails on two Wine integration tests. Other
packages pass. Compiler/runtime tests also pass natively with Go 1.27.1. ARM64
runtime checks use QEMU, not native ARM64 hardware. The pinned standalone
package passes with TinyGo 0.41.1 and Go 1.22.12. Build-profile smoke results
and the Wine certutil diagnostic are separate JSON captures.
