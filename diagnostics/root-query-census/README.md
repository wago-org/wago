# Untimed root-query census

One diagnostic overlay run covered 145 modules on clean b5ab03f9, with
experimental flags and PGO off. Native output and serialized metadata
matched the uninstrumented reference for all 145 modules. No unknown
callers or counter-relationship errors were reported. The reviewed
overlay manifest SHA-256 was
`6d2fa1b4e68ce60dbdd6a1206e33daaa057715a4f47324c15b659941cf88ee8b`.

| Query domain | Queries | Items | Left-spine steps | Reverse swaps |
|---|---:|---:|---:|---:|
| Roots | 41,197 | 27,167 | 2,486 | 8,306 |
| Types | 21,796 | 14,905 | — | — |

Branch-end prefixes recorded 18,539 repeat queries but only 5,022
overlapping items. Prefix accounting covers selected slot-prefix calls,
not all width accounting. Overlap counts items in the common prefix
against the largest prior prefix in the same audited scope; this is not
a measured saving.

Repeat classification is conservative: it recognizes only explicit
same-schema scopes with known lifetimes and invalidation boundaries.
There were no root/type repeats in those audited scopes. The dynamic-host
positive witness did not activate in this corpus; this is not an executed
positive-control test. The census does not compare arbitrary root images
across instructions or establish that every possible fusion is absent.

Caller attribution and mutex instrumentation make elapsed time unusable.
This was a work census, not a timing or RSS experiment. No projection
cache was implemented or promoted, and the results do not establish a
route to a 40% compile-time improvement.
