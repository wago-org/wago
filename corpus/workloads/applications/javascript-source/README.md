# Shared JavaScript source workload

`source.js` is a deterministic generated JavaScript application used by both
esbuild and Brotli. It contains thousands of distinct functions, classes,
objects, strings, and numeric constants so parsing/minification and compression
perform meaningful work on a roughly production-sized input instead of the
previous 1 KB smoke fixture.

Run `node generate.mjs` to regenerate `inputs/source.js`. The script compares generated
bytes with the committed fixture unless an explicit output path is supplied.
