/* The corpus runs one valid, embedded program and does not exercise recovery
 * from Lua errors. Avoid setjmp and signals so the interpreter stays a plain
 * core Wasm module; any unexpected error traps and fails the exact oracle. */
#define l_signalT int
#define _SETJMP_H
#define luai_jmpbuf int
#define LUAI_THROW(L, c) __builtin_trap()
#define LUAI_TRY(L, c, a) do { a } while (0)
