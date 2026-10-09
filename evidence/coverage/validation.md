# Validation ledger

Commands use GOMAXPROCS=1, -p=1 and private GOCACHE. Whole-root/bench commands use
local permission escalation for socket and filesystem ACL fixtures. Results are
recorded explicitly below after each command completes.

| Check | Outcome | Evidence |
|---|---|---|
| Focused checked/profile and workflow guard at c57256f5 | Pass | final-source-checks.txt |
| Ordinary root suite, app-update attempt | Interrupted | root-ordinary-interrupted.txt |
| Ordinary root suite, completed | Fail: TinyGo VCS harness + absent spec-v3 corpus | root-ordinary.txt / root-ordinary-exit.txt |
| TinyGo package with process-local GOFLAGS=-buildvcs=false | Pass | tinygo-vcs-control.txt / tinygo-vcs-exit.txt |
| Unchanged main c95243aa selected staged corpus controls | Same missing corpus failure | main-staged-control.txt |
| Public runtime Fibonacci 20 and 30, private cache | 6765 / 832040 | fib-public-exec-private-cache.txt |
| Checked root suite, process-local VCS workaround | Fail: absent spec-v3 corpus only; all other packages pass | root-checked.txt / root-checked-exit.txt |
| Checked benchmark-module tests | Pass | bench-checked.txt / bench-checked-exit.txt |
| Final merged-source focused checked/profile + vet | Pass | final-merged-checks.txt |
| Final merged-source ARM64 checked/profile cross-build | Pass (compile only) | final-arm64-crossbuild.txt |
| Standalone self/constant/raw controls | Exit 0; changes retained | cli-control-status.json / *-report.json |
| Standalone unsupported mapped control | Exit 3; unknown + raw bytes retained | unknown-synthetic-report.json |
| Final inherited ARM64 encoder tests | Pass on AMD64 (encoding controls) | upstream-arm64-encoder-check.txt |
| Final focused just recipe | Pass: checked, profile/checked, vet | final-just-checks.txt |
| Documentation validator | Pass: 97 tracked Markdown files | final-docs-check.txt |
| Formatting recipe | Pass with private runtime temp path; initial sandbox temp path read-only | final-fmt.txt / final-fmt-sandbox-attempt.txt |

Saved text logs normalize insignificant tabs/trailing whitespace for Git review; numeric samples, errors and outcomes are retained.
