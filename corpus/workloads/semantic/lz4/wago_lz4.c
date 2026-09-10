/* Wago semantic-corpus porting layer and runner for LZ4 (block API).
 *
 * Reviewed adaptation (not upstream): lib/lz4.c is compiled byte-for-byte from
 * the pinned upstream revision; this file provides the freestanding allocator
 * hooks (LZ4's streaming API declares them even though the block API here does
 * not allocate) and exported compress/decompress runners over guest-owned
 * buffers.
 */
#include <stdint.h>
#include <stddef.h>

#include "wago_freestanding.h"

/* build.sh compiles the upstream lz4.c with -DLZ4_USER_MEMORY_FUNCTIONS, which
 * replaces <stdlib.h> malloc/calloc/free with the LZ4_malloc/LZ4_calloc/LZ4_free
 * hooks defined below (bounded freestanding allocators instead of a host libc). */
#include "lz4.h"

void *
LZ4_malloc(size_t s)
{
    return wago_malloc(s);
}

void *
LZ4_calloc(size_t n, size_t s)
{
    return wago_calloc(n, s);
}

void
LZ4_free(void *p)
{
    wago_free(p);
}

#ifndef WLZ4_INPUT_LEN
#define WLZ4_INPUT_LEN 16384
#endif
#define WLZ4_BUF_LEN (LZ4_COMPRESSBOUND(WLZ4_INPUT_LEN))

uint8_t input_buf[WLZ4_BUF_LEN];  /* pattern (compress) or stream (decompress) */
uint8_t output_buf[WLZ4_BUF_LEN]; /* stream (compress) or pattern (decompress) */

/* Deterministic test input: one third short runs, two thirds high-entropy, so
 * the stream exercises LZ4's literal/match finding rather than a trivial run. */
static void
fill_pattern(uint8_t *buf, size_t len)
{
    size_t i;
    for (i = 0; i < len; i++)
    {
        if ((i / 64) % 3 == 0)
            buf[i] = (uint8_t)(i % 7);
        else
            buf[i] = (uint8_t)((uint32_t)(i * 2654435761u) >> 16);
    }
}

uint32_t
lz4_input_ptr(void)
{
    return (uint32_t)(uintptr_t)input_buf;
}

uint32_t
lz4_output_ptr(void)
{
    return (uint32_t)(uintptr_t)output_buf;
}

/* lz4_compress_run() -> compressed size; compresses the deterministic pattern. */
uint32_t
lz4_compress_run(void)
{
    int c;
    fill_pattern(input_buf, WLZ4_INPUT_LEN);
    c = LZ4_compress_default((const char *)input_buf,
                             (char *)output_buf,
                             (int)WLZ4_INPUT_LEN,
                             (int)WLZ4_BUF_LEN);
    if (c <= 0)
        return 0;
    return (uint32_t)c;
}

/* lz4_decompress_run(comp_len) -> decompressed size; decompresses input_buf's
 * stream (comp_len bytes) into output_buf. */
uint32_t
lz4_decompress_run(uint32_t comp_len)
{
    int d;
    d = LZ4_decompress_safe((const char *)input_buf,
                            (char *)output_buf,
                            (int)comp_len,
                            (int)WLZ4_BUF_LEN);
    if (d < 0)
        return 0;
    return (uint32_t)d;
}
