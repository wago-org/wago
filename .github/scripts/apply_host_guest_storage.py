#!/usr/bin/env python3
from pathlib import Path

plugin = Path("docs/plugin-api.md")
text = plugin.read_text()
section = """

## Direct guest storage from host imports

A synchronous host function that needs more than the `HostModule.Memory()`
memory-0 convenience can opt into callback-scoped guest storage.

`GuestStorageHostModule.WithGuestStorage` provides checked access to arbitrary
linear-memory indexes, Memory32/Memory64 metadata, Wasm GC arrays, nested GC
array references, and the importing module's exact structural parameter/result
types. `GuestGCArrayAllocatorHostModule.NewGCArrayResult` allocates the exact
caller-selected numeric or `v128` array result type and initializes it before
publication.

Every borrowed slice and callback-scoped GC reference expires when the storage
callback returns. Wago rejects Wasm re-entry while a direct guest-storage borrow
is active so memory growth or moving collection cannot invalidate a live host
view.

See [Host guest-storage access](host-guest-storage.md) for the complete API,
lifetime rules, and examples. [Facet](https://github.com/jtenner/facet-spec) is
one motivating consumer, but these interfaces are general Wago host APIs.
"""
if "## Direct guest storage from host imports" not in text:
    plugin.write_text(text.rstrip() + section + "\n")

features = Path("FEATURES.md")
text = features.read_text()
section = """

## Callback-scoped host guest storage

Synchronous host imports can use optional callback-scoped APIs for zero-copy
access to indexed linear memory and Wasm GC arrays. The API reports Memory32
versus Memory64, bounds linear-memory ranges, preserves exact structural import
types, supports nested GC-array traversal, and can allocate an exact
caller-selected numeric or `v128` GC-array result.

Direct views cannot outlive the host callback. Wago serializes collector/native
mutation while a view is active and rejects Wasm re-entry during the borrow.
See [`docs/host-guest-storage.md`](docs/host-guest-storage.md).
"""
if "## Callback-scoped host guest storage" not in text:
    features.write_text(text.rstrip() + section + "\n")
