#!/usr/bin/env python3
# One-shot layout repair for the guest-storage borrow flag.
from pathlib import Path

path = Path("src/wago/hostcall.go")
text = path.read_text()
old = """\tinvocationID       invocationID
\tguestStorageBorrow atomic.Uint32
\tclose              atomic.Pointer[instanceCloseState]
\tgcConfig           *GCConfig
\torigin             InstantiateOrigin
\tgcGlobalRootCount  uint8
\tgcPublic           atomic.Pointer[gcPublicState]
"""
new = """\tinvocationID       invocationID
\tclose              atomic.Pointer[instanceCloseState]
\tgcConfig           *GCConfig
\torigin             InstantiateOrigin
\tgcGlobalRootCount  uint8
\tguestStorageBorrow atomic.Uint32
\tgcPublic           atomic.Pointer[gcPublicState]
"""
if old not in text:
    raise SystemExit("instancePluginState guest-storage field layout not found")
path.write_text(text.replace(old, new, 1))
