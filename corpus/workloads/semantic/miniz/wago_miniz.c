#include <stdint.h>
#include <string.h>
#include "miniz.h"

static uint64_t hash_bytes(uint64_t hash, const unsigned char *data, size_t len) {
    for (size_t i = 0; i < len; i++) hash = (hash ^ data[i]) * UINT64_C(1099511628211);
    return hash;
}

uint64_t miniz_run(void) {
    static unsigned char input[131072];
    static unsigned char compressed[140000];
    static unsigned char output[131072];
    for (size_t i = 0; i < sizeof(input); i++)
        input[i] = (unsigned char)(((i * 17u) ^ (i >> 3) ^ (i % 251u)) & 255u);
    mz_ulong compressed_len = sizeof(compressed);
    if (mz_compress2(compressed, &compressed_len, input, sizeof(input), 6) != MZ_OK) return 1;
    mz_ulong output_len = sizeof(output);
    if (mz_uncompress(output, &output_len, compressed, compressed_len) != MZ_OK) return 2;
    if (output_len != sizeof(input) || memcmp(input, output, sizeof(input)) != 0) return 3;
    uint64_t hash = hash_bytes(UINT64_C(14695981039346656037), compressed, compressed_len);
    hash = hash_bytes(hash, output, sizeof(output));
    return hash ^ compressed_len;
}
