/* Wago semantic-corpus shared freestanding shim (see wago_freestanding.h). */
#include "wago_freestanding.h"

#include <stdint.h>

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
memmove(void *dst, const void *src, size_t n)
{
    uint8_t       *d = (uint8_t *)dst;
    const uint8_t *s = (const uint8_t *)src;
    if (d < s)
    {
        size_t i;
        for (i = 0; i < n; i++)
            d[i] = s[i];
    }
    else if (d > s)
    {
        size_t i;
        for (i = n; i > 0; i--)
            d[i - 1] = s[i - 1];
    }
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

int
memcmp(const void *a, const void *b, size_t n)
{
    const uint8_t *x = (const uint8_t *)a;
    const uint8_t *y = (const uint8_t *)b;
    size_t         i;
    for (i = 0; i < n; i++)
    {
        if (x[i] != y[i])
            return (int)x[i] - (int)y[i];
    }
    return 0;
}

size_t
strlen(const char *s)
{
    size_t n = 0;
    while (s[n])
        n++;
    return n;
}

/* Bounded bump arena. `free` is a no-op: each corpus case runs on a fresh
 * instance and the total allocation across one case is bounded well below the
 * arena size, so leaking is deterministic and safe. A NULL return means the
 * arena is exhausted, which the runner must surface (not silently ignore). */
#define WAGO_ARENA_SIZE (512 * 1024)

static uint8_t arena[WAGO_ARENA_SIZE];
static size_t  arena_used;

void
wago_allocator_reset(void)
{
    arena_used = 0;
}

static size_t
align_up(size_t n, size_t a)
{
    return (n + a - 1) & ~(a - 1);
}

void *
wago_malloc(size_t n)
{
    void *p;
    n = align_up(n, 8);
    if (n == 0 || arena_used + n > WAGO_ARENA_SIZE)
        return NULL;
    p = arena + arena_used;
    arena_used += n;
    return p;
}

void
wago_free(void *p)
{
    (void)p;
}

void *
wago_calloc(size_t count, size_t size)
{
    size_t total = count * size;
    void  *p = wago_malloc(total);
    if (p != NULL)
        memset(p, 0, total);
    return p;
}

void *
wago_realloc(void *p, size_t n)
{
    (void)p;
    return wago_malloc(n);
}

/* libc-compatible aliases: zlib's zcalloc/zcfree and zstd's ZSTD_malloc/
 * ZSTD_calloc/ZSTD_free resolve to the bounded arena through these. */
void *
malloc(size_t n)
{
    return wago_malloc(n);
}

void *
calloc(size_t count, size_t size)
{
    return wago_calloc(count, size);
}

void
free(void *p)
{
    wago_free(p);
}
