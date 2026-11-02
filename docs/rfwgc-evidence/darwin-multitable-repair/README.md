# Multi-table Darwin expectation repair

Native run41447853354 failed because this test selected rejection solely from GOOS=darwin after Darwin ARM64 gained opaque receipts. The predicate now selects rejection only when opaque hooks are absent on non-RV64 targets; RV64 retains its compact-reference positive path. Darwin ARM64 uses the same positive sum545, host hello and collection assertions as Linux. Unsupported adapters retain the before-write table-byte and host-token rejection assertions.

No Wasm bytes, deadlines, collection configuration or positive/rejection assertion changed. All three Linux AMD64 development runs pass the exact test, zero skips. Native Darwin positive follow-through remains pending. This changes only test selection, not production behavior or performance. Existing native failure and original raw receipts remain unchanged.
