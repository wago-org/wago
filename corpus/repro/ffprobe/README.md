# FFprobe ARM64 compiler blocker

The a-Shell release 0.1 `ffprobe.wasm` asset ID `35803134` (updated
2021-04-25) has SHA-256
`43143986a2cd207f809c82917c8ccc81e32708d26be9e1121509bef5ec89a342`.
It identifies itself as FFprobe `N-97747-gffae62d96c`. Its 0.25-second PCM
WAV fixture is `inputs/tone.wav`, SHA-256
`bc97571b85e6d48658007748614b48c2b303e6a7f2a534035e9a8be761934969`.

Wasmtime and wazero execute:

```sh
ffprobe.wasm -v error -show_entries stream=codec_name,sample_rate,channels,duration -of default=noprint_wrappers=1 -i pipe:0 < inputs/tone.wav
```

They output `pcm_s16le`, sample rate `8000`, one channel, and duration `N/A`
(streamed WAV). Wago on Darwin/arm64 fails at compile time:

```text
compile: arm64: function 7500: arm64: no GP register available after applying the transient register floor
```

No FFprobe workload is admitted until Wago can compile and execute it.
