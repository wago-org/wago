# Dedicated Eden core qualification

Source68eba856, Go1.27.1 and patched TinyGo0.42.0. No performance measurements.
Default/Eden/coexisting-age2 compact cores, incompatible constructor rejection,
Eden race, default Wago/AMD64/nativeGC/wrapper/directcore and TinyGo Eden CORE pass.
Current compact runtime adapter remains unsupported for TinyGo; Eden has no native
runtime adapter at all and returns zero native snapshots/capabilities.

Initial tagged runs found a default-only footprint assertion applying to Eden
builds.68eba856 scopes that assertion to empty Eden state; the untagged default
assertion is unchanged. No GC semantic failure occurred. Initial full-suite and
TinyGo commands hit readonly default caches; reruns used shared writable GOPATH
and XDG_CACHE_HOME. Initial failures retained in *-first.log/status-first.txt.

Pinned spec-v3 corpus was copied from the established canonical checkout before
the default Wago suite. Qualification uses existing admission restrictions; it is
not proof of unrestricted WasmGC support. Randomized model qualification is pending
at this documentation checkpoint. Logs report test durations, not throughput.
