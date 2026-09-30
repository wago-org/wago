/* Wago semantic-corpus porting layer and runner for zstd (decompress).
 *
 * Reviewed adaptation (not upstream): the decompress translation units are
 * compiled byte-for-byte from the pinned upstream revision; this file only
 * drives ZSTD_decompress over a guest-owned buffer pair. The shared
 * freestanding shim supplies malloc/free so ZSTD_malloc/ZSTD_free resolve to
 * the bounded arena instead of a host libc.
 */
#include <stdint.h>
#include <stddef.h>
#include <string.h>

#include "wago_freestanding.h"
#include "zstd.h"

#define WZSTD_OUT_LEN 65536 /* decompressed output capacity */
#define WZSTD_IN_LEN  32768 /* compressed frame capacity */

uint8_t input_buf[WZSTD_IN_LEN];   /* reference zstd frame */
uint8_t output_buf[WZSTD_OUT_LEN]; /* decompressed output */

uint32_t
zstd_input_ptr(void)
{
    return (uint32_t)(uintptr_t)input_buf;
}

uint32_t
zstd_output_ptr(void)
{
    return (uint32_t)(uintptr_t)output_buf;
}

/* zstd_decompress_run(comp_len) -> decompressed size; decompresses input_buf's
 * frame (comp_len bytes) into output_buf. Returns 0 on any zstd error. */
uint32_t
zstd_decompress_run(uint32_t comp_len)
{
    size_t d;
    wago_allocator_reset();
    d = ZSTD_decompress(output_buf, WZSTD_OUT_LEN, input_buf, comp_len);
    if (ZSTD_isError(d))
        return 0;
    return (uint32_t)d;
}
