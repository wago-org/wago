#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "kiss_fft.h"

static uint64_t fnv1a(const void *data, size_t len) {
    const unsigned char *p = (const unsigned char *)data;
    uint64_t hash = UINT64_C(14695981039346656037);
    for (size_t i = 0; i < len; i++) { hash ^= p[i]; hash *= UINT64_C(1099511628211); }
    return hash;
}

uint64_t kissfft_run(void) {
    enum { N = 257 };
    kiss_fft_cpx input[N], frequency[N], roundtrip[N];
    for (int i = 0; i < N; i++) {
        input[i].r = (kiss_fft_scalar)((i % 17) - 8) / 8.0f;
        input[i].i = (kiss_fft_scalar)((i % 11) - 5) / 7.0f;
    }
    kiss_fft_cfg forward = kiss_fft_alloc(N, 0, NULL, NULL);
    kiss_fft_cfg inverse = kiss_fft_alloc(N, 1, NULL, NULL);
    if (!forward || !inverse) { free(forward); free(inverse); return 0; }
    kiss_fft(forward, input, frequency);
    kiss_fft(inverse, frequency, roundtrip);
    uint64_t hash = fnv1a(frequency, sizeof(frequency));
    hash ^= fnv1a(roundtrip, sizeof(roundtrip));
    free(forward); free(inverse);
    return hash;
}
