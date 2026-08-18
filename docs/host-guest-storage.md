# Host guest-storage access

Wago host functions normally receive a `HostModule`. Its `Memory()` method is a
small compatibility convenience for memory 0.

Some system interfaces and language runtimes need more exact access. They can
need:

- a nonzero linear-memory index;
- the difference between Memory32 and Memory64;
- a checked slice of one memory instead of the complete memory;
- a Wasm GC array without copying it through linear memory;
- several GC-array views at once for scatter/gather validation;
- the exact structural type of a host import parameter or result;
- a fresh GC array whose concrete type was selected by the importing module.

Wago exposes those operations through optional callback-scoped interfaces. They
are general host APIs. They are not tied to a particular guest system interface.

[Facet](https://github.com/jtenner/facet-spec) is one motivating consumer. Its
[Facet 0.1 specification](https://github.com/jtenner/facet-spec/blob/main/SPEC.md)
uses explicit memory indexes, Memory64, and typed GC-array buffer facets.

## Callback scope

A synchronous host function can opt into `GuestStorageHostModule`:

```go
func host(m wago.HostModule, params, results []uint64) {
    storageModule, ok := m.(wago.GuestStorageHostModule)
    if !ok {
        panic(wago.HostTrap{Err: errors.New("guest storage unavailable")})
    }

    err := storageModule.WithGuestStorage(func(storage wago.GuestStorage) error {
        // Borrow guest storage here.
        return nil
    })
    if err != nil {
        panic(wago.HostTrap{Err: err})
    }
}
```

Every slice and `GuestGCRef` returned by `GuestStorage` is valid only while the
`WithGuestStorage` callback is active.

The host MUST NOT retain a slice or `GuestGCRef` after the callback returns.
Wago also makes later method calls through the expired `GuestStorage` fail
closed.

Only one guest-storage borrow can be active for one instance at a time. A nested
borrow fails instead of recursively taking runtime or collector locks.

## Re-entry

Wago rejects `Instance.InvokeFromHost` while a guest-storage borrow is active.

This rule is required for direct storage views. Re-entered Wasm could otherwise:

- grow a linear memory while a host slice refers to its previous bounds;
- allocate and trigger a moving collection while a host slice refers to a GC
  array payload.

End the `WithGuestStorage` callback before re-entering Wasm.

## Indexed linear memory

`MemoryInfo` operates in WebAssembly memory-index order:

```go
info, err := storage.MemoryInfo(index)
```

`GuestMemoryInfo.AddressType` is `GuestMemory32` or `GuestMemory64`. The value
comes from the module's actual memory declaration. A host can therefore reject a
Memory32-specific operation when the selected memory is Memory64, and vice
versa.

`GuestMemoryInfo.ByteLength` is the current byte length of the memory.

Use `MemoryRange` for data access:

```go
buf, err := storage.MemoryRange(
    memoryIndex,
    offset,
    length,
    wago.GuestStorageWrite,
)
```

Wago validates the memory index, unsigned range arithmetic, and the current
memory bounds before it returns the slice. The returned slice aliases guest
memory directly. Writes are visible to Wasm.

The API uses `uint64` offsets and lengths even for Memory32. The caller should
use `MemoryInfo.AddressType` when its external ABI distinguishes address widths.

## Wasm GC arrays

GC references in ordinary host-function parameter slots remain opaque Wago
public tokens. Do not interpret the slot as a collector handle or address.

Convert a GC-reference slot into a callback-scoped reference with:

```go
ref, err := storage.GCRef(params[0])
```

A zero slot becomes a null `GuestGCRef`. A nonzero token must be live, belong to
the calling instance, and resolve through its current collector root.

Inspect a dynamic array with:

```go
info, err := storage.GCArrayInfo(ref)
```

`GuestGCArrayInfo` reports:

- the dynamic element storage class;
- the array length;
- whether the array is mutable;
- the producer-local flattened defined-type index.

For numeric and `v128` arrays, `GCArrayBytes` returns the complete logical array
payload:

```go
bytes, info, err := storage.GCArrayBytes(
    ref,
    wago.GuestStorageRead,
)
```

The slice contains no collector header or allocation padding. Numeric storage is
exposed in the runtime's canonical little-endian in-memory representation.

A write borrow of an immutable array fails. A read borrow of an immutable array
is valid.

Reference arrays do not expose raw bytes. Use `GCArrayRef` instead:

```go
child, err := storage.GCArrayRef(outer, index)
```

This makes nested array-of-array interfaces possible without publishing an
intermediate retained GC token for each child. Multiple child views can stay
valid in one `WithGuestStorage` callback. A host can therefore validate every
selected child before it performs scatter/gather I/O.

## Exact import types

The callback view also preserves the importing module's structural signature:

```go
paramType, ok := storage.ImportParamType(0)
resultType, ok := storage.ImportResultType(0)
defined, ok := storage.DefinedType(resultType.Ref.Heap.TypeIndex)
```

These methods return `ValueTypeDescriptor` and `DefinedTypeDescriptor` values.
They preserve defined heap-type indexes, nullability, exactness, array storage,
and mutability instead of collapsing all GC references to `ValAnyRef`.

The indexes are local to the calling module's flattened type graph. Do not use a
type index from one instance with another instance.

## Exact GC-array result allocation

A host function that returns a caller-selected defined array type can opt into
`GuestGCArrayAllocatorHostModule`:

```go
allocator := m.(wago.GuestGCArrayAllocatorHostModule)
token, err := allocator.NewGCArrayResult(0, length,
    func(bytes []byte, info wago.GuestGCArrayInfo) error {
        copy(bytes, source)
        return nil
    })
results[0] = token
```

`resultIndex` identifies the WebAssembly result position, not the raw host ABI
slot. Wago reads the exact result heap type from the active import signature and
allocates that concrete array type.

The initializer runs while the new object is private, rooted, and protected from
relocation. This permits initialization of an immutable array before it becomes
observable. The initializer slice expires when the callback returns.

If initialization fails, Wago does not publish a result token.

After successful initialization, Wago publishes the new object through the same
opaque GC-result token machinery used by other host GC results. Normal host
result translation then verifies the token against the import's exact result
type before Wasm receives the reference.

The initial allocation API intentionally supports numeric and `v128` array
payloads. Reference-array construction should use a future barrier-aware typed
initializer rather than exposing raw reference storage.

## Collector safety

Wago keeps the collector domain serialized for the complete
`WithGuestStorage` lifetime. A borrowed GC payload therefore does not move while
the host holds its slice.

The public API never exposes:

- collector object addresses;
- backing pointers;
- collector handle bit layouts;
- GC metadata headers.

Hosts receive bounded slices and opaque callback-scoped references only.

This keeps moving-GC and storage-layout details private to Wago while permitting
zero-copy synchronous system interfaces.
