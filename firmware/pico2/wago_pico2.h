#ifndef WAGO_PICO2_H
#define WAGO_PICO2_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define WAGO_PICO2_TRANSPORT_MAGIC UINT32_C(0x4f474157)
#define WAGO_PICO2_TRANSPORT_VERSION UINT16_C(1)
#define WAGO_PICO2_TRANSPORT_HEADER_BYTES UINT32_C(24)
#define WAGO_PICO2_TRANSPORT_RESPONSE_MASK UINT16_C(0x8000)
#define WAGO_PICO2_TRANSPORT_CALL_HEADER_BYTES UINT32_C(12)
#define WAGO_PICO2_TRANSPORT_HELLO_BYTES UINT32_C(16)

#define WAGO_PICO2_CONTEXT_ABI_BYTES UINT32_C(84)
#define WAGO_PICO2_CALL_ABI_BYTES UINT32_C(12)
#define WAGO_PICO2_CONTEXT_TRAP_CELL_OFFSET UINT32_C(8)
#define WAGO_PICO2_CONTEXT_CANCEL_CELL_OFFSET UINT32_C(12)
#define WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET UINT32_C(16)

#define WAGO_PICO2_HELPER_F64_OFFSET UINT32_C(0)
#define WAGO_PICO2_HELPER_SIMD_OFFSET UINT32_C(4)
#define WAGO_PICO2_HELPER_I64_OFFSET UINT32_C(8)
#define WAGO_PICO2_HELPER_F32_OFFSET UINT32_C(12)
#define WAGO_PICO2_HELPER_TABLE_BYTES UINT32_C(16)

#define WAGO_PICO2_TARGET_ARM32 UINT32_C(1)
#define WAGO_PICO2_TARGET_RISCV32 UINT32_C(2)

typedef uint32_t wago_pico2_code;

enum wago_pico2_kind {
    WAGO_PICO2_HELLO = 1,
    WAGO_PICO2_INSTANTIATE = 2,
    WAGO_PICO2_START = 3,
    WAGO_PICO2_CALL = 4,
    WAGO_PICO2_CANCEL = 5,
    WAGO_PICO2_RESET = 6,
};

#define WAGO_PICO2_OK UINT32_C(0)
#define WAGO_PICO2_BAD_FRAME UINT32_C(0x80000001)
#define WAGO_PICO2_UNSUPPORTED UINT32_C(0x80000002)
#define WAGO_PICO2_CAPACITY UINT32_C(0x80000003)
#define WAGO_PICO2_STATE UINT32_C(0x80000004)

enum wago_pico2_dispatch_result {
    WAGO_PICO2_DISPATCH_OK = 0,
    WAGO_PICO2_DISPATCH_FRAME = -1,
    WAGO_PICO2_DISPATCH_CHECKSUM = -2,
    WAGO_PICO2_DISPATCH_CAPACITY = -3,
    WAGO_PICO2_DISPATCH_IO = -4,
};

struct wago_pico2_call_abi {
    uint32_t context;
    uint32_t parameters;
    uint32_t results;
};

struct wago_pico2_f32_frame {
    uint32_t op;
    uint32_t a_lo;
    uint32_t a_hi;
    uint32_t b_lo;
    uint32_t b_hi;
    uint32_t out_lo;
    uint32_t out_hi;
    uint32_t trap;
};

struct wago_pico2_f64_frame {
    uint32_t op;
    uint32_t a_lo;
    uint32_t a_hi;
    uint32_t b_lo;
    uint32_t b_hi;
    uint32_t out_lo;
    uint32_t out_hi;
    uint32_t trap;
};

struct wago_pico2_i64_frame {
    uint32_t op;
    uint32_t a_lo;
    uint32_t a_hi;
    uint32_t b_lo;
    uint32_t b_hi;
    uint32_t out_lo;
    uint32_t out_hi;
    uint32_t i32_out;
    uint32_t trap;
};

struct wago_pico2_simd_frame {
    uint32_t op;
    uint32_t scalar_lo;
    uint32_t scalar_hi;
    uint32_t a[4];
    uint32_t b[4];
    uint32_t c[4];
    uint32_t immediate[4];
    uint32_t out[4];
    uint32_t scalar_out_lo;
    uint32_t scalar_out_hi;
    uint32_t memory_base;
    uint32_t memory_length;
    uint32_t address;
    uint32_t lane;
    uint32_t trap;
};

typedef void (*wago_pico2_f32_helper)(struct wago_pico2_f32_frame *frame);
typedef void (*wago_pico2_f64_helper)(struct wago_pico2_f64_frame *frame);
typedef void (*wago_pico2_i64_helper)(struct wago_pico2_i64_frame *frame);
typedef void (*wago_pico2_simd_helper)(struct wago_pico2_simd_frame *frame);

struct wago_pico2_helper_callbacks {
    wago_pico2_f64_helper f64;
    wago_pico2_simd_helper simd;
    wago_pico2_i64_helper i64;
    wago_pico2_f32_helper f32;
};

struct wago_pico2_helper_entries {
    uint32_t f64;
    uint32_t simd;
    uint32_t i64;
    uint32_t f32;
};

/* Fixed symbols exported by the allocation-free embedded32 helper package. */
void wago_embedded32_f64(struct wago_pico2_f64_frame *frame);
void wago_embedded32_simd_abi(struct wago_pico2_simd_frame *frame);
void wago_embedded32_i64(struct wago_pico2_i64_frame *frame);
void wago_embedded32_f32(struct wago_pico2_f32_frame *frame);

#define WAGO_PICO2_EMBEDDED32_HELPER_CALLBACKS \
    {wago_embedded32_f64, wago_embedded32_simd_abi, \
     wago_embedded32_i64, wago_embedded32_f32}

struct wago_pico2_function {
    uint32_t address;
    uint32_t context;
    uint16_t parameter_slots;
    uint16_t result_slots;
};

struct wago_pico2_image {
    uint32_t target;
    uint32_t maximum_payload;
    uint32_t load_address;
    uint8_t *image;
    const uint8_t *initial_image;
    uint32_t image_size;
    uint32_t context_address;
    uint32_t start_address;
    const struct wago_pico2_function *functions;
    uint32_t function_count;
    const uint32_t *contexts;
    uint32_t context_count;
};

struct wago_pico2_runner;

struct wago_pico2_invoker {
    void *user;
    wago_pico2_code (*instantiate)(void *user, struct wago_pico2_runner *runner);
    wago_pico2_code (*start)(void *user, uint32_t entry, uint32_t context);
    wago_pico2_code (*call)(void *user, uint32_t entry, uint32_t context,
                            const uint32_t *parameters, uint32_t parameter_slots,
                            uint32_t *results, uint32_t result_slots);
    wago_pico2_code (*cancel)(void *user, uint32_t context);
    wago_pico2_code (*reset)(void *user, struct wago_pico2_runner *runner);
};

struct wago_pico2_runner {
    uint32_t target;
    uint32_t maximum_payload;
    uint32_t context_address;
    uint32_t start_address;
    const struct wago_pico2_function *functions;
    uint32_t function_count;

    uint8_t *image;
    const uint8_t *initial_image;
    uint32_t image_size;

    const struct wago_pico2_invoker *invoker;
    uint8_t initialized;
    uint8_t started;

    uint32_t image_address;
    const uint32_t *contexts;
    uint32_t context_count;
    struct wago_pico2_helper_entries helpers;
};

struct wago_pico2_endpoint {
    struct wago_pico2_runner *runner;
    uint32_t *parameter_slots;
    uint32_t parameter_capacity;
    uint32_t *result_slots;
    uint32_t result_capacity;
    uint8_t *payload_scratch;
    uint32_t payload_capacity;
    uint32_t maximum_payload;
};

struct wago_pico2_io {
    void *user;
    int (*read)(void *user, uint8_t *destination, uint32_t length);
    int (*write)(void *user, const uint8_t *source, uint32_t length);
};

uint32_t wago_pico2_crc32(const uint8_t *first, uint32_t first_length,
                          const uint8_t *second, uint32_t second_length);

wago_pico2_code wago_pico2_helper_entries_init(
    struct wago_pico2_helper_entries *entries, uint32_t target,
    const struct wago_pico2_helper_callbacks *callbacks);
wago_pico2_code wago_pico2_runner_init(
    struct wago_pico2_runner *runner, const struct wago_pico2_image *image,
    const struct wago_pico2_helper_entries *helpers);

wago_pico2_code wago_pico2_runner_instantiate(struct wago_pico2_runner *runner);
wago_pico2_code wago_pico2_runner_start(struct wago_pico2_runner *runner);
wago_pico2_code wago_pico2_runner_call(struct wago_pico2_runner *runner,
                                        uint32_t export_ordinal,
                                        const uint32_t *parameters,
                                        uint32_t parameter_slots,
                                        uint32_t *results,
                                        uint32_t result_slots);
wago_pico2_code wago_pico2_runner_cancel(struct wago_pico2_runner *runner);
wago_pico2_code wago_pico2_runner_reset(struct wago_pico2_runner *runner);

int wago_pico2_dispatch(struct wago_pico2_endpoint *endpoint,
                        const uint8_t *request, uint32_t request_length,
                        uint8_t *response, uint32_t response_capacity,
                        uint32_t *response_length);

int wago_pico2_serve_once(struct wago_pico2_endpoint *endpoint,
                          const struct wago_pico2_io *io,
                          uint8_t *request, uint32_t request_capacity,
                          uint8_t *response, uint32_t response_capacity);

extern const struct wago_pico2_invoker wago_pico2_direct_invoker;

#ifdef WAGO_PICO2_PICO_SDK
int wago_pico2_pico_sdk_serve(struct wago_pico2_endpoint *endpoint,
                              uint8_t *request, uint32_t request_capacity,
                              uint8_t *response, uint32_t response_capacity);
#endif

#ifdef __cplusplus
}
#endif

#endif
