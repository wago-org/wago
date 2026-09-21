# Repository Instructions

Wago is a pure-Go WebAssembly engine that compiles WebAssembly directly to native machine code.

1. Use ASD-STE100 when you communicate with the user.
2. Before each tool call, provide one sentence that explains what you will do.
3. Keep commits atomic and easy to review.
4. If you have to provide a long comment on a piece of code, instead, just fix it
5. Do not introduce heap allocations unless they help with a serious safety issue, and they don't allocate too much memory. Wago should be lean and fast, suitable for embedded systems.
6. When committing, please considder which benchmarks are affected, and how. Performance is a primary concern when building Wago.
7. Run `just lint` before each commit and fix any failures before committing.
8. Do not add text files, generated artifacts, or new research docs to commits.
