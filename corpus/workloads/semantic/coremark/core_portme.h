/* Wago semantic-corpus porting header for CoreMark.
 *
 * This is a reviewed, freestanding port of CoreMark's porting layer. It is not
 * upstream source: it replaces the barebones `core_portme.h` so the benchmark
 * can be built to an import-free wasm32 module and driven through Wago's core
 * API. The benchmark algorithms themselves (`core_list_join.c`, `core_matrix.c`,
 * `core_state.c`, `core_util.c`) are compiled byte-for-byte from the pinned
 * upstream revision; only this porting layer and the runner below are Wago's.
 *
 * Upstream: https://github.com/eembc/coremark (Apache License 2.0).
 */
#ifndef CORE_PORTME_H
#define CORE_PORTME_H

#define HAS_FLOAT   1
#define HAS_TIME_H  0
#define USE_CLOCK   0
#define HAS_STDIO   0
#define HAS_PRINTF  0

#ifndef COMPILER_VERSION
#define COMPILER_VERSION "wasi-sdk freestanding"
#endif
#ifndef COMPILER_FLAGS
#define COMPILER_FLAGS "-O2"
#endif
#ifndef MEM_LOCATION
#define MEM_LOCATION "STACK"
#endif

typedef signed short   ee_s16;
typedef unsigned short ee_u16;
typedef signed int     ee_s32;
typedef unsigned int   ee_u32;
typedef double         ee_f32;
typedef unsigned char  ee_u8;
/* Pointer-sized integer: 32-bit on wasm32, native-width otherwise so the same
 * runner also builds as a native differential reference (oracle strength 3). */
#ifdef __wasm32__
typedef ee_u32 ee_ptr_int;
#else
typedef unsigned long ee_ptr_int;
#endif
typedef ee_u32 ee_size_t;
#ifndef NULL
#define NULL ((void *)0)
#endif

#define align_mem(x) (void *)(4 + (((ee_ptr_int)(x)-1) & ~3))

#define CORETIMETYPE ee_u32
typedef ee_u32 CORE_TICKS;

#define SEED_METHOD       SEED_VOLATILE
#define MEM_METHOD        MEM_STACK
#define MULTITHREAD       1
#define USE_PTHREAD       0
#define USE_FORK          0
#define USE_SOCKET        0
#define MAIN_HAS_NOARGC   1
#define MAIN_HAS_NORETURN 0

extern ee_u32 default_num_contexts;

typedef struct CORE_PORTABLE_S
{
    ee_u8 portable_id;
} core_portable;

void portable_init(core_portable *p, int *argc, char *argv[]);
void portable_fini(core_portable *p);

int ee_printf(const char *fmt, ...);

#endif /* CORE_PORTME_H */
