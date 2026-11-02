# Final formatting repair

Two gofmt-only changes repair the 5b02 replay-format failure. Original bytes are retained as .go.txt files here; historical logs and historical source hashes remain unchanged and refer to their original pins. The production tree is unchanged: one test adapter and one archived review probe change only whitespace. No assertion, build tag, deadline, or behavior changes. This cannot affect shipped runtime performance.

The earlier byte-identical RR adapter claim remains true at 5847 and its reviewed pins; the current formatted adapter is explicitly no longer byte-identical and identity.json records the exact transition. The archived branch19 review manifest similarly continues to describe the original review pin, not this formatting update.

Current combined 5b02 qualification continues independently, retaining formatting FAIL. Final-head acceptance requires explicit delta accounting and a fresh formatting gate; no historical verdict is rewritten.
