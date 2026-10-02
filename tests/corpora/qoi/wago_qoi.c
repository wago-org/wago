/* Wago semantic-corpus porting layer and runner for QOI.
 *
 * Reviewed adaptation (not upstream): the single-header qoi.h is compiled
 * byte-for-byte from the pinned upstream revision; this file provides the
 * freestanding allocator hooks, a deterministic test image, and exported
 * encode/decode runners that use guest-owned buffers (see pointer exports).
 */
#include <stdint.h>
#include <stddef.h>

#include "wago_freestanding.h"

#define QOI_NO_STDIO
#define QOI_MALLOC wago_malloc
#define QOI_FREE    wago_free
#define QOI_IMPLEMENTATION
#include "qoi.h"

/* WQOI_ prefix avoids colliding with qoi.h's own QOI_* macros (QOI_H is the
 * header include guard). */
#define WQOI_W        32
#define WQOI_H        32
#define WQOI_CHANNELS 4
#define WQOI_PX_LEN   (WQOI_W * WQOI_H * WQOI_CHANNELS)
/* Worst-case QOI stream: 5 bytes/pixel + header + end padding. */
#define WQOI_MAX_ENC  (WQOI_W * WQOI_H * (WQOI_CHANNELS + 1) + QOI_HEADER_SIZE + 8)

/* Non-static so a native differential reference can access them directly
 * (pointer-width on native hosts); they are not exported from the wasm module. */
uint8_t input_buf[WQOI_MAX_ENC];  /* QOI bytes for decode */
uint8_t output_buf[WQOI_MAX_ENC]; /* QOI bytes (encode) or RGBA (decode) */
uint8_t rgba_buf[WQOI_PX_LEN];    /* deterministic test image */

/* Deterministic test image: per-channel gradients plus a 4-pixel alpha run so
 * the stream exercises QOI's RUN, INDEX, DIFF, and LUMA opcodes. */
static void
fill_pattern(uint8_t *px)
{
    int x, y;
    for (y = 0; y < WQOI_H; y++)
    {
        for (x = 0; x < WQOI_W; x++)
        {
            int      i = y * WQOI_W + x;
            uint8_t *p = px + i * 4;
            p[0] = (uint8_t)((x * 7 + y * 3) & 0xff);
            p[1] = (uint8_t)((x * 2 + y * 5) & 0xff);
            p[2] = (uint8_t)((x + y) & 0xff);
            p[3] = (uint8_t)((i / 4) & 0xff);
        }
    }
}

uint32_t
qoi_input_ptr(void)
{
    return (uint32_t)(uintptr_t)input_buf;
}

uint32_t
qoi_output_ptr(void)
{
    return (uint32_t)(uintptr_t)output_buf;
}

uint32_t
qoi_pattern_ptr(void)
{
    return (uint32_t)(uintptr_t)rgba_buf;
}

/* qoi_encode_run() -> encoded length; writes the deterministic image's QOI
 * stream to output_buf. */
uint32_t
qoi_encode_run(void)
{
    qoi_desc desc;
    int      out_len = 0;
    void    *encoded;

    fill_pattern(rgba_buf);
    desc.width      = WQOI_W;
    desc.height     = WQOI_H;
    desc.channels   = WQOI_CHANNELS;
    desc.colorspace = QOI_SRGB;
    encoded         = qoi_encode(rgba_buf, &desc, &out_len);
    if (encoded == NULL)
        return 0;
    memcpy(output_buf, encoded, (size_t)out_len);
    return (uint32_t)out_len;
}

/* qoi_decode_run(qoi_len) -> decoded pixel length; decodes input_buf's QOI
 * stream (qoi_len bytes) into output_buf as RGBA. */
uint32_t
qoi_decode_run(uint32_t qoi_len)
{
    qoi_desc desc;
    void    *decoded;
    uint32_t px_len;

    decoded = qoi_decode(input_buf, (int)qoi_len, &desc, WQOI_CHANNELS);
    if (decoded == NULL)
        return 0;
    px_len = desc.width * desc.height * WQOI_CHANNELS;
    memcpy(output_buf, decoded, px_len);
    return px_len;
}
