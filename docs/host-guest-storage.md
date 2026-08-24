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

## Quick start: GC arrays in a host function

Use this section when a plugin host function needs to receive, modify, or return
a Wasm GC array.

The main rule is simple: **never interpret a GC-reference `uint64` slot
itself.** Wago uses the slot as an opaque token.

```text
Wasm (ref $array)
       |
       v
params[n]                  opaque token
       |
       v
storage.GCRef(params[n])    checked callback handle
       |
       v
storage.GCArrayBytes(...)   checked array payload
```

The token, `GuestGCRef`, and directly borrowed payload all have a limited
lifetime. Do not retain them after the active callback or guest-storage borrow.

### Register the broad host ABI

A declarative plugin registers a GC-bearing import with the public reference ABI
category. The importing Wasm module can still select a concrete array type.

```go
func (p *plugin) Register(reg *wago.Registrar) error {
    imports, err := reg.HostImports()
    if err != nil {
        return err
    }
    module, err := imports.Module("example")
    if err != nil {
        return err
    }

    module.Func("fill", fill).
        Params(wago.ValAnyRef).
        Results(wago.ValI32)
    return nil
}
```

For example, Wasm can import that function with an exact caller-defined type
such as `(ref null $bytes)` where `$bytes` is `array (mut i8)`. Wago validates
the broad ABI and keeps the exact caller type in the active callback metadata.
The plugin does not create one global exact type for `fill`.

### Read or write a numeric GC array

Resolve the opaque parameter token inside `WithGuestStorage`. Then request the
array payload.

```go
func fill(m wago.HostModule, params, results []uint64) {
    storageModule, ok := m.(wago.GuestStorageHostModule)
    if !ok {
        panic(wago.HostTrap{Err: errors.New("guest storage unavailable")})
    }

    err := storageModule.WithGuestStorage(func(storage wago.GuestStorage) error {
        ref, err := storage.GCRef(params[0])
        if err != nil {
            return err
        }
        if ref.IsNull() {
            return errors.New("expected a non-null array")
        }

        payload, info, err := storage.GCArrayBytes(
            ref,
            wago.GuestStorageWrite,
        )
        if err != nil {
            return err
        }
        if info.Storage != wago.GuestGCArrayI8 {
            return errors.New("expected array<i8>")
        }

        n := copy(payload, []byte("hello"))
        results[0] = uint64(n)
        return nil
    })
    if err != nil {
        panic(wago.HostTrap{Err: err})
    }
}
```

For a mutable numeric array, `payload` aliases the real GC array storage. A
synchronous syscall can therefore write directly into it:

```go
n, err := unix.Read(fd, payload)
```

There is no required intermediate buffer and no raw pointer API. Keep the
syscall inside the active `WithGuestStorage` callback.

### Return a caller-typed GC array

Use `GuestGCArrayAllocatorHostModule` when the host must create a fresh array
whose concrete type comes from the importing Wasm module.

```go
func makeBytes(m wago.HostModule, _ []uint64, results []uint64) {
    allocator, ok := m.(wago.GuestGCArrayAllocatorHostModule)
    if !ok {
        panic(wago.HostTrap{Err: errors.New("GC result allocation unavailable")})
    }

    source := []byte("hello")
    token, err := allocator.NewGCArrayResult(
        0,
        uint32(len(source)),
        func(dst []byte, info wago.GuestGCArrayInfo) error {
            if info.Storage != wago.GuestGCArrayI8 {
                return errors.New("caller result must be array<i8>")
            }
            copy(dst, source)
            return nil
        },
    )
    if err != nil {
        panic(wago.HostTrap{Err: err})
    }

    results[0] = token
}
```

The first argument to `NewGCArrayResult` is the WebAssembly result index. It is
not the raw slot index. Wago reads that result's exact defined array type,
allocates it in the caller's GC domain, roots it, runs the initializer, and then
returns an ephemeral host-result token. Write that token to the result slot in
the same host call. Do not retain it.

The initializer can write an immutable caller-selected array because the new
object is still private and has not been published to Wasm.

### Which API should I use?

| Need | API |
|---|---|
| Inspect Memory32 vs Memory64 | `MemoryInfo` |
| Borrow a linear-memory range | `MemoryRange` |
| Resolve a GC parameter token | `GCRef` |
| Inspect a GC array | `GCArrayInfo` |
| Read or write a numeric or `v128` GC array | `GCArrayBytes` |
| Follow one reference-array element | `GCArrayRef` |
| Read the caller's exact parameter type | `ImportParamType` |
| Read the caller's exact result type | `ImportResultType` |
| Resolve a caller-defined type | `DefinedType` |
| Allocate a caller-typed numeric or `v128` GC array result | `NewGCArrayResult` |

### Common mistakes

Do not:

- cast a GC-reference `uint64` slot to an internal collector reference;
- retain a `GuestGCRef`, result token, or directly borrowed slice;
- use a `GuestGCRef` with a different `GuestStorage` view;
- request write access to an immutable published array;
- use `GCArrayBytes` for a reference array;
- re-enter Wasm while `WithGuestStorage` is active.

The remaining sections describe these rules and APIs in detail.

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

Every directly borrowed slice and `GuestGCRef` returned by `GuestStorage` is
valid only while the `WithGuestStorage` callback is active. Immutable GC-array
reads are detached copies rather than direct borrows. A handle is also bound to
its exact Runtime, calling instance, collector domain, and `GuestStorage` view;
using it with another view or after expiry fails closed.

The host MUST NOT retain a directly borrowed slice or `GuestGCRef` after the
callback returns. Wago also makes later method calls through the expired
`GuestStorage` fail closed.

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
tokens. Declarative Runtime plugin imports use temporary callback-scoped tokens;
other public APIs retain their documented token lifetime. In neither case may a
host interpret the slot as an internal `gc.Ref`, collector handle, object
address, or pointer.

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

Mutable arrays return a zero-copy alias for both read and write access. The
slice is the real collector payload, so synchronous code may pass it directly to
`read`, `pread`, `readv`, or similar syscall wrappers while the
`WithGuestStorage` borrow remains active. No intermediate copy and no raw pointer
API are required. Long blocking operations hold native execution and collector
mutation stable for the borrow; callers should prefer readiness polling and
non-blocking I/O when that hold would be excessive.

A write borrow of an immutable array fails. A read of an immutable array returns
a copy, because a Go `[]byte` cannot enforce read-only access to directly aliased
storage. Mutating that copy cannot change guest state. Hosts should account for
one allocation and a payload-sized copy when reading immutable arrays.

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

The returned `uint64` is an ephemeral host-result token. Write it to the
corresponding result slot during the same host call. Wago releases this
allocator-created token after result translation has rooted the object for the
parked Wasm frame. Do not retain or reuse the token. Ordinary `GCRef` tokens
supplied by other APIs keep their existing retained lifetime.

The initial allocation API intentionally supports numeric and `v128` array
payloads. Reference-array construction should use a future barrier-aware typed
initializer rather than exposing raw reference storage.

## Collector safety

Wago keeps native execution and the collector domain serialized for the complete
`WithGuestStorage` lifetime. A borrowed GC payload therefore does not move while
the host holds its slice. Guest re-entry, collection, memory growth, and other
operations that could invalidate a direct view are rejected during the borrow.

The public API never exposes:

- collector object addresses;
- backing pointers;
- collector handle bit layouts;
- GC metadata headers.

Hosts receive bounded slices and opaque callback-scoped references only.

This keeps moving-GC and storage-layout details private to Wago while permitting
zero-copy synchronous system interfaces.
