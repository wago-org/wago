set working-directory := '..'

# Verify pinned upstream sources and exact WABT artifacts.
[default]
check upstream=env('REGRESSION_UPSTREAM', '.tmp/regression-corpus-upstream') converter=env('WAST2JSON', 'wast2json'):
    go run ./tests/tools/regression-corpus -repo '{{ justfile_directory() }}' -upstream '{{ upstream }}' -wast2json '{{ converter }}'

# Fetch the pinned upstream revision and refresh regression artifacts.
sync upstream=env('REGRESSION_UPSTREAM', '.tmp/regression-corpus-upstream') converter=env('WAST2JSON', 'wast2json'):
    go run ./tests/tools/regression-corpus -repo '{{ justfile_directory() }}' -upstream '{{ upstream }}' -wast2json '{{ converter }}' -fetch -write

# Repeat lifecycle, optimizer, guard, and fuzz tests.
stress:
    tests/scripts/regression-stress.sh

# Rebuild all checked PolyBench/C kernels.
build-polybench sdk=env('WASI_SDK', '/opt/wasi-sdk'):
    WASI_SDK='{{ sdk }}' corpus/build/polybench.sh
