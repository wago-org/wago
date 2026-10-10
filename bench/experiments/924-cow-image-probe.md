# #924: initialized memory image — bounded OS oracle, runtime integration pending

Base: `origin/main` `209e448c392510a0325d5b282a0d86a776fb379c`. No Wago production behavior changed in this branch.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^(TestCOWImageCoverage|TestCOWImagePrivateMapping|TestCOWImageMultiInstancePSS)$' -count=1 -v` surveys six real modules and builds temporary file-backed images for SQLite and PHP. Every surveyed module has one owned, unshared memory32 and constant-offset active data. None of their active spans overlap in this sample; write order still needs a general overlap oracle for a production path.

| Module | Active segments | Source payload bytes | Active 4 KiB pages | Highest initialized byte |
| --- | ---: | ---: | ---: | ---: |
| SQLite | 412 | 123,227 | 32 | 1,178,403 |
| QuickJS | 214 | 126,461 | 32 | 1,177,539 |
| jq | 407 | 389,688 | 97 | 1,443,883 |
| PHP | 63,770 | 3,203,245 | 2,418 | 9,907,883 |
| Lua | 3 | 35,844 | 10 | 36,880 |
| yyjson | 59 | 15,655 | 5 | 82,423 |

The test-only oracle creates one ordered image file, maps it with `MAP_PRIVATE` twice, checks the original bytes at every segment, writes one mapping, and verifies the sibling and backing file remain unchanged. It passes for SQLite and PHP. Mapping-specific `/proc/self/smaps` PSS after reading every active page is directional: SQLite 1/10/100 live instances **128/120/100 KiB** (32 active pages), PHP 1/10 instances **9,672/9,670 KiB** (2,418 pages). PSS can fluctuate with kernel page accounting and concurrent process activity; the near-constant result is consistent with shared clean image pages. Virtual mapping lengths still scale: PHP 9.9 MiB per instance. The compiled module's input bytes, image file/page cache, other Wago mappings, and total process PSS are outside this mapping-only figure.

`GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^$' -bench '^BenchmarkCOWImagePrototype$' -benchtime=50ms -count=2 -benchmem` compares isolated image operations. SQLite: private map only **4.3/4.5 µs**, map plus read all 32 active pages **11.1/11.0 µs**, map plus one first write **10.0/10.5 µs**, fresh buffer and segment copies **55.6/52.0 µs**. PHP: map only **7.9/7.9 µs**, map plus read all 2,418 active pages **501/487 µs**, map plus one first write **9.7/10.1 µs**, fresh buffer and 63,770 segment copies **1,318/1,460 µs**. Map cases report 0 Go B/op and 0 Go allocs/op because the mapped pages are off heap; copy cases allocate approximately 1.18 MiB (SQLite) or 9.91 MiB (PHP) per operation. Image file creation, compilation, source retention, actual Wago instance setup, memory growth, guard layout, and cleanup are excluded. The copy loop is a proxy for active initialization, not a measured current Wago instantiation baseline.

The [Wasmtime article](https://bytecodealliance.org/articles/wasmtime-10-performance) describes its own CoW instance allocator; these standalone results cannot be attributed to Wago. Wago's memory ownership, reservation, guard, growth, mapping cache, imported/shared/multiple memory fallback, and partial failure paths still need a production implementation and correctness suite. Native code size and compilation B/op are unchanged on this branch. Peak/retained *total* process memory and first-write costs across all active pages are not measured.

Recommendation: keep draft as a promising feasibility probe. Do not promote until a real Wago mapped-memory variant shows proportional **total PSS** savings and preserves all memory semantics at 1/10/100 instances.
