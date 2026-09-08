Build sizes: 4 profiles within budget

| Profile | Size | Delta vs main | Budget |
|---|---:|---:|---:|
| manager | 7.41 MiB | +8.0 KiB | 8.58 MiB (1.17 MiB free) |
| runtime-standard | 7.45 MiB | -144.0 KiB | 8.46 MiB (1.01 MiB free) |
| runtime-minimal | 7.14 MiB | -140.0 KiB | 8.16 MiB (1.02 MiB free) |
| runtime-minimal-tiny | 2.15 MiB | +12.2 KiB | 2.21 MiB (60 KiB free) |

Target: `linux/amd64`; stripped, `-trimpath`, `-buildvcs=false`. Top-symbol data: `size-symbols.tsv`.
