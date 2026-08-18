#!/usr/bin/env python3
from pathlib import Path

path = Path("src/wago/hostcall.go")
text = path.read_text()
original = text

# Preserve the exact structural import signature on the callback-scoped module.
marker = "type instanceHostModule struct {"
start = text.find(marker)
if start < 0:
    raise SystemExit("instanceHostModule declaration not found")
end = text.find("\n}", start)
if end < 0:
    raise SystemExit("instanceHostModule declaration end not found")
block = text[start:end]
if "exactParams" not in block:
    text = text[:end] + "\n\texactParams  []ValueTypeDescriptor\n\texactResults []ValueTypeDescriptor" + text[end:]

# Track one callback-scoped storage borrow. Nested borrows are rejected before
# acquiring native/collector locks.
needle = "\tinvocationID      invocationID\n"
if needle not in text:
    raise SystemExit("instancePluginState invocationID field not found")
if "guestStorageBorrow" not in text[text.find("type instancePluginState struct {"):text.find("type instanceCloseState struct {")]:
    text = text.replace(needle, needle + "\tguestStorageBorrow atomic.Uint32\n", 1)

# Re-entry while a host holds direct guest slices could grow memory or run a
# moving collection. Fail closed rather than deadlock or invalidate a view.
needle = "\tif active == nil || id == 0 {\n\t\treturn nil, fmt.Errorf(\"wago: re-entry requires the active host caller: %w\", ErrPermissionDenied)\n\t}\n"
replacement = needle + "\tif state := active.pluginState.Load(); state != nil && state.guestStorageBorrow.Load() != 0 {\n\t\treturn nil, fmt.Errorf(\"wago: re-entry is unavailable while guest storage is borrowed: %w\", ErrPermissionDenied)\n\t}\n"
if needle not in text:
    raise SystemExit("InvokeFromHost validation block not found")
if "re-entry is unavailable while guest storage is borrowed" not in text:
    text = text.replace(needle, replacement, 1)

# newHostDispatch already computes exactParams/exactResults from the importing
# module. Expose those immutable descriptors only for this callback generation.
needle = "\t\tcaller := in.beginHostCallScopeReservedWithID(invocation.id, invocation.reservation)\n"
replacement = needle + "\t\tcaller.exactParams = exactParams\n\t\tcaller.exactResults = exactResults\n"
if needle not in text:
    raise SystemExit("newHostDispatch caller construction not found")
if "caller.exactParams = exactParams" not in text:
    text = text.replace(needle, replacement, 1)

if text == original:
    raise SystemExit("hostcall.go already contains all guest-storage changes")
path.write_text(text)
