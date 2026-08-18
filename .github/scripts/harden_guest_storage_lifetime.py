#!/usr/bin/env python3
from pathlib import Path

# Bind callback-scoped GC refs to the exact GuestStorage view that created them.
p = Path("src/wago/guest_storage.go")
t = p.read_text()
t = t.replace(
    "type GuestGCRef struct{ ref gc.Ref }",
    "type GuestGCRef struct {\n\tref  gc.Ref\n\tview *guestStorageView\n}",
    1,
)
t = t.replace(
    "\tif slot == 0 {\n\t\treturn GuestGCRef{}, nil\n\t}",
    "\tif slot == 0 {\n\t\treturn GuestGCRef{view: v}, nil\n\t}",
    1,
)
t = t.replace("\treturn GuestGCRef{ref: ref}, nil", "\treturn GuestGCRef{ref: ref, view: v}, nil", 1)
needle = "\tif v.in.gc == nil || v.in.c == nil {\n\t\treturn GuestGCArrayInfo{}, 0, fmt.Errorf(\"wago: guest has no live Wasm GC collector\")\n\t}\n"
replacement = needle + "\tif ref.view != nil && ref.view != v {\n\t\treturn GuestGCArrayInfo{}, 0, fmt.Errorf(\"wago: GC reference belongs to a different guest-storage view: %w\", ErrPermissionDenied)\n\t}\n"
if needle not in t:
    raise SystemExit("gcArrayInfo collector guard not found")
t = t.replace(needle, replacement, 1)
# GCArrayRef has a second GuestGCRef construction.
old = "\treturn GuestGCRef{ref: value.Ref}, nil"
if old not in t:
    raise SystemExit("GCArrayRef return not found")nt = None
