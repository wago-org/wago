go test -p=1 -race -tags wago_gc_direct_trusted ./src/wago -run \^\(TestDirectGCMemoryImport\|TestDirectMemoryImportPolicy\) -count=1 
