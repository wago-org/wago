#include <cstdint>
#include <cstring>
#include "fast_float/fast_float.h"

static uint64_t mix(uint64_t hash, uint64_t value) {
    return (hash ^ value) * UINT64_C(1099511628211);
}

extern "C" uint64_t fastfloat_run(void) {
    static const char *inputs[] = {
        "0", "-0", "1.5", "3.141592653589793238462643383279", "2.2250738585072014e-308",
        "1.7976931348623157e308", "9007199254740991", "0.1000000000000000055511151231257827",
        "6.02214076e23", "-7.3177701707893310e+15", "4.9406564584124654e-324"
    };
    uint64_t hash = UINT64_C(14695981039346656037);
    for (const char *text : inputs) {
        double value = 0;
        const char *end = text + strlen(text);
        auto result = fast_float::from_chars(text, end, value);
        if (result.ec != std::errc() || result.ptr != end) return 1;
        uint64_t bits; memcpy(&bits, &value, sizeof(bits)); hash = mix(hash, bits);
    }
    return hash;
}
