#!/usr/bin/env python3
from pathlib import Path

# Bind callback-scoped GC refs to the exact GuestStorage view that created them.
p = Path("src/wago/guest_storage.go")
t = p.read_text()
old = "type GuestGCRef struct{ ref gc.Ref }"
new = "type GuestGCRef struct {\n\tref  gc.Ref\n\tview *guestStorageView\n}"
if old not in t:
    raise SystemExit("GuestGCRef declaration not found")
t = t.replace(old, new, 1)
old = "\tif slot == 0 {\n\t\treturn GuestGCRef{}, nil\n\t}"
new = "\tif slot == 0 {\n\t\treturn GuestGCRef{view: v}, nil\n\t}"
if old not in t:
    raise SystemExit("null GCRef return not found")
t = t.replace(old, new, 1)
old = "\treturn GuestGCRef{ref: ref}, nil"
if old not in t:
    raise SystemExit("resolved GCRef return not found")
t = t.replace(old, "\treturn GuestGCRef{ref: ref, view: v}, nil", 1)
needle = "\tif v.in.gc == nil || v.in.c == nil {\n\t\treturn GuestGCArrayInfo{}, 0, fmt.Errorf(\"wago: guest has no live Wasm GC collector\")\n\t}\n"
replacement = needle + "\tif ref.view != nil && ref.view != v {\n\t\treturn GuestGCArrayInfo{}, 0, fmt.Errorf(\"wago: GC reference belongs to a different guest-storage view: %w\", ErrPermissionDenied)\n\t}\n"
if needle not in t:
    raise SystemExit("gcArrayInfo collector guard not found")
t = t.replace(needle, replacement, 1)
old = "\treturn GuestGCRef{ref: value.Ref}, nil"
if old not in t:
    raise SystemExit("GCArrayRef return not found")
t = t.replace(old, "\treturn GuestGCRef{ref: value.Ref, view: v}, nil", 1)
p.write_text(t)

# Explicit collection cannot run while a direct guest-storage view is active.
p = Path("src/wago/hostcall.go")
t = p.read_text()
old = "func (h staticHostModule) CollectGC() error { return h.in.CollectGC() }"
new = """func (h staticHostModule) CollectGC() error {
\tif h.in == nil {
\t\treturn fmt.Errorf(\"wago: GC host module has no instance\")
\t}
\tif state := h.in.pluginState.Load(); state != nil && state.guestStorageBorrow.Load() != 0 {
\t\treturn fmt.Errorf(\"wago: collection is unavailable while guest storage is borrowed: %w\", ErrPermissionDenied)
\t}
\treturn h.in.CollectGC()
}"""
if old not in t:
    raise SystemExit("staticHostModule CollectGC not found")
t = t.replace(old, new, 1)
needle = """func (h instanceHostModule) CollectGC() error {
\tif !h.valid() {
\t\treturn fmt.Errorf(\"wago: GC host module is outside its active callback: %w\", ErrPermissionDenied)
\t}
"""
replacement = needle + "\tif state := h.in.pluginState.Load(); state != nil && state.guestStorageBorrow.Load() != 0 {\n\t\treturn fmt.Errorf(\"wago: collection is unavailable while guest storage is borrowed: %w\", ErrPermissionDenied)\n\t}\n"
if needle not in t:
    raise SystemExit("instanceHostModule CollectGC not found")
t = t.replace(needle, replacement, 1)
p.write_text(t)

# Verify the explicit collection guard through a real callback-scoped HostModule.
p = Path("src/wago/guest_storage_test.go")
t = p.read_text()
needle = """\t\t\tif _, err := in.InvokeFromHost(context.Background(), m, \"peek\"); err == nil || !strings.Contains(err.Error(), \"guest storage is borrowed\") {
\t\t\t\treturn &guestStorageTestError{\"re-entry during borrow was not rejected\"}
\t\t\t}
"""
addition = needle + """\t\t\tgcModule, ok := m.(GCHostModule)
\t\t\tif !ok {
\t\t\t\treturn &guestStorageTestError{\"GC host module unavailable\"}
\t\t\t}
\t\t\tif err := gcModule.CollectGC(); err == nil || !strings.Contains(err.Error(), \"guest storage is borrowed\") {
\t\t\t\treturn &guestStorageTestError{\"collection during borrow was not rejected\"}
\t\t\t}
"""
if needle not in t:
    raise SystemExit("guest storage re-entry test block not found")
t = t.replace(needle, addition, 1)
p.write_text(t)

# A GuestGCRef must not cross from one storage view into another.
p = Path("src/wago/guest_storage_gc_amd64_test.go")
t = p.read_text()
t = t.replace("\tvar retained GuestStorage\n", "\tvar retained GuestStorage\n\tvar retainedRef GuestGCRef\n", 1)
needle = """\t\tref, err := storage.GCRef(token)
\t\tif err != nil {
\t\t\treturn err
\t\t}
"""
replacement = needle + "\t\tretainedRef = ref\n"
if needle not in t:
    raise SystemExit("GC ref test acquisition not found")
t = t.replace(needle, replacement, 1)
needle = """\tif _, ok := retained.ImportResultType(0); ok {
\t\tt.Fatal(\"expired GC guest-storage view still exposes import types\")
\t}
"""
replacement = needle + """\tif err := caller.WithGuestStorage(func(storage GuestStorage) error {
\t\tif _, err := storage.GCArrayInfo(retainedRef); err == nil || !strings.Contains(err.Error(), \"different guest-storage view\") {
\t\t\treturn &guestStorageTestError{\"GC reference crossed guest-storage views\"}
\t\t}
\t\treturn nil
\t}); err != nil {
\t\tt.Fatal(err)
\t}
"""
if needle not in t:
    raise SystemExit("expired GC view test block not found")
t = t.replace(needle, replacement, 1)
t = t.replace('import (\n\t"bytes"\n\t"testing"', 'import (\n\t"bytes"\n\t"strings"\n\t"testing"', 1)
p.write_text(t)
