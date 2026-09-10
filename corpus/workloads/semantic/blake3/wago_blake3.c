/* Wago semantic-corpus porting layer and runner for BLAKE3.
 *
 * Reviewed adaptation (not upstream): supplies the freestanding libc shims the
 * portable BLAKE3 core needs (memcpy/memset/strlen) and three exported runners
 * that compute the published-vector modes. The portable core (c/blake3.c and
 * its includes) is compiled byte-for-byte from the pinned upstream revision.
 *
 * The key and context strings are the ones published in upstream
 * test_vectors/test_vectors.json; they are embedded here so each export takes
 * only (input_ptr, input_len, out_ptr).
 */
#include <stddef.h>
#include <stdint.h>

#include "blake3.h"

/* ---- freestanding libc shims (BLAKE3's portable core needs only these) ---- */

void *
memcpy(void *dst, const void *src, size_t n)
{
    uint8_t       *d = (uint8_t *)dst;
    const uint8_t *s = (const uint8_t *)src;
    size_t         i;
    for (i = 0; i < n; i++)
        d[i] = s[i];
    return dst;
}

void *
memset(void *dst, int c, size_t n)
{
    uint8_t *d = (uint8_t *)dst;
    size_t   i;
    for (i = 0; i < n; i++)
        d[i] = (uint8_t)c;
    return dst;
}

size_t
strlen(const char *s)
{
    size_t n = 0;
    while (s[n])
        n++;
    return n;
}

/* ---- published-vector constants ----
 *
 * Each upstream test vector is 131 bytes: the 32-byte hash followed by 99 bytes
 * of extended (XOF) output, produced with blake3_hasher_finalize_seek from
 * byte offset 0. */

#define BLAKE3_VECTOR_LEN 131
#define BLAKE3_MAX_INPUT   102400

static const uint8_t key[32] = "whats the Elvish word for friend";
static const char    context[] = "BLAKE3 2019-12-27 16:29:52 test vectors context";

/* Static guest-owned buffers. The host resolves their addresses through the
 * pointer exports below rather than assuming a linear-memory layout, so the
 * corpus never depends on the guest's stack placement. */
static uint8_t input_buf[BLAKE3_MAX_INPUT];
static uint8_t output_buf[BLAKE3_VECTOR_LEN];

uint32_t
blake3_input_ptr(void)
{
    return (uint32_t)(uintptr_t)input_buf;
}

uint32_t
blake3_output_ptr(void)
{
    return (uint32_t)(uintptr_t)output_buf;
}

static uint32_t
hash_common(uint32_t in, uint32_t len, uint32_t out, uint32_t mode)
{
    blake3_hasher h;
    switch (mode)
    {
        case 0:
            blake3_hasher_init(&h);
            break;
        case 1:
            blake3_hasher_init_keyed(&h, key);
            break;
        default:
            blake3_hasher_init_derive_key(&h, context);
            break;
    }
    blake3_hasher_update(&h, (const void *)(uintptr_t)in, (size_t)len);
    blake3_hasher_finalize_seek(&h, 0, (uint8_t *)(uintptr_t)out, BLAKE3_VECTOR_LEN);
    return 0;
}

uint32_t
blake3_hash(uint32_t in, uint32_t len, uint32_t out)
{
    return hash_common(in, len, out, 0);
}

uint32_t
blake3_keyed_hash(uint32_t in, uint32_t len, uint32_t out)
{
    return hash_common(in, len, out, 1);
}

uint32_t
blake3_derive_key(uint32_t in, uint32_t len, uint32_t out)
{
    return hash_common(in, len, out, 2);
}
