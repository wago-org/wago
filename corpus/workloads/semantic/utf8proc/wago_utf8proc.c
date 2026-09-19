#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "utf8proc.h"

static uint64_t fnv1a(const void *data, size_t len) {
    const unsigned char *p = (const unsigned char *)data;
    uint64_t hash = UINT64_C(14695981039346656037);
    for (size_t i = 0; i < len; i++) {
        hash ^= p[i];
        hash *= UINT64_C(1099511628211);
    }
    return hash;
}

uint64_t utf8proc_run(void) {
    static const utf8proc_uint8_t input[] =
        "Stra\xC3\x9F" "e A\xCC\x8A \xEF\xAC\x83 \xCE\x9F\xCE\x94\xCE\xA5\xCE\xA3\xCE\xA3\xCE\x95\xCE\x8E\xCE\xA3";
    utf8proc_uint8_t *out = NULL;
    utf8proc_ssize_t len = utf8proc_map(input, 0, &out,
        UTF8PROC_NULLTERM | UTF8PROC_STABLE | UTF8PROC_COMPOSE |
        UTF8PROC_COMPAT | UTF8PROC_CASEFOLD);
    if (len < 0 || !out) return 0;
    uint64_t hash = fnv1a(out, (size_t)len);
    free(out);
    return hash;
}
