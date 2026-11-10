#!/usr/bin/env python3
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

"""Pinned 0.42.0 threads-scheduler integration; invoked by patch-tinygo-042.sh."""
from pathlib import Path
import hashlib
import sys

root = Path(sys.argv[1])
source = Path(__file__).resolve().parent / 'tinygo-042/wago_foreign_stack.go.in'
changes = [
 ('src/internal/task/task_threads.go',
  'b30042e2a95668106bf6f369a6f02bf69fe6d3868abede4fca9d1a3dc7ec88f9',
  [('markRoots(t.state.stackBottom, t.state.stackTop)',
    'WagoScanStack(t.state.stackBottom, t.state.stackTop)'),
   ('\t// Scan all globals (implemented in the runtime).',
    '\twagoScanForeignStacks()\n\n\t// Scan all globals (implemented in the runtime).')]),
 ('src/runtime/gc_stack_threads.go', '9bf8ef482a20d34c02cb137c13b5daef30e67abb9f95dc8653c58b5391acad69',
  [('markRoots(sp, task.StackTop())', 'task.WagoScanStack(sp, task.StackTop())')]),
]
# Validate every input before writing anything. Accept only exact originals or
# exact patched outputs, making repeated CI bootstrap safe and fail-closed.
pending = []
for name, digest, replacements in changes:
 path = root / name
 text = path.read_text()
 original = text
 for old, new in reversed(replacements):
  if new in original:
   original = original.replace(new, old)
 if digest and hashlib.sha256(original.encode()).hexdigest() != digest:
  raise SystemExit(f'unexpected TinyGo source: {path}')
 patched = original
 for old, new in replacements:
  if patched.count(old) != 1:
   raise SystemExit(f'unexpected TinyGo anchor: {path}')
  patched = patched.replace(old, new)
 if text not in (original, patched):
  raise SystemExit(f'partially patched TinyGo source: {path}')
 pending.append((path, patched))
target = root / 'src/internal/task/wago_foreign_stack.go'
if target.exists() and target.read_bytes() != source.read_bytes():
 raise SystemExit(f'unexpected existing hook: {target}')
for path, text in pending:
 path.write_text(text)
target.write_bytes(source.read_bytes())
print('patch-tinygo-042: Wago foreign-stack GC integration present')
