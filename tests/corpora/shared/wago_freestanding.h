/* Wago semantic-corpus shared freestanding shim.
 *
 * Codec runners compiled to import-free wasm32 with `-nostdlib` still need a
 * handful of libc functions (memcpy/memset/memmove/memcmp/strlen) and, for
 * libraries that allocate through malloc/free, a bounded allocator. This shim
 * provides both so each corpus runner can stay deterministic and import-free.
 * It is a reviewed Wago port, not upstream source.
 */
#ifndef WAGO_FREESTANDING_H
#define WAGO_FREESTANDING_H

#include <stddef.h>

void *wago_malloc(size_t n);
void  wago_free(void *p);
void *wago_calloc(size_t count, size_t size);
void *wago_realloc(void *p, size_t n);

/* libc-compatible aliases over the same bounded bump arena. Libraries that
 * allocate through malloc/calloc/free (zlib, zstd) resolve to these instead of
 * a host libc. realloc is intentionally absent: the bump arena cannot reclaim
 * or shrink, and neither zlib inflate nor zstd decompress needs it. */
void *malloc(size_t n);
void *calloc(size_t count, size_t size);
void  free(void *p);

#endif /* WAGO_FREESTANDING_H */
