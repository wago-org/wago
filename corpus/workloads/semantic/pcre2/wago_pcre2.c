#include <stdint.h>
#include <string.h>
#define PCRE2_CODE_UNIT_WIDTH 8
#include "pcre2.h"

static uint64_t mix(uint64_t hash, uint64_t value) { return (hash ^ value) * UINT64_C(1099511628211); }

uint64_t pcre2_run(void) {
    static const char *patterns[] = {
        "(?<word>[A-Za-z]+)-(?<number>[0-9]{2,5})",
        "^(?:https?://)?([a-z0-9-]+\\.)+[a-z]{2,}(?:/[^ ]*)?$",
        "(ab|a)+?b", "(?i)straße|STRASSE", "\\b([A-F0-9]{2}:){5}[A-F0-9]{2}\\b"
    };
    static const char *subjects[] = {
        "prefix alpha-31415 suffix", "https://bench.example.org/a/b?q=7", "aaaaabab",
        "Die STRASSE ist lang", "device 0A:1B:2C:3D:4E:5F ready"
    };
    uint64_t hash = UINT64_C(14695981039346656037);
    for (size_t i = 0; i < sizeof(patterns) / sizeof(patterns[0]); i++) {
        int error; PCRE2_SIZE offset;
        pcre2_code *code = pcre2_compile((PCRE2_SPTR)patterns[i], PCRE2_ZERO_TERMINATED,
            PCRE2_UTF | PCRE2_UCP, &error, &offset, NULL);
        if (!code) return 1 + i;
        pcre2_match_data *data = pcre2_match_data_create_from_pattern(code, NULL);
        int count = pcre2_match(code, (PCRE2_SPTR)subjects[i], strlen(subjects[i]), 0, 0, data, NULL);
        if (count < 1) { pcre2_match_data_free(data); pcre2_code_free(code); return 10 + i; }
        PCRE2_SIZE *ovector = pcre2_get_ovector_pointer(data);
        hash = mix(hash, (uint64_t)count);
        for (int j = 0; j < count * 2; j++) hash = mix(hash, ovector[j]);
        pcre2_match_data_free(data); pcre2_code_free(code);
    }
    return hash;
}
