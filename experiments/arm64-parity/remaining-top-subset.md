# Focused arm64 subset before product and select scheduling

Apple M4 Max. Historical snapshot before `crowded-products` and `select-source-read` were enabled. Newer FIR and ADPCM measurements are recorded in [the main report](../../steady-exec-wago-vs-w2c2.md). Median of two 150 ms execution samples with the optimizations retained at capture time. w2c2 values come from the recorded device baseline; this is a focused subset, not a refreshed full ranking. Times are microseconds.

| Corpus | Current Wago | Recorded w2c2 | Ratio |
|---|---:|---:|---:|
| audio-fir | 52.346 | 23.250 | 2.251× |
| audio-adpcm | 43.767 | 23.458 | 1.866× |
| serialization-protobuf | 24.363 | 13.833 | 1.761× |
| compiler-register-allocation | 16.181 | 9.916 | 1.632× |
| geo-point-in-polygon | 19.421 | 12.208 | 1.591× |
| vision-components | 35.409 | 23.084 | 1.534× |
| files-glob-match | 117.820 | 78.542 | 1.500× |
| ml-knn | 33.665 | 22.917 | 1.469× |
| language-register-vm | 9.683 | 6.875 | 1.408× |
| grid-pathfinding | 31.882 | 29.250 | 1.090× |
