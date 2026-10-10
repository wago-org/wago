# Focused arm64 gaps after streaming reductions

Apple M4 Max, signal bounds. This subset measures retained source before the direct-shift-source experiment. Four 100 ms samples per workload, with exact results checked on every invocation. Historical w2c2 values come from the recorded-main report; they were not remeasured alongside these runs. This is a five-workload subset, not a full ranking.

| Workload | Wago steady µs | Recorded w2c2 µs | Ratio |
| --- | ---: | ---: | ---: |
| compiler-register-allocation | 17.659 | 9.916 | 1.781× |
| geo-point-in-polygon | 21.418 | 12.208 | 1.754× |
| vision-components | 38.436 | 23.084 | 1.665× |
| video-dct | 43.176 | 27.417 | 1.575× |
| audio-adpcm | 36.829 | 23.458 | 1.570× |

Raw samples: `direct-shift-0-off.txt`, `direct-shift-3-off.txt`. Timing varies between processes; use paired experiments to judge a change.
