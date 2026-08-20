#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: Path, old: str, new: str, label: str) -> None:
    text = path.read_text()
    if new in text:
        return
    if old not in text:
        raise SystemExit(f"{path}: {label} marker not found")
    path.write_text(text.replace(old, new, 1))


hostcall = Path("src/wago/hostcall.go")
replace_once(
    hostcall,
    "\treservation      *pluginOperationReservation\n\texactParams      []ValueTypeDescriptor\n\texactResults     []ValueTypeDescriptor\n",
    "\treservation       *pluginOperationReservation\n\texactParams       []ValueTypeDescriptor\n\texactResults      []ValueTypeDescriptor\n\tephemeralGCResults *gcHostTempTokens\n",
    "instanceHostModule ephemeral result tracker",
)
replace_once(
    hostcall,
    "\t\tcaller.exactParams = exactParams\n\t\tcaller.exactResults = exactResults\n\t\tdefer caller.scope.end(caller.generation, caller.parentGeneration)\n",
    "\t\tcaller.exactParams = exactParams\n\t\tcaller.exactResults = exactResults\n\t\tvar gcResultTemps gcHostTempTokens\n\t\tcaller.ephemeralGCResults = &gcResultTemps\n\t\tdefer gcResultTemps.release(in)\n\t\tdefer caller.scope.end(caller.generation, caller.parentGeneration)\n",
    "host dispatch ephemeral result lifetime",
)

alloc = Path("src/wago/guest_storage_alloc.go")
replace_once(
    alloc,
    "\tif resultIndex < 0 || resultIndex >= len(h.exactResults) {\n\t\treturn 0, fmt.Errorf(\"wago: host result index %d is out of range\", resultIndex)\n\t}\n",
    "\tif resultIndex < 0 || resultIndex >= len(h.exactResults) {\n\t\treturn 0, fmt.Errorf(\"wago: host result index %d is out of range\", resultIndex)\n\t}\n\tif h.ephemeralGCResults == nil {\n\t\treturn 0, fmt.Errorf(\"wago: GC result allocation requires the active host dispatch: %w\", ErrPermissionDenied)\n\t}\n\tif int(h.ephemeralGCResults.count) >= len(h.ephemeralGCResults.tokens) {\n\t\treturn 0, fmt.Errorf(\"wago: allocated GC host result count exceeds %d\", len(h.ephemeralGCResults.tokens))\n\t}\n",
    "allocator result tracker validation",
)
replace_once(
    alloc,
    "\tunlockNative := lockNativeExecutionForHostAccess()\n\tdefer unlockNative()\n",
    "\tunlockNative := h.in.lockInstanceNativeStateForHostAccess()\n\tdefer unlockNative()\n",
    "instance-aware native storage lock",
)
replace_once(
    alloc,
    "\treturn issueHostGCResultLocked(h.in, state, ref, required, localType, domainType)\n",
    "\ttoken, err := issueHostGCResultLocked(h.in, state, ref, required, localType, domainType)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\ttemps := h.ephemeralGCResults\n\ttemps.tokens[temps.count] = token\n\ttemps.count++\n\treturn token, nil\n",
    "track published allocator result token",
)

# Make the one-call token lifetime part of the public API contract.
text = alloc.read_text()
needle = "// fails, no public result token is created.\n"
addition = (
    "// fails, no public result token is created.\n"
    "//\n"
    "// The returned token is ephemeral to the active host call. Write it to the\n"
    "// corresponding result slot during that call. Wago releases allocator-created\n"
    "// result tokens after host-result translation has rooted the object for the\n"
    "// parked Wasm frame. Hosts MUST NOT retain or reuse the token.\n"
)
if "The returned token is ephemeral to the active host call" not in text:
    if needle not in text:
        raise SystemExit("guest_storage_alloc.go: public token lifetime comment marker not found")
    alloc.write_text(text.replace(needle, addition, 1))

# The synthetic allocator test supplies the same per-dispatch tracker used by
# newHostDispatch, verifies publication while live, and verifies automatic-style
# cleanup makes the token stale afterward.
test = Path("src/wago/guest_storage_gc_amd64_test.go")
replace_once(
    test,
    "\tcaller.exactResults = []ValueTypeDescriptor{{\n",
    "\tvar resultTemps gcHostTempTokens\n\tcaller.ephemeralGCResults = &resultTemps\n\tcaller.exactResults = []ValueTypeDescriptor{{\n",
    "test result tracker setup",
)
text = test.read_text()
old = """\tdefer func() {\n\t\tif err := in.ReleaseGCRef(GCRef{token: token}); err != nil {\n\t\t\tt.Errorf(\"release host GC result: %v\", err)\n\t\t}\n\t}()\n\n"""
if old in text:
    text = text.replace(old, "\tif resultTemps.count != 1 || resultTemps.tokens[0] != token {\n\t\tt.Fatalf(\"tracked host GC result = count %d token %#x, want 1/%#x\", resultTemps.count, resultTemps.tokens[0], token)\n\t}\n\n", 1)
    test.write_text(text)
elif "tracked host GC result" not in text:
    raise SystemExit("guest_storage_gc_amd64_test.go: manual release block not found")
text = test.read_text()
needle = """\tif err := caller.WithGuestStorage(func(storage GuestStorage) error {\n\t\tif _, err := storage.GCArrayInfo(retainedRef); err == nil || !strings.Contains(err.Error(), \"different guest-storage view\") {\n\t\t\treturn &guestStorageTestError{\"GC reference crossed guest-storage views\"}\n\t\t}\n\t\treturn nil\n\t}); err != nil {\n\t\tt.Fatal(err)\n\t}\n"""
addition = needle + """\tresultTemps.release(in)\n\tif resultTemps.count != 0 {\n\t\tt.Fatalf(\"released host GC result token count = %d, want 0\", resultTemps.count)\n\t}\n\tif err := in.ReleaseGCRef(GCRef{token: token}); err == nil || !strings.Contains(err.Error(), \"invalid or stale\") {\n\t\tt.Fatalf(\"released host GC result token remained live: %v\", err)\n\t}\n"""
if "released host GC result token remained live" not in text:
    if needle not in text:
        raise SystemExit("guest_storage_gc_amd64_test.go: final borrow marker not found")
    test.write_text(text.replace(needle, addition, 1))

# Document the temporary token contract for plugin authors.
docs = Path("docs/host-guest-storage.md")
text = docs.read_text()
needle = """After successful initialization, Wago publishes the new object through the same\nopaque GC-result token machinery used by other host GC results. Normal host\nresult translation then verifies the token against the import's exact result\ntype before Wasm receives the reference.\n"""
addition = needle + """\nThe returned `uint64` is an ephemeral host-result token. Write it to the\ncorresponding result slot during the same host call. Wago releases this\nallocator-created token after result translation has rooted the object for the\nparked Wasm frame. Do not retain or reuse the token. Ordinary `GCRef` tokens\nsupplied by other APIs keep their existing retained lifetime.\n"""
if "ephemeral host-result token" not in text:
    if needle not in text:
        raise SystemExit("docs/host-guest-storage.md: result token paragraph not found")
    docs.write_text(text.replace(needle, addition, 1))
