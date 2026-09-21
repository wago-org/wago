# Fixed-work results

All values below use 20 matched process pairs. Means and paired mean differences are shown; intervals are the prespecified paired 95% bootstrap. B means bytes; KiB means 1,024 bytes. These datasets are separate sessions. Normal endpoints contain no explicit GC or scavenging.

## historical-full-context

| Workload / API / mode / endpoint | Metric | Baseline mean | Candidate mean | Change | 95% interval |
| --- | --- | ---: | ---: | ---: | --- |
| cjson/raw/scavenge/closed-normal | HeapAlloc (B) | 1,793,981.6 | 1,763,699.6 | -30,282.0 (-1.69%) | -574,808.4 to +437,904.0 |
| cjson/raw/scavenge/closed-normal | RSS_KiB (KiB) | 25,458.8 | 25,215.8 | -243.0 (-0.95%) | -625.6 to +161.4 |
| cjson/raw/scavenge/released-gc | HeapAlloc (B) | 696,800.0 | 699,801.6 | +3,001.6 (+0.43%) | +1,385.6 to +4,546.4 |
| cjson/raw/scavenge/released-gc | RSS_KiB (KiB) | 21,639.2 | 21,483.2 | -156.0 (-0.72%) | -604.0 to +340.8 |
| minimal-wasi/raw/scavenge/closed-normal | HeapAlloc (B) | 1,805,211.6 | 2,299,000.0 | +493,788.4 (+27.35%) | +163,712.4 to +765,729.2 |
| minimal-wasi/raw/scavenge/closed-normal | RSS_KiB (KiB) | 24,745.8 | 25,302.8 | +557.0 (+2.25%) | -26.0 to +1,137.8 |
| minimal-wasi/raw/scavenge/released-gc | HeapAlloc (B) | 696,590.0 | 699,192.0 | +2,602.0 (+0.37%) | +490.4 to +4,560.8 |
| minimal-wasi/raw/scavenge/released-gc | RSS_KiB (KiB) | 21,263.2 | 21,646.6 | +383.4 (+1.80%) | -210.8 to +977.0 |
| tinyxml2/raw/scavenge/closed-normal | HeapAlloc (B) | 1,936,424.4 | 2,443,233.6 | +506,809.2 (+26.17%) | +120,928.4 to +839,911.2 |
| tinyxml2/raw/scavenge/closed-normal | RSS_KiB (KiB) | 25,182.2 | 25,379.0 | +196.8 (+0.78%) | -410.0 to +830.4 |
| tinyxml2/raw/scavenge/released-gc | HeapAlloc (B) | 694,134.0 | 696,488.0 | +2,354.0 (+0.34%) | +802.0 to +4,016.4 |
| tinyxml2/raw/scavenge/released-gc | RSS_KiB (KiB) | 21,385.2 | 21,702.2 | +317.0 (+1.48%) | -283.8 to +931.4 |

## historical-memory-session1

| Workload / API / mode / endpoint | Metric | Baseline mean | Candidate mean | Change | 95% interval |
| --- | --- | ---: | ---: | ---: | --- |
| cjson/provider/gc/post | HeapAlloc (B) | 667,508.4 | 672,700.4 | +5,192.0 (+0.78%) | +3,588.0 to +6,745.2 |
| cjson/provider/gc/post | RSS_KiB (KiB) | 25,103.6 | 25,235.4 | +131.8 (+0.53%) | -477.6 to +816.4 |
| cjson/provider/normal/released | HeapAlloc (B) | 1,894,164.8 | 1,872,380.8 | -21,784.0 (-1.15%) | -53,732.0 to +8,747.2 |
| cjson/provider/normal/released | RSS_KiB (KiB) | 25,006.4 | 25,126.2 | +119.8 (+0.48%) | -483.8 to +709.8 |
| cjson/provider/scavenge/post | HeapAlloc (B) | 668,836.8 | 673,067.2 | +4,230.4 (+0.63%) | +2,060.0 to +6,322.8 |
| cjson/provider/scavenge/post | RSS_KiB (KiB) | 21,506.6 | 22,004.4 | +497.8 (+2.31%) | -128.4 to +1,142.6 |
| cjson/raw/gc/post | HeapAlloc (B) | 625,482.4 | 627,810.8 | +2,328.4 (+0.37%) | +854.4 to +3,867.2 |
| cjson/raw/gc/post | RSS_KiB (KiB) | 24,732.2 | 24,667.6 | -64.6 (-0.26%) | -691.4 to +528.8 |
| cjson/raw/normal/released | HeapAlloc (B) | 2,572,147.6 | 1,179,180.8 | -1,392,966.8 (-54.16%) | -1,890,807.6 to -848,855.2 |
| cjson/raw/normal/released | RSS_KiB (KiB) | 24,887.8 | 24,909.8 | +22.0 (+0.09%) | -698.2 to +762.8 |
| cjson/raw/scavenge/post | HeapAlloc (B) | 625,349.6 | 627,615.2 | +2,265.6 (+0.36%) | +668.8 to +3,896.0 |
| cjson/raw/scavenge/post | RSS_KiB (KiB) | 21,484.6 | 21,368.6 | -116.0 (-0.54%) | -723.2 to +509.2 |
| minimal-wasi/provider/gc/post | HeapAlloc (B) | 551,344.4 | 556,102.4 | +4,758.0 (+0.86%) | +3,310.4 to +6,196.4 |
| minimal-wasi/provider/gc/post | RSS_KiB (KiB) | 25,216.8 | 24,925.8 | -291.0 (-1.15%) | -869.8 to +311.0 |
| minimal-wasi/provider/normal/released | HeapAlloc (B) | 1,538,450.0 | 1,532,099.6 | -6,350.4 (-0.41%) | -7,989.2 to -4,795.6 |
| minimal-wasi/provider/normal/released | RSS_KiB (KiB) | 25,297.6 | 24,681.0 | -616.6 (-2.44%) | -1,324.8 to +80.6 |
| minimal-wasi/provider/scavenge/post | HeapAlloc (B) | 551,867.6 | 556,790.8 | +4,923.2 (+0.89%) | +3,812.8 to +6,063.2 |
| minimal-wasi/provider/scavenge/post | RSS_KiB (KiB) | 21,486.4 | 21,468.2 | -18.2 (-0.08%) | -776.2 to +719.4 |
| minimal-wasi/raw/gc/post | HeapAlloc (B) | 490,956.4 | 494,132.4 | +3,176.0 (+0.65%) | +1,428.8 to +4,918.4 |
| minimal-wasi/raw/gc/post | RSS_KiB (KiB) | 24,350.4 | 24,047.2 | -303.2 (-1.25%) | -989.4 to +395.4 |
| minimal-wasi/raw/normal/released | HeapAlloc (B) | 2,885,224.4 | 1,524,789.2 | -1,360,435.2 (-47.15%) | -1,607,675.2 to -1,059,861.6 |
| minimal-wasi/raw/normal/released | RSS_KiB (KiB) | 24,651.8 | 24,707.0 | +55.2 (+0.22%) | -807.8 to +844.4 |
| minimal-wasi/raw/scavenge/post | HeapAlloc (B) | 492,118.0 | 495,286.0 | +3,168.0 (+0.64%) | +1,511.2 to +4,799.2 |
| minimal-wasi/raw/scavenge/post | RSS_KiB (KiB) | 20,692.2 | 20,380.2 | -312.0 (-1.51%) | -722.2 to +87.8 |
| tinyxml2/provider/gc/post | HeapAlloc (B) | 685,579.6 | 689,982.0 | +4,402.4 (+0.64%) | +2,563.6 to +6,242.4 |
| tinyxml2/provider/gc/post | RSS_KiB (KiB) | 25,125.0 | 25,016.6 | -108.4 (-0.43%) | -843.0 to +585.4 |
| tinyxml2/provider/normal/released | HeapAlloc (B) | 3,147,634.4 | 3,198,732.8 | +51,098.4 (+1.62%) | -47,110.4 to +146,629.6 |
| tinyxml2/provider/normal/released | RSS_KiB (KiB) | 25,302.6 | 25,267.6 | -35.0 (-0.14%) | -563.6 to +522.0 |
| tinyxml2/provider/scavenge/post | HeapAlloc (B) | 684,957.2 | 691,174.8 | +6,217.6 (+0.91%) | +4,510.4 to +7,810.4 |
| tinyxml2/provider/scavenge/post | RSS_KiB (KiB) | 22,142.2 | 21,614.2 | -528.0 (-2.38%) | -1,074.4 to -52.0 |
| tinyxml2/raw/gc/post | HeapAlloc (B) | 624,856.0 | 626,782.8 | +1,926.8 (+0.31%) | +287.6 to +3,661.6 |
| tinyxml2/raw/gc/post | RSS_KiB (KiB) | 24,917.0 | 24,561.0 | -356.0 (-1.43%) | -872.0 to +196.0 |
| tinyxml2/raw/normal/released | HeapAlloc (B) | 1,459,458.4 | 2,068,380.0 | +608,921.6 (+41.72%) | +206,596.0 to +942,537.6 |
| tinyxml2/raw/normal/released | RSS_KiB (KiB) | 25,315.6 | 25,070.0 | -245.6 (-0.97%) | -928.6 to +398.0 |
| tinyxml2/raw/scavenge/post | HeapAlloc (B) | 623,713.6 | 626,020.0 | +2,306.4 (+0.37%) | +324.8 to +4,401.2 |
| tinyxml2/raw/scavenge/post | RSS_KiB (KiB) | 21,365.0 | 21,361.4 | -3.6 (-0.02%) | -530.6 to +460.0 |

## historical-memory-session2

| Workload / API / mode / endpoint | Metric | Baseline mean | Candidate mean | Change | 95% interval |
| --- | --- | ---: | ---: | ---: | --- |
| cjson/provider/gc/post | HeapAlloc (B) | 666,953.2 | 673,563.6 | +6,610.4 (+0.99%) | +5,028.8 to +8,096.8 |
| cjson/provider/gc/post | RSS_KiB (KiB) | 25,506.0 | 24,966.6 | -539.4 (-2.11%) | -1,111.8 to -25.4 |
| cjson/provider/normal/released | HeapAlloc (B) | 1,863,132.4 | 1,861,191.6 | -1,940.8 (-0.10%) | -14,484.4 to +9,552.8 |
| cjson/provider/normal/released | RSS_KiB (KiB) | 25,228.4 | 25,114.8 | -113.6 (-0.45%) | -628.8 to +393.0 |
| cjson/provider/scavenge/post | HeapAlloc (B) | 668,648.4 | 671,823.6 | +3,175.2 (+0.47%) | +1,317.2 to +4,869.6 |
| cjson/provider/scavenge/post | RSS_KiB (KiB) | 21,814.6 | 22,186.2 | +371.6 (+1.70%) | +152.2 to +607.0 |
| cjson/raw/gc/post | HeapAlloc (B) | 625,191.6 | 627,978.0 | +2,786.4 (+0.45%) | +832.4 to +4,704.0 |
| cjson/raw/gc/post | RSS_KiB (KiB) | 24,795.8 | 24,654.6 | -141.2 (-0.57%) | -620.0 to +382.6 |
| cjson/raw/normal/released | HeapAlloc (B) | 2,750,212.8 | 1,159,282.4 | -1,590,930.4 (-57.85%) | -1,944,187.6 to -1,192,229.2 |
| cjson/raw/normal/released | RSS_KiB (KiB) | 24,876.8 | 25,170.4 | +293.6 (+1.18%) | -459.8 to +1,046.8 |
| cjson/raw/scavenge/post | HeapAlloc (B) | 624,920.8 | 627,724.4 | +2,803.6 (+0.45%) | +1,260.0 to +4,440.4 |
| cjson/raw/scavenge/post | RSS_KiB (KiB) | 21,272.2 | 21,310.0 | +37.8 (+0.18%) | -465.6 to +495.6 |
| minimal-wasi/provider/gc/post | HeapAlloc (B) | 551,830.8 | 557,458.8 | +5,628.0 (+1.02%) | +3,642.0 to +7,570.8 |
| minimal-wasi/provider/gc/post | RSS_KiB (KiB) | 24,737.0 | 24,675.6 | -61.4 (-0.25%) | -853.4 to +766.0 |
| minimal-wasi/provider/normal/released | HeapAlloc (B) | 1,538,635.2 | 1,533,704.8 | -4,930.4 (-0.32%) | -6,718.8 to -3,152.8 |
| minimal-wasi/provider/normal/released | RSS_KiB (KiB) | 25,074.8 | 25,060.0 | -14.8 (-0.06%) | -663.2 to +595.2 |
| minimal-wasi/provider/scavenge/post | HeapAlloc (B) | 551,764.4 | 556,952.4 | +5,188.0 (+0.94%) | +3,784.4 to +6,810.4 |
| minimal-wasi/provider/scavenge/post | RSS_KiB (KiB) | 21,316.8 | 21,458.4 | +141.6 (+0.66%) | -482.2 to +786.2 |
| minimal-wasi/raw/gc/post | HeapAlloc (B) | 490,204.4 | 494,435.2 | +4,230.8 (+0.86%) | +2,670.4 to +5,913.2 |
| minimal-wasi/raw/gc/post | RSS_KiB (KiB) | 24,362.6 | 24,494.4 | +131.8 (+0.54%) | -514.6 to +789.0 |
| minimal-wasi/raw/normal/released | HeapAlloc (B) | 3,052,150.4 | 1,492,847.6 | -1,559,302.8 (-51.09%) | -1,676,310.4 to -1,443,285.6 |
| minimal-wasi/raw/normal/released | RSS_KiB (KiB) | 24,329.0 | 24,525.0 | +196.0 (+0.81%) | -421.0 to +790.4 |
| minimal-wasi/raw/scavenge/post | HeapAlloc (B) | 490,781.2 | 494,138.0 | +3,356.8 (+0.68%) | +1,337.2 to +5,350.0 |
| minimal-wasi/raw/scavenge/post | RSS_KiB (KiB) | 20,637.0 | 20,254.2 | -382.8 (-1.85%) | -863.0 to +110.8 |
| tinyxml2/provider/gc/post | HeapAlloc (B) | 683,737.6 | 689,154.4 | +5,416.8 (+0.79%) | +3,858.8 to +6,946.4 |
| tinyxml2/provider/gc/post | RSS_KiB (KiB) | 25,324.0 | 25,070.8 | -253.2 (-1.00%) | -925.4 to +392.0 |
| tinyxml2/provider/normal/released | HeapAlloc (B) | 3,057,966.8 | 3,154,111.6 | +96,144.8 (+3.14%) | +3,223.6 to +185,890.0 |
| tinyxml2/provider/normal/released | RSS_KiB (KiB) | 25,374.2 | 25,525.6 | +151.4 (+0.60%) | -363.8 to +654.0 |
| tinyxml2/provider/scavenge/post | HeapAlloc (B) | 685,821.6 | 689,479.6 | +3,658.0 (+0.53%) | +1,745.2 to +5,706.8 |
| tinyxml2/provider/scavenge/post | RSS_KiB (KiB) | 21,956.0 | 21,918.2 | -37.8 (-0.17%) | -672.8 to +641.6 |
| tinyxml2/raw/gc/post | HeapAlloc (B) | 625,612.4 | 626,629.6 | +1,017.2 (+0.16%) | -1,018.4 to +3,020.0 |
| tinyxml2/raw/gc/post | RSS_KiB (KiB) | 25,045.2 | 24,628.2 | -417.0 (-1.66%) | -907.6 to +42.2 |
| tinyxml2/raw/normal/released | HeapAlloc (B) | 1,327,740.4 | 1,961,140.0 | +633,399.6 (+47.71%) | +323,886.0 to +892,045.6 |
| tinyxml2/raw/normal/released | RSS_KiB (KiB) | 24,586.0 | 25,217.4 | +631.4 (+2.57%) | +11.8 to +1,162.6 |
| tinyxml2/raw/scavenge/post | HeapAlloc (B) | 626,213.6 | 626,218.0 | +4.4 (+0.00%) | -1,313.6 to +1,320.8 |
| tinyxml2/raw/scavenge/post | RSS_KiB (KiB) | 21,550.6 | 21,381.8 | -168.8 (-0.78%) | -757.6 to +468.8 |

## current-memory

| Workload / API / mode / endpoint | Metric | Baseline mean | Candidate mean | Change | 95% interval |
| --- | --- | ---: | ---: | ---: | --- |
| cjson/provider/gc/post | HeapAlloc (B) | 666,950.4 | 672,735.6 | +5,785.2 (+0.87%) | +4,041.6 to +7,533.2 |
| cjson/provider/gc/post | RSS_KiB (KiB) | 25,046.4 | 25,373.4 | +327.0 (+1.31%) | -63.0 to +704.6 |
| cjson/provider/normal/released | HeapAlloc (B) | 1,861,510.0 | 1,864,494.8 | +2,984.8 (+0.16%) | -15,829.2 to +20,372.8 |
| cjson/provider/normal/released | RSS_KiB (KiB) | 25,595.0 | 25,250.6 | -344.4 (-1.35%) | -865.4 to +187.0 |
| cjson/provider/scavenge/post | HeapAlloc (B) | 666,711.6 | 673,201.6 | +6,490.0 (+0.97%) | +4,899.6 to +8,021.2 |
| cjson/provider/scavenge/post | RSS_KiB (KiB) | 21,879.8 | 21,747.8 | -132.0 (-0.60%) | -738.8 to +450.2 |
| cjson/raw/gc/post | HeapAlloc (B) | 624,282.4 | 626,972.0 | +2,689.6 (+0.43%) | +1,216.4 to +4,335.2 |
| cjson/raw/gc/post | RSS_KiB (KiB) | 24,745.8 | 24,645.0 | -100.8 (-0.41%) | -705.8 to +447.6 |
| cjson/raw/normal/released | HeapAlloc (B) | 2,992,012.8 | 1,231,375.6 | -1,760,637.2 (-58.84%) | -2,208,900.0 to -1,216,143.2 |
| cjson/raw/normal/released | RSS_KiB (KiB) | 25,195.6 | 24,664.6 | -531.0 (-2.11%) | -1,000.6 to -31.4 |
| cjson/raw/scavenge/post | HeapAlloc (B) | 626,440.8 | 627,563.2 | +1,122.4 (+0.18%) | -410.8 to +2,667.2 |
| cjson/raw/scavenge/post | RSS_KiB (KiB) | 21,192.8 | 20,947.2 | -245.6 (-1.16%) | -753.6 to +217.6 |
| minimal-wasi/provider/gc/post | HeapAlloc (B) | 552,227.2 | 556,082.4 | +3,855.2 (+0.70%) | +2,828.8 to +4,704.4 |
| minimal-wasi/provider/gc/post | RSS_KiB (KiB) | 24,733.0 | 24,848.4 | +115.4 (+0.47%) | -608.6 to +817.4 |
| minimal-wasi/provider/normal/released | HeapAlloc (B) | 1,539,756.8 | 1,533,287.6 | -6,469.2 (-0.42%) | -7,815.2 to -5,173.6 |
| minimal-wasi/provider/normal/released | RSS_KiB (KiB) | 24,941.4 | 24,902.2 | -39.2 (-0.16%) | -816.8 to +734.6 |
| minimal-wasi/provider/scavenge/post | HeapAlloc (B) | 551,867.6 | 556,525.6 | +4,658.0 (+0.84%) | +3,231.2 to +6,081.6 |
| minimal-wasi/provider/scavenge/post | RSS_KiB (KiB) | 21,422.6 | 21,431.0 | +8.4 (+0.04%) | -793.4 to +776.8 |
| minimal-wasi/raw/gc/post | HeapAlloc (B) | 490,775.2 | 494,490.4 | +3,715.2 (+0.76%) | +2,242.0 to +5,176.4 |
| minimal-wasi/raw/gc/post | RSS_KiB (KiB) | 24,714.4 | 24,402.8 | -311.6 (-1.26%) | -879.6 to +294.4 |
| minimal-wasi/raw/normal/released | HeapAlloc (B) | 3,071,418.4 | 1,710,820.4 | -1,360,598.0 (-44.30%) | -1,524,991.6 to -1,185,842.4 |
| minimal-wasi/raw/normal/released | RSS_KiB (KiB) | 24,460.2 | 24,135.6 | -324.6 (-1.33%) | -1,016.8 to +301.8 |
| minimal-wasi/raw/scavenge/post | HeapAlloc (B) | 492,187.6 | 495,181.2 | +2,993.6 (+0.61%) | +1,512.4 to +4,616.8 |
| minimal-wasi/raw/scavenge/post | RSS_KiB (KiB) | 20,572.2 | 20,627.2 | +55.0 (+0.27%) | -705.0 to +844.0 |
| tinyxml2/provider/gc/post | HeapAlloc (B) | 683,603.2 | 689,718.4 | +6,115.2 (+0.89%) | +4,266.4 to +7,908.0 |
| tinyxml2/provider/gc/post | RSS_KiB (KiB) | 25,577.0 | 25,023.6 | -553.4 (-2.16%) | -1,224.4 to +73.0 |
| tinyxml2/provider/normal/released | HeapAlloc (B) | 3,025,814.8 | 3,170,915.6 | +145,100.8 (+4.80%) | +44,484.4 to +234,442.4 |
| tinyxml2/provider/normal/released | RSS_KiB (KiB) | 25,611.4 | 25,205.2 | -406.2 (-1.59%) | -928.0 to +110.4 |
| tinyxml2/provider/scavenge/post | HeapAlloc (B) | 684,790.4 | 689,457.2 | +4,666.8 (+0.68%) | +3,111.6 to +6,240.4 |
| tinyxml2/provider/scavenge/post | RSS_KiB (KiB) | 21,997.2 | 21,509.8 | -487.4 (-2.22%) | -1,104.2 to +120.4 |
| tinyxml2/raw/gc/post | HeapAlloc (B) | 625,098.0 | 626,091.2 | +993.2 (+0.16%) | -951.2 to +2,802.8 |
| tinyxml2/raw/gc/post | RSS_KiB (KiB) | 24,833.2 | 24,802.6 | -30.6 (-0.12%) | -627.8 to +570.2 |
| tinyxml2/raw/normal/released | HeapAlloc (B) | 1,177,402.4 | 2,103,347.6 | +925,945.2 (+78.64%) | +696,569.2 to +1,175,127.2 |
| tinyxml2/raw/normal/released | RSS_KiB (KiB) | 25,081.6 | 25,111.4 | +29.8 (+0.12%) | -659.4 to +730.6 |
| tinyxml2/raw/scavenge/post | HeapAlloc (B) | 624,365.6 | 627,289.2 | +2,923.6 (+0.47%) | +1,048.8 to +4,808.8 |
| tinyxml2/raw/scavenge/post | RSS_KiB (KiB) | 20,872.4 | 21,671.8 | +799.4 (+3.83%) | +232.2 to +1,340.0 |

## current-tinyxml2-confirmation

| Workload / API / mode / endpoint | Metric | Baseline mean | Candidate mean | Change | 95% interval |
| --- | --- | ---: | ---: | ---: | --- |
| tinyxml2/raw/gc/post | HeapAlloc (B) | 626,086.0 | 626,574.4 | +488.4 (+0.08%) | -1,120.8 to +2,136.4 |
| tinyxml2/raw/gc/post | RSS_KiB (KiB) | 24,689.8 | 24,714.2 | +24.4 (+0.10%) | -490.8 to +578.8 |
| tinyxml2/raw/normal/released | HeapAlloc (B) | 1,554,874.8 | 2,102,648.8 | +547,774.0 (+35.23%) | +313,395.6 to +823,524.4 |
| tinyxml2/raw/normal/released | RSS_KiB (KiB) | 25,048.0 | 25,155.6 | +107.6 (+0.43%) | -476.4 to +716.6 |
| tinyxml2/raw/scavenge/post | HeapAlloc (B) | 625,268.4 | 626,388.0 | +1,119.6 (+0.18%) | -222.4 to +2,499.2 |
| tinyxml2/raw/scavenge/post | RSS_KiB (KiB) | 21,021.0 | 21,255.4 | +234.4 (+1.12%) | -350.8 to +824.0 |

## snapshot-memory

| Workload / API / mode / endpoint | Metric | Baseline mean | Candidate mean | Change | 95% interval |
| --- | --- | ---: | ---: | ---: | --- |
| cjson/provider/gc/post | HeapAlloc (B) | 672,568.8 | 671,998.4 | -570.4 (-0.08%) | -2,244.8 to +1,179.2 |
| cjson/provider/gc/post | RSS_KiB (KiB) | 25,056.6 | 25,718.8 | +662.2 (+2.64%) | +29.6 to +1,259.2 |
| cjson/provider/normal/released | HeapAlloc (B) | 1,867,788.4 | 1,870,719.6 | +2,931.2 (+0.16%) | -18,342.8 to +29,070.0 |
| cjson/provider/normal/released | RSS_KiB (KiB) | 25,446.0 | 25,094.0 | -352.0 (-1.38%) | -757.8 to +44.2 |
| cjson/provider/scavenge/post | HeapAlloc (B) | 672,830.8 | 672,436.4 | -394.4 (-0.06%) | -2,616.0 to +1,838.4 |
| cjson/provider/scavenge/post | RSS_KiB (KiB) | 21,917.0 | 22,038.4 | +121.4 (+0.55%) | -463.6 to +688.6 |
| cjson/raw/gc/post | HeapAlloc (B) | 627,766.0 | 627,198.0 | -568.0 (-0.09%) | -2,077.2 to +910.0 |
| cjson/raw/gc/post | RSS_KiB (KiB) | 24,952.6 | 24,977.0 | +24.4 (+0.10%) | -596.0 to +635.2 |
| cjson/raw/normal/released | HeapAlloc (B) | 3,106,628.4 | 1,139,572.4 | -1,967,056.0 (-63.32%) | -2,191,950.4 to -1,670,807.6 |
| cjson/raw/normal/released | RSS_KiB (KiB) | 25,277.0 | 25,124.2 | -152.8 (-0.60%) | -783.4 to +435.2 |
| cjson/raw/scavenge/post | HeapAlloc (B) | 626,673.2 | 627,949.6 | +1,276.4 (+0.20%) | -1,216.0 to +3,918.8 |
| cjson/raw/scavenge/post | RSS_KiB (KiB) | 20,893.4 | 21,847.8 | +954.4 (+4.57%) | +269.2 to +1,579.8 |
| minimal-wasi/provider/gc/post | HeapAlloc (B) | 556,866.0 | 556,522.4 | -343.6 (-0.06%) | -2,091.2 to +1,377.2 |
| minimal-wasi/provider/gc/post | RSS_KiB (KiB) | 24,706.4 | 24,705.8 | -0.6 (-0.00%) | -512.4 to +519.2 |
| minimal-wasi/provider/normal/released | HeapAlloc (B) | 1,533,704.4 | 1,532,406.8 | -1,297.6 (-0.08%) | -3,788.8 to +1,082.8 |
| minimal-wasi/provider/normal/released | RSS_KiB (KiB) | 24,779.6 | 24,663.6 | -116.0 (-0.47%) | -630.0 to +410.8 |
| minimal-wasi/provider/scavenge/post | HeapAlloc (B) | 557,136.8 | 556,662.8 | -474.0 (-0.09%) | -2,071.2 to +1,088.4 |
| minimal-wasi/provider/scavenge/post | RSS_KiB (KiB) | 21,959.6 | 21,202.8 | -756.8 (-3.45%) | -1,266.6 to -253.4 |
| minimal-wasi/raw/gc/post | HeapAlloc (B) | 493,924.0 | 495,242.8 | +1,318.8 (+0.27%) | -878.8 to +3,217.2 |
| minimal-wasi/raw/gc/post | RSS_KiB (KiB) | 24,274.8 | 23,655.4 | -619.4 (-2.55%) | -1,325.0 to +104.2 |
| minimal-wasi/raw/normal/released | HeapAlloc (B) | 2,965,021.2 | 1,638,008.8 | -1,327,012.4 (-44.76%) | -1,799,177.6 to -769,769.2 |
| minimal-wasi/raw/normal/released | RSS_KiB (KiB) | 24,412.8 | 24,465.4 | +52.6 (+0.22%) | -564.4 to +658.2 |
| minimal-wasi/raw/scavenge/post | HeapAlloc (B) | 493,546.0 | 494,154.0 | +608.0 (+0.12%) | -1,346.8 to +2,444.0 |
| minimal-wasi/raw/scavenge/post | RSS_KiB (KiB) | 20,802.4 | 20,840.8 | +38.4 (+0.18%) | -522.0 to +572.8 |
| tinyxml2/provider/gc/post | HeapAlloc (B) | 690,395.6 | 689,304.8 | -1,090.8 (-0.16%) | -2,798.8 to +646.0 |
| tinyxml2/provider/gc/post | RSS_KiB (KiB) | 24,774.8 | 25,156.2 | +381.4 (+1.54%) | -157.6 to +999.6 |
| tinyxml2/provider/normal/released | HeapAlloc (B) | 3,148,363.6 | 2,974,017.6 | -174,346.0 (-5.54%) | -534,260.8 to +102,938.8 |
| tinyxml2/provider/normal/released | RSS_KiB (KiB) | 25,461.4 | 25,493.4 | +32.0 (+0.13%) | -682.0 to +779.4 |
| tinyxml2/provider/scavenge/post | HeapAlloc (B) | 691,068.4 | 689,432.4 | -1,636.0 (-0.24%) | -3,126.0 to -121.6 |
| tinyxml2/provider/scavenge/post | RSS_KiB (KiB) | 21,989.8 | 22,068.0 | +78.2 (+0.36%) | -537.0 to +727.6 |
| tinyxml2/raw/gc/post | HeapAlloc (B) | 626,735.6 | 625,737.2 | -998.4 (-0.16%) | -3,102.8 to +1,288.8 |
| tinyxml2/raw/gc/post | RSS_KiB (KiB) | 24,672.8 | 24,437.6 | -235.2 (-0.95%) | -979.2 to +473.2 |
| tinyxml2/raw/normal/released | HeapAlloc (B) | 1,558,531.2 | 2,040,868.8 | +482,337.6 (+30.95%) | +62,583.2 to +811,792.0 |
| tinyxml2/raw/normal/released | RSS_KiB (KiB) | 25,095.0 | 24,689.0 | -406.0 (-1.62%) | -811.0 to +29.2 |
| tinyxml2/raw/scavenge/post | HeapAlloc (B) | 626,640.0 | 625,653.2 | -986.8 (-0.16%) | -2,519.2 to +604.0 |
| tinyxml2/raw/scavenge/post | RSS_KiB (KiB) | 21,186.2 | 21,038.2 | -148.0 (-0.70%) | -533.4 to +228.8 |

## Scale, epochs and sampled peaks

These are descriptive means, not additional independent samples within each process. Interval peaks are external 5 ms samples for the work interval ending at the named point; a very short peak can be missed. RSS is KiB. Heap counters are bytes. Whole-process high water and total faults remain in each raw record.

| Run / workload / API / point | HeapAlloc B→C | Last measured live heap B→C | RSS B→C | Interval sampled peak RSS B→C | NumGC B→C |
| --- | ---: | ---: | ---: | ---: | ---: |
| current-100commands/cjson/provider/normal/epoch1 | 1,934,318 → 1,934,532 | 978,126 → 978,404 | 24,680 → 24,897 | 24,680 → 24,897 | 1 → 1 |
| current-100commands/cjson/raw/normal/epoch1 | 1,623,952 → 3,683,006 | 700,143 → 1,000,937 | 25,243 → 24,542 | 25,243 → 24,542 | 2 → 1 |
| current-100commands/minimal-wasi/provider/normal/epoch1 | 1,174,690 → 1,168,246 | 0 → 0 | 19,910 → 19,942 | 19,910 → 19,942 | 0 → 0 |
| current-100commands/minimal-wasi/raw/normal/epoch1 | 1,215,434 → 2,929,047 | 565,028 → 0 | 23,989 → 21,579 | 23,989 → 21,579 | 1 → 0 |
| current-100commands/tinyxml2/provider/normal/epoch1 | 2,308,239 → 2,296,540 | 921,206 → 898,462 | 24,723 → 24,738 | 24,723 → 24,738 | 1 → 1 |
| current-100commands/tinyxml2/raw/normal/epoch1 | 1,956,217 → 821,572 | 723,652 → 709,965 | 25,244 → 24,924 | 25,244 → 24,924 | 2 → 2 |
| current-epochs/cjson/provider/normal/epoch1 | 1,867,842 → 1,901,887 | 721,262 → 725,959 | 24,992 → 25,526 | 25,076 → 25,614 | 3 → 3 |
| current-epochs/cjson/provider/normal/epoch2 | 2,518,309 → 2,593,124 | 721,291 → 724,564 | 24,847 → 25,541 | 25,032 → 25,747 | 5 → 5 |
| current-epochs/cjson/provider/normal/epoch3 | 3,198,100 → 3,304,737 | 730,732 → 729,272 | 25,147 → 25,481 | 25,298 → 25,778 | 7 → 7 |
| current-epochs/cjson/provider/normal/epoch4 | 1,985,612 → 1,687,385 | 733,442 → 729,887 | 24,926 → 25,432 | 25,232 → 25,689 | 10 → 10 |
| current-epochs/cjson/raw/normal/epoch1 | 2,798,562 → 1,221,670 | 715,332 → 715,328 | 25,207 → 25,351 | 25,865 → 25,877 | 12 → 9 |
| current-epochs/cjson/raw/normal/epoch2 | 2,404,274 → 1,688,189 | 723,516 → 714,166 | 25,005 → 25,215 | 25,579 → 25,700 | 24 → 17 |
| current-epochs/cjson/raw/normal/epoch3 | 2,349,256 → 2,114,126 | 725,581 → 709,728 | 25,177 → 25,331 | 25,473 → 25,671 | 36 → 25 |
| current-epochs/cjson/raw/normal/epoch4 | 2,126,744 → 2,290,748 | 721,694 → 713,046 | 25,138 → 25,424 | 25,566 → 25,892 | 48 → 33 |
| current-epochs/minimal-wasi/provider/normal/epoch1 | 1,537,143 → 1,532,442 | 642,136 → 641,674 | 24,813 → 24,374 | 24,813 → 24,374 | 1 → 1 |
| current-epochs/minimal-wasi/provider/normal/epoch2 | 2,209,200 → 2,199,556 | 610,371 → 604,781 | 24,920 → 24,466 | 25,016 → 24,574 | 2 → 2 |
| current-epochs/minimal-wasi/provider/normal/epoch3 | 2,885,367 → 2,872,833 | 605,005 → 599,753 | 24,974 → 24,254 | 25,159 → 24,654 | 3 → 3 |
| current-epochs/minimal-wasi/provider/normal/epoch4 | 3,420,298 → 3,398,467 | 610,890 → 606,113 | 24,895 → 24,294 | 25,073 → 24,410 | 4 → 4 |
| current-epochs/minimal-wasi/raw/normal/epoch1 | 3,051,150 → 1,488,082 | 542,114 → 530,779 | 24,279 → 24,181 | 24,606 → 24,394 | 10 → 7 |
| current-epochs/minimal-wasi/raw/normal/epoch2 | 2,246,575 → 2,502,403 | 539,354 → 541,991 | 24,110 → 24,090 | 24,437 → 24,424 | 21 → 14 |
| current-epochs/minimal-wasi/raw/normal/epoch3 | 1,655,890 → 3,035,782 | 549,037 → 536,977 | 24,341 → 24,450 | 24,658 → 24,772 | 32 → 21 |
| current-epochs/minimal-wasi/raw/normal/epoch4 | 1,991,539 → 1,363,791 | 560,525 → 540,506 | 24,311 → 24,464 | 24,676 → 24,854 | 43 → 29 |
| current-epochs/tinyxml2/provider/normal/epoch1 | 3,071,467 → 3,191,997 | 736,061 → 737,251 | 25,575 → 25,293 | 25,664 → 25,394 | 3 → 3 |
| current-epochs/tinyxml2/provider/normal/epoch2 | 1,555,212 → 1,832,079 | 748,161 → 743,658 | 25,439 → 25,271 | 25,855 → 25,451 | 6 → 6 |
| current-epochs/tinyxml2/provider/normal/epoch3 | 2,887,678 → 3,328,060 | 742,838 → 741,778 | 25,367 → 25,099 | 25,633 → 25,375 | 8 → 8 |
| current-epochs/tinyxml2/provider/normal/epoch4 | 1,471,105 → 1,727,855 | 743,294 → 742,068 | 25,245 → 25,093 | 25,608 → 25,300 | 11 → 11 |
| current-epochs/tinyxml2/raw/normal/epoch1 | 1,371,028 → 2,021,366 | 720,073 → 709,424 | 25,286 → 25,347 | 25,832 → 25,674 | 13 → 9 |
| current-epochs/tinyxml2/raw/normal/epoch2 | 1,810,247 → 2,832,390 | 718,992 → 711,349 | 25,290 → 25,063 | 25,726 → 25,557 | 25 → 17 |
| current-epochs/tinyxml2/raw/normal/epoch3 | 1,774,676 → 2,516,905 | 721,139 → 720,853 | 25,288 → 25,096 | 25,771 → 25,494 | 37 → 25 |
| current-epochs/tinyxml2/raw/normal/epoch4 | 2,038,880 → 1,657,875 | 724,860 → 720,403 | 25,458 → 25,101 | 25,984 → 25,466 | 49 → 34 |

## Faults and whole-process peaks

Each entry is the observed minimum–maximum across complete processes, not a per-command or per-phase estimate. Major faults include file-backed pages and may include executable loading; they do not by themselves show guest-memory disk I/O.

| Run | Minor faults | Major faults | Whole-process max RSS KiB |
| --- | ---: | ---: | ---: |
| historical-memory-session1 | 1,976–9,990 | 0–0 | 39,760–40,332 |
| historical-memory-session2 | 1,978–9,826 | 0–0 | 39,904–40,348 |
| current-memory | 1,975–9,809 | 0–0 | 39,796–40,308 |
| current-tinyxml2-confirmation | 9,176–10,177 | 0–0 | 39,724–39,980 |
| snapshot-memory | 1,977–9,787 | 0–0 | 39,940–40,324 |
| current-epochs | 5,058–36,182 | 0–0 | 39,720–40,104 |
