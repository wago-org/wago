#!/usr/bin/env bash
# TinyGo 0.42.0 shipped with a duplicate task-exit symbol when using
# -scheduler=tasks. Apply upstream tinygo-org/tinygo#5656 to its installed
# source tree until a release containing the fix is available.
set -euo pipefail

case "$(tinygo version)" in
  'tinygo version 0.42.0 '*) ;;
  *) echo 'patch-tinygo-042: expected TinyGo 0.42.0' >&2; exit 1 ;;
esac

tinygo_root=$(tinygo env TINYGOROOT)
if command -v python3 >/dev/null 2>&1; then
  python_cmd=python3
elif command -v python >/dev/null 2>&1; then
  python_cmd=python
else
  echo 'patch-tinygo-042: Python 3 is required to patch the toolchain' >&2
  exit 1
fi
"$python_cmd" - "$tinygo_root" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
changes = (
    (root / "src/internal/task/task_threads.c",
     "void tinygo_task_exit(void) {", "void tinygo_task_exit_thread(void) {"),
    (root / "src/internal/task/task_threads.go",
     "//go:linkname tinygo_task_exit tinygo_task_exit\n",
     "//go:linkname tinygo_task_exit tinygo_task_exit_thread\n"),
)
for path, old, new in changes:
    source = path.read_text()
    if source.count(new) == 1 and old not in source:
        continue
    if source.count(old) != 1 or new in source:
        raise SystemExit(f"patch-tinygo-042: unexpected contents in {path}")
    path.write_text(source.replace(old, new, 1))
print("patch-tinygo-042: upstream task-exit fix present")
PY
