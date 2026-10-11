#!/bin/zsh
set -eu
export WAGO_SHARED_SCALAR=0
/tmp/paired-entry-dead-zeros -option entry-dead-zeros -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 1 -budget 10ms -code-dir /tmp/native-entry-dead-zeros-core > /tmp/entry-dead-zeros-core-first.jsonl
/tmp/paired-entry-dead-zeros -option entry-dead-zeros -workloads serialization-protobuf,language-register-vm,files-glob-match,audio-adpcm -rounds 1 -budget 10ms -code-dir /tmp/native-entry-dead-zeros-app > /tmp/entry-dead-zeros-app-first.jsonl
python3 /tmp/vector-next-entry-identity.py
/tmp/paired-entry-dead-zeros -option entry-dead-zeros -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 8 -budget 200ms > /tmp/entry-dead-zeros-core-exec.jsonl
/tmp/paired-entry-dead-zeros -option entry-dead-zeros -core-compile -phase compile -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 8 -budget 100ms > /tmp/entry-dead-zeros-core-compile.jsonl
/tmp/paired-entry-dead-zeros -option entry-dead-zeros -workloads serialization-protobuf,language-register-vm,files-glob-match,audio-adpcm -rounds 6 -budget 150ms > /tmp/entry-dead-zeros-app-exec.jsonl
/tmp/paired-entry-dead-zeros -option entry-dead-zeros -phase compile -workloads serialization-protobuf,language-register-vm,files-glob-match,audio-adpcm -rounds 6 -budget 100ms > /tmp/entry-dead-zeros-app-compile.jsonl
