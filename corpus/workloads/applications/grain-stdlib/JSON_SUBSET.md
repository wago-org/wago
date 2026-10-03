# JSON assertion subset: scope and source changes

This fixture is **not the complete upstream JSON suite**. It retains the
`Parse`, `ToString`, and `Lenses` modules from Grain commit
[`49829d7966b38b177291f7e91f5eb81c65ec07aa`](https://github.com/grain-lang/grain/blob/49829d7966b38b177291f7e91f5eb81c65ec07aa/compiler/test/stdlib/json.test.gr).
They contain 235 assertion sites (Parse 85, ToString 103, Lenses 47).
These exercise parsing values/numbers/Unicode/arrays/objects, serialization
formats and escaping, round trips, and JSON access/update lenses.

## Exact transformation

The upstream file has SHA-256
`ccc62b48964114b37beb67856eae1a5d950bbb3b065d4badcdc500a5ffff0a6e`.
The bundled `compiler/test/stdlib/json.test.gr` retains upstream lines 1–39
(the original copyright/license/provenance header, module name and imports)
and 177–1238 (all of Parse, ToString and Lenses), byte for byte. It replaces
lines 40–176, the **entire Validation module**, with a dated Wago modification
notice. No retained assertion or expected result is changed or disabled.
The manifest distinguishes bundled hashes from original upstream hashes.

The removed module includes both passing and failing examples attributed to
JSON_checker. Its redistribution grant was not verified. Excluding the whole
module also intentionally drops other validation assertions, rather than
claiming a narrowly filtered full-suite result. Removed data is not included
in the source archive, executable, patch, or documentation. The historical
attribution URL is retained in the original header as provenance only.

## Permissions and corresponding source

The retained test includes Maciej Hirsz's MIT notice for borrowed number tests;
that notice is preserved in its header and `THIRD_PARTY_NOTICES.md`. Grain's
compiler test tree is outside the separate stdlib MIT license, so the Grain
test contributions are conservatively distributed under the root LGPLv3
terms, with `COPYING.LESSER` and the incorporated GPLv3 text in `COPYING`.
The modified file prominently identifies the change and date.

The archive includes every source needed to rebuild the linked executable.
JSON adds `stdlib/{buffer,json,option,result,uint8}.gr` to the existing source
inventory. Those files are unchanged and covered by `stdlib/LICENSE` (MIT).
Their transitive runtime code and notices were already bundled for Array and
String. Existing third-party notices remain intact. These fixture terms do
not alter Wago's own license.

Extract `source.tar.gz` to inspect or modify the guest source, and use the
compiler invocation in `build.sh` to rebuild it. The build needs only that
archive and the separately obtained pinned official compiler; no full Grain
checkout or excluded input is required. The compiler uses `--release
--no-wasm-tail-call`; assertions remain enabled. `json-subset.wasm` is the
output of the bundled `json.test.gr`, not a renamed full-suite binary.
