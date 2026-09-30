# age 1.3.2 WASI commands

`age.wasm` and `age-keygen.wasm` are built from [age v1.3.2](https://github.com/FiloSottile/age/releases/tag/v1.3.2)
at commit `b74dce4cdbe35b5e5f66c06d9612b72f89028758` with Go 1.27.0.
Run `bash build.sh /path/to/clean/age-checkout` and compare both SHA-256 values
with `corpus/catalog.json`. The source's `go.sum` pins transitive dependencies;
their license files and the Go toolchain license are included in `licenses/`.

The identity in `inputs/test-identity.txt` and `keygen-inputs/test-identity.txt`
is deliberately **public test data**. Never use it to protect real content.
The pinned ciphertext was made from
`corpus/workloads/applications/esbuild/inputs/source.js` with the matching
public recipient; encryption is randomized, so the corpus checks deterministic
decryption and public-key derivation instead of ciphertext byte equality.
Wasmtime independently produced both exact output hashes.
