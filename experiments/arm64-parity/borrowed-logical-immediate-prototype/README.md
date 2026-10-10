# Borrowed logical immediate prototype

Prepared only; not integrated or tested. Do not claim a performance gain.

General pattern: a borrowed integer local/global followed by an encodable AND, OR, or XOR constant, without an explicit local sink. Read the borrowed source directly into the result register. Concrete operands preserve evaluation order; encodability is checked before allocating, source remains pinned across allocation, and inherited pins are restored. This extends the existing three-operand local sink to ordinary temporary results. It does not alter shifts, comparisons, multiplication, corpus eligibility, or argument values.

Motivation: retained ADPCM source-read assembly contains MOV X9,X19; AND W9,W9,#15 in the hot loop. Other consumers may use the same general form. Verify the fresh retained profiles before integration.

Integration: default-off per-compilation option, helper hook after commutative constant reassociation in condenseBinary. Tests must cover both widths, encodable/nonencodable and zero/all-one masks, dirty i32 carriers, source reuse after the operation, explicit destination/source alias, commuted constants, allocator diagnostics, and both bounds modes. Freeze the retained snapshot first. Compare only affected images and focused execution/compile samples. If layout or pressure regresses, first try preserving layout or limiting result lifetimes before rejection.
