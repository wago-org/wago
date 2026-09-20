#include <cstdint>
#include <cstdlib>
#include <cstring>
#include "lodepng.h"

static uint64_t hash_bytes(uint64_t hash, const unsigned char *data, size_t len) {
    for (size_t i = 0; i < len; i++) hash = (hash ^ data[i]) * UINT64_C(1099511628211);
    return hash;
}

extern "C" uint64_t lodepng_run(void) {
    enum { width = 97, height = 65, bytes = width * height * 4 };
    static unsigned char image[bytes];
    for (unsigned y = 0; y < height; y++) for (unsigned x = 0; x < width; x++) {
        size_t p = (y * width + x) * 4;
        image[p] = (unsigned char)(x * 3u + y); image[p + 1] = (unsigned char)(y * 5u + x);
        image[p + 2] = (unsigned char)((x ^ y) * 7u); image[p + 3] = (unsigned char)(255u - ((x * y) & 63u));
    }
    unsigned char *png = nullptr; size_t png_size = 0;
    if (lodepng_encode32(&png, &png_size, image, width, height) != 0) return 1;
    unsigned char *decoded = nullptr; unsigned decoded_width = 0, decoded_height = 0;
    if (lodepng_decode32(&decoded, &decoded_width, &decoded_height, png, png_size) != 0) { free(png); return 2; }
    if (decoded_width != width || decoded_height != height || memcmp(image, decoded, bytes) != 0) {
        free(decoded); free(png); return 3;
    }
    uint64_t hash = hash_bytes(UINT64_C(14695981039346656037), png, png_size);
    hash = hash_bytes(hash, decoded, bytes);
    free(decoded); free(png);
    return hash ^ png_size;
}
