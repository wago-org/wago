#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOCAL_BULK_PROOF=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=0 WAGO_PARITY_CORE_CODE_DIR=/tmp/small-mul-shift-off-core /tmp/parity-small-mul-shift.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/small-mul-shift-off-core.txt 2>&1
WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=0 WAGO_PARITY_CODE_DIR=/tmp/small-mul-shift-off-app /tmp/parity-small-mul-shift.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/small-mul-shift-off-app.txt 2>&1
for image in /tmp/small-mul-shift-off-core/*.bin; do cmp "$image" "/tmp/constant-shift-core-before/${image##*/}"; done
for image in /tmp/small-mul-shift-off-app/*.bin; do cmp "$image" "/tmp/store-immediate-probe/${image##*/}"; done
index=0
for variant in on off off on; do
 enabled=0
 if [ "$variant" = on ];then enabled=1;fi
 WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT="$enabled" /tmp/parity-small-mul-shift.test -test.run=^$ -test.bench='BenchmarkParity/(audio-biquad|bezier-tessellation|files-path-trie|map-point-segment|mesh-skinning|search-aho-corasick|search-bk-tree|video-optical-flow|video-yuv420)/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/small-mul-shift-affected-app-$index-$variant.txt" 2>&1
 for id in wago/coremark/2k-performance wago/drwav/pcm-decode-seek wago/fastfloat/decimal-parse wago/json-as-simd/serializeN wago/kissfft/complex-roundtrip wago/nanosvg/parse-structure wago/pcre2/compile-match wago/utf-as-simd/validateN wago/utf8proc/normalize-casefold wago/yyjson/parse-edit-write wago/zstd/decompress; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT="$enabled" /tmp/parity-small-mul-shift.test -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/small-mul-shift-affected-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
