/*
 * Include one unmodified PolyBench/C kernel and turn its dump pass into a
 * deterministic checksum.  The upstream main already initializes, executes,
 * scans every live-out element, and frees its arrays; intercepting fprintf
 * keeps that complete data-flow alive without timing text formatting or I/O.
 */
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

static uint32_t wago_checksum;
#ifdef __wasm__
static uintptr_t wago_heap_cursor;
extern unsigned char __heap_base;

void *polybench_alloc_data(unsigned long long elements, int element_size) {
  uintptr_t bytes = (uintptr_t)elements * (uintptr_t)element_size;
  uintptr_t start = (wago_heap_cursor + 63u) & ~(uintptr_t)63u;
  uintptr_t end = start + bytes;
  uintptr_t current_bytes = __builtin_wasm_memory_size(0) * 65536u;
  if (end < start)
    return NULL;
  if (end > current_bytes) {
    uintptr_t pages = (end - current_bytes + 65535u) / 65536u;
    if (__builtin_wasm_memory_grow(0, pages) == (size_t)-1)
      return NULL;
  }
  wago_heap_cursor = end;
  return (void *)start;
}

static void wago_free(void *ptr) { (void)ptr; }
#else
void *polybench_alloc_data(unsigned long long elements, int element_size) {
  return malloc((size_t)elements * (size_t)element_size);
}

static void wago_free(void *ptr) { free(ptr); }
#endif

static int wago_strcmp(const char *left, const char *right) {
  while (*left != '\0' && *left == *right) {
    ++left;
    ++right;
  }
  return (unsigned char)*left - (unsigned char)*right;
}

static void wago_mix_byte(uint8_t value) {
  wago_checksum ^= value;
  wago_checksum *= UINT32_C(16777619);
}

static void wago_mix_u32(uint32_t value) {
  for (unsigned shift = 0; shift != 32; shift += 8)
    wago_mix_byte((uint8_t)(value >> shift));
}

static void wago_mix_u64(uint64_t value) {
  for (unsigned shift = 0; shift != 64; shift += 8)
    wago_mix_byte((uint8_t)(value >> shift));
}

static char wago_conversion(const char *format) {
  for (; *format != '\0'; ++format) {
    if (*format != '%')
      continue;
    ++format;
    while ((*format >= '0' && *format <= '9') || *format == '.' ||
           *format == '-' || *format == '+' || *format == ' ' ||
           *format == '#' || *format == 'l' || *format == 'h' ||
           *format == 'L' || *format == 'j' || *format == 'z' ||
           *format == 't')
      ++format;
    return *format;
  }
  return '\0';
}

static int wago_fprintf(FILE *stream, const char *format, ...) {
  (void)stream;
  va_list args;
  va_start(args, format);
  char conversion = wago_conversion(format);
  if (conversion == 'd') {
    wago_mix_u32((uint32_t)va_arg(args, int));
  } else if (conversion == 'f' || conversion == 'e' || conversion == 'g') {
    double value = va_arg(args, double);
    int64_t hundredths = (int64_t)(value * 100.0 + (value < 0.0 ? -0.5 : 0.5));
    wago_mix_u64((uint64_t)hundredths);
  }
  va_end(args);
  return 0;
}

#define fprintf wago_fprintf
#define free wago_free
#define strcmp wago_strcmp
#define main polybench_upstream_main
#include POLYBENCH_SOURCE
#undef main
#undef strcmp
#undef free
#undef fprintf

#ifdef __wasm__
#define WAGO_EXPORT(name) __attribute__((export_name(name)))
#else
#define WAGO_EXPORT(name)
#endif

WAGO_EXPORT("polybench_run") uint32_t polybench_run(void) {
  char empty[] = "";
  char *argv[] = {empty, NULL};
  wago_checksum = UINT32_C(2166136261);
#ifdef __wasm__
  wago_heap_cursor = (uintptr_t)&__heap_base;
#endif
  if (polybench_upstream_main(43, argv) != 0)
    return 0;
  return wago_checksum;
}

#ifdef WAGO_POLYBENCH_NATIVE_MAIN
int main(void) {
  printf("%u\n", polybench_run());
  return 0;
}
#endif
