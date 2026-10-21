No new findings in the fixture delta. The allocator now stores a default type 1 struct in separate eqref global 9. The trap body and its global 8 input are unchanged. The new checks require each trapped leaf to report function 0 with no program counter and the all-ones sentinel.

This was a read-only review. I did not run the reported tests or assess the separate production overlay.