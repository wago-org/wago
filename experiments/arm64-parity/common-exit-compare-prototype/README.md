# Common-exit compare prototype

Prepared only, unintegrated/untested. General finalized CMP/CMN + conditional branch pairs with a shared exit; no corpus names, constants, dimensions, or stencil assumptions. Preserve code size: retain firstcompare and lastbranch, replace firstbranch withCCMP/CCMN and secondcompare withNOP. Register comparisons and immediate<=31 forms admitted; immediate Rn31 is excluded because CMP readsSP but conditional compare readsZR.

NZCV fallback must satisfy the final condition when the firstcondition exits. Both continuations must prove flagsdead beforeuse. Reject external entries into changed words, opaque/EH/custom streams. Compiler integration must include wrapper/internal entry offsets in branchTarget exclusions.

Independent tests needed: all14conditions and16flagstates; signed/unsigned boundary inputs, mixed conditions, both widths, CMP/CMN register/immediate goldens and fallback rejection (SP,shiftedregister,largeimmediate), externalentries, liveNZCV, differenttargets, native branchtruth output in bothbounds. Then qualifyaffectedimages/oracles and measure focused execution/compile cost. Dilation loweredcode contains ordinary signedcompare pairs, but eligibility must stay general. Existing guardedTestCCMP handles a different TST/AND grammar and doesnot cover these pairs.
