#include <stdint.h>
#include <stddef.h>
#include "xxhash.h"

uint64_t xxhash_run(void) {
    static unsigned char data[131072];
    uint64_t acc = UINT64_C(0x9e3779b185ebca87);
    for (size_t i = 0; i < sizeof(data); i++) data[i] = (unsigned char)((i * 131u + i / 7u) & 255u);
    static const size_t lengths[] = {0, 1, 3, 8, 16, 17, 31, 32, 33, 64, 127, 128, 129, 1024, 65537, sizeof(data)};
    for (size_t i = 0; i < sizeof(lengths) / sizeof(lengths[0]); i++) {
        uint64_t h = XXH3_64bits_withSeed(data + (i & 7), lengths[i], UINT64_C(0x123456789abcdef0) + i);
        acc ^= h + UINT64_C(0x9e3779b97f4a7c15) + (acc << 6) + (acc >> 2);
    }
    return acc;
}
