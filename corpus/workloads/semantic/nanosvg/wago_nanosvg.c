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
#define NANOSVGRAST_IMPLEMENTATION
#include "nanosvgrast.h"

static uint64_t raster_summary(const unsigned char *data, size_t len) {
    uint64_t channel_sum = 0;
    uint64_t covered = 0;
    for (size_t i = 0; i < len; i += 4) {
        channel_sum += data[i] + data[i + 1] + data[i + 2] + data[i + 3];
        covered += data[i + 3] != 0;
    }
    return (channel_sum << 16) ^ covered;
}

uint64_t nanosvg_run(void) {
    char svg[] = "<svg xmlns='http://www.w3.org/2000/svg' width='96' height='64' viewBox='0 0 96 64'>"
        "<defs><linearGradient id='g'><stop stop-color='#246'/><stop offset='1' stop-color='#f90'/></linearGradient></defs>"
        "<path fill='url(#g)' stroke='#fff' stroke-width='2' d='M4 58 L26 7 Q48 56 70 7 L92 58 Z'/>"
        "<circle cx='48' cy='32' r='11' fill='#28c' fill-opacity='.7'/></svg>";
    NSVGimage *image = nsvgParse(svg, "px", 96.0f);
    if (!image) return 0;
    NSVGrasterizer *rast = nsvgCreateRasterizer();
    unsigned char *pixels = (unsigned char *)calloc(96u * 64u * 4u, 1);
    if (!rast || !pixels) { nsvgDeleteRasterizer(rast); nsvgDelete(image); free(pixels); return 0; }
    nsvgRasterize(rast, image, 0, 0, 1.0f, pixels, 96, 64, 96 * 4);
    uint64_t hash = raster_summary(pixels, 96u * 64u * 4u);
    free(pixels); nsvgDeleteRasterizer(rast); nsvgDelete(image);
    return hash;
}
