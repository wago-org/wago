# Conservative sparse cleanup threshold sensitivity

Candidate `d63d0dc4`: switch to full clear at max(1, bitmap words/8). Same baseline4327a2eb binary and identical benchmark bodies as initial run. Go1.27.1, GOMAXPROCS1, five alternating process pairs,300ms per exact shape. All50measurements and candidate collector tests passed; no missing rows. Zero B/op and allocs/op. Previous negative results remain preserved.

| Shape | Baseline median ns | Candidate median ns | Ratio | Baseline samples | Candidate samples |
|---|---:|---:|---:|---|---|
| production/live0/fullfalse | 8810.0 | 33.9 | 0.004× | [9083.0, 7366.0, 8810.0, 9466.0, 8434.0] | [29.28, 33.91, 33.6, 39.43, 33.9] |
| production/live4096/fulltrue | 316137.0 | 322465.0 | 1.020× | [301794.0, 342133.0, 316137.0, 332530.0, 313731.0] | [266863.0, 301617.0, 322465.0, 330712.0, 332746.0] |
| production/live64/fulltrue | 124998.0 | 105569.0 | 0.845× | [117307.0, 130503.0, 108269.0, 129706.0, 124998.0] | [105569.0, 112776.0, 101723.0, 101311.0, 110351.0] |
| small/live0/fulltrue | 3348.0 | 2807.0 | 0.838× | [3348.0, 3626.0, 3081.0, 5742.0, 2834.0] | [2374.0, 2571.0, 2899.0, 2932.0, 2807.0] |
| small/live4096/fulltrue | 196539.0 | 184484.0 | 0.939× | [180047.0, 183218.0, 196539.0, 289280.0, 198698.0] | [211658.0, 181330.0, 176561.0, 187085.0, 184484.0] |

Production4096Full still regresses2.0% at median despite the contiguous fallback; therefore not a clean dense-control pass and not promoted. Other tested shapes improve, but threshold was selected after observing prior data and is not proven universally optimal. Minor0 measures empty-young collection, not application throughput. No end-to-end/V8 claim.
