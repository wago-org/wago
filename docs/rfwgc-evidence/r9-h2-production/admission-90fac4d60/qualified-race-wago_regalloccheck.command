go test -race -tags wago_regalloccheck ./src/wago -run \^TestR9\(H2\|Ownership\) -count=1 -timeout=3m 
