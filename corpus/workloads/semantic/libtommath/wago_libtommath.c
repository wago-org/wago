#include <stdint.h>
#include <stddef.h>
#include "tommath.h"

static uint64_t fnv1a(const unsigned char *data, size_t len) {
    uint64_t hash = UINT64_C(14695981039346656037);
    for (size_t i = 0; i < len; i++) { hash ^= data[i]; hash *= UINT64_C(1099511628211); }
    return hash;
}

uint64_t libtommath_run(void) {
    mp_int base, exponent, modulus, result, gcd;
    unsigned char out[512];
    size_t written = 0;
    if (mp_init_multi(&base, &exponent, &modulus, &result, &gcd, NULL) != MP_OKAY) return 0;
    if (mp_read_radix(&base, "123456789abcdef0123456789abcdef0123456789abcdef", 16) != MP_OKAY ||
        mp_read_radix(&exponent, "10001", 16) != MP_OKAY ||
        mp_read_radix(&modulus, "fffffffffffffffffffffffffffffffeffffffffffffffff", 16) != MP_OKAY ||
        mp_exptmod(&base, &exponent, &modulus, &result) != MP_OKAY ||
        mp_gcd(&base, &modulus, &gcd) != MP_OKAY ||
        mp_to_ubin(&result, out, sizeof(out), &written) != MP_OKAY) {
        mp_clear_multi(&base, &exponent, &modulus, &result, &gcd, NULL);
        return 0;
    }
    uint64_t hash = fnv1a(out, written);
    if (mp_to_ubin(&gcd, out, sizeof(out), &written) != MP_OKAY) hash = 0;
    else hash ^= fnv1a(out, written);
    mp_clear_multi(&base, &exponent, &modulus, &result, &gcd, NULL);
    return hash;
}
