# PDFium invoice processing regression

`pdfium-8111.wasm` is the **unmodified 5,828,543-byte application** reported in
[issue #1013](https://github.com/wago-org/wago/issues/1013), downloaded from
[`klippa-app/go-pdfium` at `7d0f255`](https://github.com/klippa-app/go-pdfium/blob/7d0f255cd091ad18e4696cf0c7cf301657ef4b52/internal/pdfium_wasm/pdfium.wasm).
Its full SHA-256 is
`e6f77e01dce34de05ad62515114152da580b64c23871e3e3e2afef3f7bb88ade`.
`PROVENANCE.json` pins its source and the matching chromium/8111 license archive.
`LICENSE.pdfium`, `LICENSE.upstream`, and `licenses/` retain PDFium, packaging,
wrapper, and dependency notices. The license archive supplies notices, not a
replacement binary; the application remains the reporter's exact artifact.

`invoice.pdf` is a two-page invoice with forty line items, Helvetica text and
Flate-compressed content streams, built by the original `build_input.py` source.
The workload initializes PDFium, loads the document from guest-allocated memory,
checks its page count and dimensions, extracts every UTF-16 text line, and
renders each page to a 96-by-128 BGRA bitmap. Exact text and bitmap SHA-256 values
were captured independently with Node/V8 using `oracle.mjs`; Wago must match
`oracle.json` on both pages. This exercises PDF parsing, decompression, font
handling, text extraction and rasterization with the real application.

The host contract has no filesystem, network or interactive callbacks. It
provides an empty environment, `EBADF` for the first preopen query on fd 3, and
the memory-zero growth notification. Every other host call fails. Memory starts
at 289 pages with the module's declared 32,768-page maximum.

`TestPDFiumCorpus` in `src/wago/pdfium_corpus_test.go` verifies both input hashes
before compilation. It executes fresh and codec-reloaded artifacts with one and
four compilation workers, in an isolated subprocess with a hard 60-second
deadline. The test runs in the ordinary and register-checker package gates on
supported AMD64 and ARM64 platforms. This is a regression corpus entry; it does
not add a benchmark or change the quick/website benchmark profiles.

The old ARM64 compiler fails on local function index 3986, which is global
function index 4030 after the 44 imports. That function has 350,201 Wasm bytes
and emits over 1 MiB of native code. Early forward conditional branches exceed
the signed imm19 range; this is a function-size failure.

From the repository root:

```sh
go test ./src/wago -run '^TestPDFiumCorpus$' -count=1
go test -tags=wago_regalloccheck ./src/wago -run '^TestPDFiumCorpus$' -count=1
```

To reproduce the input and independent oracle, use the Python/zlib versions in
`PROVENANCE.json` and Node 24.21.0. Review any changed bytes or oracle before
accepting them:

```sh
python3 tests/corpus/regressions/pdfium/build_input.py
node tests/corpus/regressions/pdfium/oracle.mjs
```

The Node command prints its result; it does not overwrite the checked oracle.
To reacquire the Wasm, fetch the immutable source URL above and verify its full
SHA-256 before replacing the fixture. Do not minimize, relink or pad it.
