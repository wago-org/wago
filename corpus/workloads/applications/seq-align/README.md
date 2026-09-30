# seq-align WebAssembly commands

These are the Biowasm builds of `lcs`, `needleman_wunsch`, and
`smith_waterman` from `noporpoise/seq-align` revision `dc41988`. The upstream
README declares the code public domain.

`build.sh` verifies each published module and expands its minified imports and
exports to the shared Emscripten command host names. The script generates and
checks every exact command output with the published JavaScript glue under
Node/V8, using `corpus/tools/emscripten-v8-oracle.mjs`.
