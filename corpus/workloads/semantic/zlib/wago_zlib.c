/* Wago semantic-corpus porting layer and runner for zlib (inflate).
 *
 * Reviewed adaptation (not upstream): the inflate translation units are
 * compiled byte-for-byte from the pinned upstream revision; this file only
 * drives inflate() over a guest-owned buffer pair. The shared freestanding shim
 * supplies malloc/calloc/free so zlib's zcalloc/zcfree resolve to the bounded
 * arena instead of a host libc.
 */
#include <stdint.h>
#include <stddef.h>
#include <string.h>

#include "wago_freestanding.h"
#include "zlib.h"

#define WZLIB_OUT_LEN 65536 /* decompressed output capacity */
#define WZLIB_IN_LEN  32768 /* compressed stream capacity */

uint8_t input_buf[WZLIB_IN_LEN];   /* reference zlib stream */
uint8_t output_buf[WZLIB_OUT_LEN]; /* decompressed output */

uint32_t
zlib_input_ptr(void)
{
    return (uint32_t)(uintptr_t)input_buf;
}

uint32_t
zlib_output_ptr(void)
{
    return (uint32_t)(uintptr_t)output_buf;
}

/* zlib_inflate_run(comp_len) -> decompressed size; inflates input_buf's stream
 * (comp_len bytes) into output_buf. Returns 0 on any zlib error. */
uint32_t
zlib_inflate_run(uint32_t comp_len)
{
    z_stream strm;
    int      ret;

    wago_allocator_reset();
    memset(&strm, 0, sizeof(strm));
    ret = inflateInit(&strm);
    if (ret != Z_OK)
        return 0;
    strm.next_in   = input_buf;
    strm.avail_in  = (uInt)comp_len;
    strm.next_out  = output_buf;
    strm.avail_out = WZLIB_OUT_LEN;
    ret = inflate(&strm, Z_FINISH);
    inflateEnd(&strm);
    if (ret != Z_STREAM_END)
        return 0;
    return (uint32_t)strm.total_out;
}
