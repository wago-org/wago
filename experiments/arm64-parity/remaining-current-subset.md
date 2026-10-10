# Focused arm64 gaps before shifted address lowering

Apple M4 Max, retained local optimizations including pure-select guards, before the shifted-address cover; 2026-10-09. Median of two 100 ms samples, signal bounds, exact results checked each invocation. w2c2 is the recorded device baseline. This refresh covers ten applications, not the full corpus. Times in microseconds.

| Corpus | Local Wago | Recorded w2c2 | Ratio |
|---|---:|---:|---:|
| geo-point-in-polygon | 22.437 | 12.208 | 1.838× |
| compiler-register-allocation | 17.675 | 9.916 | 1.782× |
| vision-components | 40.763 | 23.084 | 1.766× |
| ml-knn | 39.794 | 22.917 | 1.736× |
| files-glob-match | 131.451 | 78.542 | 1.674× |
| video-dct | 44.340 | 27.417 | 1.617× |
| audio-adpcm | 37.840 | 23.458 | 1.613× |
| audio-fir | 34.248 | 23.250 | 1.473× |
| graphics-reed-solomon | 120.925 | 87.792 | 1.377× |
| search-aho-corasick | 49.042 | 37.292 | 1.315× |
