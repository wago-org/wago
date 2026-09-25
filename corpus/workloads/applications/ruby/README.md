# Ruby 3.2.2 CLI

`ruby.wasm` is the slim Ruby 3.2.2 WASI asset from
`vmware-labs/webassembly-language-runtimes` release
`ruby/3.2.2+20230714-11be424`; its SHA-256 is
`de598f394e398763d2b147e3e51a6eeadf048128598ac4a3f992a97204c192b0`.
Ruby's COPYING/BSDL and the WLR Apache/NOTICE files are retained here.

`--disable-gems` avoids startup warnings from unbundled RubyGems files. The
interpreter reads a pinned script on stdin, groups five thousand integers,
and emits the same exact seventeen-bucket output as the Lua workload.
Wasmtime, Wago, and wazero agree on the output. The slim asset does not imply
that the full Ruby standard library has been packaged or tested.
