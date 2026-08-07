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
- function-worker policy; and
- the ordered compiler-optimization bit set.

The binary key format has an explicit version and domain prefix. Changing the
meaning or order of any encoded field requires incrementing `cacheKeyFormat`.
Optimization names are not serialized into the hot cache-key path; their stable
registry order is interpreted under the build identity that owns that registry.

The final SHA-256 key remains hexadecimal in the filesystem path. Artifact
loading keeps its existing version, ABI, target, and malformed-input checks;
cache corruption remains a safe miss followed by recompilation and atomic
replacement.

## Validation

Run the focused correctness and allocation checks with:

```sh
go test ./cli/runtime/internal/artifactcache
go test ./cli/runtime
go test ./cli/runtime/internal/artifactcache -run '^$' -bench '^BenchmarkCachePath$' -benchmem
```

TinyGo runtime profiles also import this package, so changes to identity or key
construction must retain `tinygo test ./cli/runtime/internal/artifactcache`.
