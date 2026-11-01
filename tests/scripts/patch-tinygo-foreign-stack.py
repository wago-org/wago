#!/usr/bin/env python3
"""Exercise pinned bootstrap guards against an installed patched TinyGo root."""
import hashlib
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

repo = Path(__file__).resolve().parents[2]
root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(subprocess.check_output(['tinygo', 'env', 'TINYGOROOT'], text=True).strip())
patcher = repo / 'scripts/patch-tinygo-foreign-stack.py'
files = ['src/internal/task/task_threads.go', 'src/runtime/gc_stack_threads.go', 'src/internal/task/wago_foreign_stack.go']
with tempfile.TemporaryDirectory(prefix='wago-bootstrap-test-') as directory:
 tree = Path(directory)
 for name in files:
  target = tree / name
  target.parent.mkdir(parents=True, exist_ok=True)
  shutil.copyfile(root / name, target)
 def snapshot():
  return {name: hashlib.sha256((tree/name).read_bytes()).hexdigest() for name in files}
 def run(success):
  p = subprocess.run([sys.executable, str(patcher), str(tree)], capture_output=True, text=True)
  assert (p.returncode == 0) == success, p.stdout+p.stderr
 before = snapshot()
 run(True)
 run(True)
 assert snapshot() == before, 'idempotent bootstrap changed files'
 for name in files:
  path = tree/name
  original = path.read_bytes()
  path.write_bytes(original+b'\n// incompatible local change\n')
  changed = snapshot()
  run(False)
  assert snapshot() == changed, 'failed bootstrap wrote files'
  path.write_bytes(original)
 # One replaced call without the second scan insertion is rejected.
 path = tree/files[0]
 original = path.read_text()
 path.write_text(original.replace('WagoScanStack(t.state.stackBottom, t.state.stackTop)', 'markRoots(t.state.stackBottom, t.state.stackTop)'))
 changed = snapshot()
 run(False)
 assert snapshot() == changed
 path.write_text(original)
 # Reconstruct the exact pristine inputs and verify first application too.
 path.write_text(original.replace('WagoScanStack(t.state.stackBottom, t.state.stackTop)', 'markRoots(t.state.stackBottom, t.state.stackTop)').replace('\twagoScanForeignStacks()\n\n', ''))
 path = tree/files[1]
 path.write_text(path.read_text().replace('task.WagoScanStack(sp, task.StackTop())', 'markRoots(sp, task.StackTop())'))
 (tree/files[2]).unlink()
 run(True)
 assert snapshot() == before
print('PASS: pristine apply, idempotence, source/hook drift and partial patch rejection')
