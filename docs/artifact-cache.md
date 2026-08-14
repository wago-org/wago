# Artifact cache identity

The runtime CLI caches compiled `.wago` artifacts only when it can prove that
the compiler/runtime build and compilation configuration are compatible with
the cached native code. Cache misses and unavailable identity always fall back
to compilation.

## Build identity

Standard Go builds derive a fixed identity from the build information embedded
by the Go toolchain. The identity covers the Go version, command package, main
module, dependencies and replacements, build settings, target, build tags, and
clean VCS revision. Development builds are cacheable only when both a revision
and an explicit `vcs.modified=false` setting are present. Dirty trees and builds
without a versioned module or clean VCS identity disable the cache; they never
reuse an identity based only on a path or version placeholder.

Generated plugin runtimes call `runtime.MainWithArtifactCacheIdentity` with a
fresh SHA-256 identity embedded on every actual binary rebuild. Reusing an
unchanged plugin binary preserves its identity; rebuilding it creates a new
module-cache namespace even when mutable local replacements keep the same module
paths or versions. If the builder cannot obtain cryptographic randomness, it
embeds an empty identity and the generated runtime safely bypasses the artifact
cache. Custom runtime-CLI builders may use the same entry point with
an identity covering their generated code, plugin codegen ABI, lock state, and
build inputs. An explicit identity replaces automatic build metadata and must
change whenever native output or artifact interpretation can change.

Automatic identity also requires every dependency and replacement to have an
immutable module version and checksum. Filesystem replacements, development
dependencies, missing checksums, malformed dependency records, and excessive
replacement chains disable caching. Builders that intentionally use those
inputs must compute and supply their own content-aware identity.

## Configuration signature

The cache key hashes the build identity and source bytes together with numeric,
fixed-order configuration values:

- target GOOS and GOARCH;
- accepted Core feature bits;
- bounds-check and deferred-bounds policy;
- maximum memory pages;
- the top-level optimization objective; and
- the ordered compiler-optimization bit set.

Function-worker policy is intentionally excluded: it controls only bounded
compiler scheduling, while validation order, generated native code, and
serialized artifact bytes remain deterministic across worker counts. Cache
lookup validates and uses the destination runtime's effective configuration
before lookup or bypass decisions—the caller's compatibility parameter cannot
select code compiled under a different runtime policy, and a warm entry cannot
admit a configuration that a cold compile rejects.

One `PreparedCompile` owns each lookup. Source transforms run exactly once before
key selection, and compile observers see the same `CompilationIdentity` on cold
compilation and warm artifact adoption. Generations with source transforms or
custom compiler instructions currently bypass serialized reuse because those
plugin semantics do not yet expose a deterministic fingerprint. Observer-only
generations remain cacheable and still receive warm-hit events.

The compile-only GC native-code telemetry option bypasses the cache entirely
because that attribution is deliberately not serialized and a warm artifact
cannot satisfy the request. Signals-based compilation also bypasses the cache
because guard-page native code is intentionally nonserializable.

The binary key format uses version 1 and a domain prefix. Wago is unreleased, so
incompatible development encodings are folded into version 1 rather than consuming
new public format numbers. After the first release, changing the meaning or order
of any encoded field requires incrementing `cacheKeyFormat`. Optimization names
are not serialized into the hot cache-key path; their stable registry order is
interpreted under the build identity that owns that registry.

The final SHA-256 key remains hexadecimal in the filesystem path. Artifact
loading keeps its existing version, ABI, target, section-size, file-type, and
malformed-input checks; cache entries are streamed into bounded code/metadata
sections rather than first buffered as an unbounded whole file. After decoding,
the loader verifies EOF, the final opened-file size, and that the cache path still
names the same regular file, so concurrent replacement or growth remains a safe
miss. Cache corruption remains a safe miss followed by recompilation and atomic
replacement. Once an
artifact decodes successfully, runtime binding and `AfterCompile` policy errors
are returned directly rather than being converted into cache misses; the decoded
mapping is closed on that failure path.
Publication uses a unique same-directory temporary file and a
platform-specific replace-existing operation, including `MoveFileExW` on
Windows. Existing symlink, directory, and non-regular destinations are rejected;
failed writes leave the prior regular artifact intact and remove the temporary
file. Because cache entries are regenerable, publication does not promise
power-loss durability or sync the parent directory. Publication failures remain
best-effort for execution but are reported (or delivered to `Cache.ReportError`)
so a persistent miss is diagnosable.

## Validation

Run the focused correctness and allocation checks with:

```sh
go test ./cli/runtime/internal/artifactcache
go test ./cli/runtime
go test ./cli/runtime/internal/artifactcache -run '^$' -bench '^BenchmarkCachePath$' -benchmem
```

TinyGo runtime profiles also import this package, so changes to identity or key
construction must retain `tinygo test ./cli/runtime/internal/artifactcache`.
