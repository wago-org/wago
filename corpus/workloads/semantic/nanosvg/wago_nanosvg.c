#include <stdint.h>
#include <stdlib.h>
#include <stdarg.h>

static int hex_digit(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

/* NanoSVG uses sscanf only for #rgb/#rrggbb and integer rgb(). Keeping this
 * tiny parser local avoids dragging stdio/WASI imports into a core module. */
int sscanf(const char *str, const char *format, ...) {
    va_list ap;
    va_start(ap, format);
    if (format[0] == '#' && str[0] == '#') {
        unsigned int *r = va_arg(ap, unsigned int *);
        unsigned int *g = va_arg(ap, unsigned int *);
        unsigned int *b = va_arg(ap, unsigned int *);
        int wide = format[2] == '2';
        int step = wide ? 2 : 1;
        int values[3];
        for (int i = 0; i < 3; i++) {
            int hi = hex_digit(str[1 + i * step]);
            int lo = wide ? hex_digit(str[2 + i * step]) : 0;
            if (hi < 0 || lo < 0) { va_end(ap); return i; }
            values[i] = wide ? hi * 16 + lo : hi;
        }
        *r = (unsigned int)values[0]; *g = (unsigned int)values[1]; *b = (unsigned int)values[2];
        va_end(ap); return 3;
    }
    if (format[0] == 'r' && str[0] == 'r') {
        unsigned int *out[3] = {va_arg(ap, unsigned int *), va_arg(ap, unsigned int *), va_arg(ap, unsigned int *)};
        const char *p = str + 4;
        for (int i = 0; i < 3; i++) {
            while (*p == ' ') p++;
            if (*p < '0' || *p > '9') { va_end(ap); return i; }
            unsigned int value = 0;
            while (*p >= '0' && *p <= '9') value = value * 10 + (unsigned int)(*p++ - '0');
            *out[i] = value;
            while (*p == ' ') p++;
            if (i < 2 && *p++ != ',') { va_end(ap); return i + 1; }
        }
        va_end(ap); return 3;
    }
    va_end(ap); return 0;
}

#define NANOSVG_IMPLEMENTATION
#include "nanosvg.h"
static uint64_t mix(uint64_t hash, uint64_t value) {
    return (hash ^ value) * UINT64_C(1099511628211);
}

uint64_t nanosvg_run(void) {
    char svg[] = "<svg xmlns='http://www.w3.org/2000/svg' width='96' height='64' viewBox='0 0 96 64'>"
        "<rect x='4' y='4' width='40' height='24' fill='#246'/>"
        "<rect x='52' y='8' width='36' height='48' fill='#f90'/>"
        "<path fill='#28c' d='M8 36 L44 36 L44 56 L8 56 Z'/></svg>";
    NSVGimage *image = nsvgParse(svg, "px", 96.0f);
    if (!image) return 1;
    uint64_t hash = UINT64_C(14695981039346656037);
    hash = mix(hash, (uint64_t)image->width);
    hash = mix(hash, (uint64_t)image->height);
    for (NSVGshape *shape = image->shapes; shape; shape = shape->next) {
        hash = mix(hash, shape->fill.type);
        hash = mix(hash, shape->fill.color);
        hash = mix(hash, shape->stroke.type);
        hash = mix(hash, shape->stroke.color);
        hash = mix(hash, (uint64_t)shape->bounds[0]);
        hash = mix(hash, (uint64_t)shape->bounds[1]);
        hash = mix(hash, (uint64_t)shape->bounds[2]);
        hash = mix(hash, (uint64_t)shape->bounds[3]);
        for (NSVGpath *path = shape->paths; path; path = path->next) {
            hash = mix(hash, (uint64_t)path->npts);
            hash = mix(hash, (uint64_t)path->closed);
        }
    }
    nsvgDelete(image);
    return hash;
}
