#include "wago_pico2.h"

#include <limits.h>

static void wago_copy(uint8_t *destination, const uint8_t *source, uint32_t length) {
    uint32_t i;
    for (i = 0; i < length; ++i) {
        destination[i] = source[i];
    }
}

static uint16_t wago_get16(const uint8_t *p) {
    return (uint16_t)((uint16_t)p[0] | (uint16_t)((uint16_t)p[1] << 8));
}

static uint32_t wago_get32(const uint8_t *p) {
    return (uint32_t)p[0] |
           ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) |
           ((uint32_t)p[3] << 24);
}

static void wago_put16(uint8_t *p, uint16_t value) {
    p[0] = (uint8_t)value;
    p[1] = (uint8_t)(value >> 8);
}

static void wago_put32(uint8_t *p, uint32_t value) {
    p[0] = (uint8_t)value;
    p[1] = (uint8_t)(value >> 8);
    p[2] = (uint8_t)(value >> 16);
    p[3] = (uint8_t)(value >> 24);
}

static int wago_kind_valid(uint16_t kind) {
    uint16_t base = (uint16_t)(kind & (uint16_t)~WAGO_PICO2_TRANSPORT_RESPONSE_MASK);
    return base >= WAGO_PICO2_HELLO && base <= WAGO_PICO2_RESET;
}

static uint32_t wago_crc_part(uint32_t crc, const uint8_t *bytes, uint32_t length) {
    uint32_t i;
    for (i = 0; i < length; ++i) {
        uint32_t bit;
        crc ^= bytes[i];
        for (bit = 0; bit < 8; ++bit) {
            uint32_t mask = 0u - (crc & 1u);
            crc = (crc >> 1) ^ (UINT32_C(0xedb88320) & mask);
        }
    }
    return crc;
}

uint32_t wago_pico2_crc32(const uint8_t *first, uint32_t first_length,
                          const uint8_t *second, uint32_t second_length) {
    uint32_t crc = UINT32_MAX;
    if (first_length != 0) {
        crc = wago_crc_part(crc, first, first_length);
    }
    if (second_length != 0) {
        crc = wago_crc_part(crc, second, second_length);
    }
    return ~crc;
}

static int wago_helper_address(uint32_t *out, uint32_t target,
                               const void *function_object, size_t function_size) {
#if UINTPTR_MAX == UINT32_MAX
    uintptr_t address = 0;
    if (out == NULL || function_object == NULL || function_size != sizeof(address)) {
        return 0;
    }
    wago_copy((uint8_t *)&address, (const uint8_t *)function_object,
              (uint32_t)function_size);
    if (target == WAGO_PICO2_TARGET_ARM32) {
        address |= (uintptr_t)1;
    } else if (target != WAGO_PICO2_TARGET_RISCV32 || (address & (uintptr_t)1) != 0) {
        return 0;
    }
    if (address == 0) {
        return 0;
    }
    *out = (uint32_t)address;
    return 1;
#else
    (void)out;
    (void)target;
    (void)function_object;
    (void)function_size;
    return 0;
#endif
}

wago_pico2_code wago_pico2_helper_entries_init(
    struct wago_pico2_helper_entries *entries, uint32_t target,
    const struct wago_pico2_helper_callbacks *callbacks) {
    struct wago_pico2_helper_entries out;
    if (entries == NULL || callbacks == NULL || callbacks->f64 == NULL ||
        callbacks->simd == NULL || callbacks->i64 == NULL || callbacks->f32 == NULL) {
        return WAGO_PICO2_STATE;
    }
    if (!wago_helper_address(&out.f64, target, &callbacks->f64, sizeof(callbacks->f64)) ||
        !wago_helper_address(&out.simd, target, &callbacks->simd, sizeof(callbacks->simd)) ||
        !wago_helper_address(&out.i64, target, &callbacks->i64, sizeof(callbacks->i64)) ||
        !wago_helper_address(&out.f32, target, &callbacks->f32, sizeof(callbacks->f32))) {
        return WAGO_PICO2_UNSUPPORTED;
    }
    *entries = out;
    return WAGO_PICO2_OK;
}

static int wago_address_range(uint32_t base, uint32_t image_size,
                              uint32_t address, uint32_t length, uint32_t *offset) {
    uint64_t start;
    uint64_t end;
    uint64_t image_end;
    if (offset == NULL || address < base) {
        return 0;
    }
    start = (uint64_t)address;
    end = start + (uint64_t)length;
    image_end = (uint64_t)base + (uint64_t)image_size;
    if (end < start || end > image_end) {
        return 0;
    }
    *offset = address - base;
    return 1;
}

static int wago_code_range(const struct wago_pico2_image *image, uint32_t callable) {
    uint32_t address = callable;
    uint32_t offset;
    if (image->target == WAGO_PICO2_TARGET_ARM32) {
        if ((address & 1u) == 0) {
            return 0;
        }
        address &= ~UINT32_C(1);
    } else if ((address & 1u) != 0) {
        return 0;
    }
    return wago_address_range(image->load_address, image->image_size,
                              address, 1, &offset);
}

wago_pico2_code wago_pico2_runner_init(
    struct wago_pico2_runner *runner, const struct wago_pico2_image *image,
    const struct wago_pico2_helper_entries *helpers) {
    uint64_t image_end;
    uint32_t i;
    uint32_t count;
    if (runner == NULL || image == NULL || helpers == NULL ||
        (image->target != WAGO_PICO2_TARGET_ARM32 &&
         image->target != WAGO_PICO2_TARGET_RISCV32) ||
        image->maximum_payload == 0 || image->load_address == 0 ||
        image->image == NULL || image->initial_image == NULL || image->image_size == 0 ||
        image->context_address == 0 ||
        (image->function_count != 0 && image->functions == NULL) ||
        (image->context_count != 0 && image->contexts == NULL) ||
        helpers->f64 == 0 || helpers->simd == 0 || helpers->i64 == 0 || helpers->f32 == 0) {
        return WAGO_PICO2_STATE;
    }
    image_end = (uint64_t)image->load_address + (uint64_t)image->image_size;
    if (image_end > (uint64_t)UINT32_MAX + 1u ||
        image->context_address < image->load_address ||
        (uint64_t)image->context_address + WAGO_PICO2_CONTEXT_ABI_BYTES > image_end) {
        return WAGO_PICO2_STATE;
    }
    if (image->start_address != 0 && !wago_code_range(image, image->start_address)) {
        return WAGO_PICO2_STATE;
    }
    for (i = 0; i < image->function_count; ++i) {
        uint32_t function_context = image->functions[i].context != 0 ?
                                    image->functions[i].context : image->context_address;
        uint32_t context_offset;
        if (!wago_code_range(image, image->functions[i].address) ||
            !wago_address_range(image->load_address, image->image_size, function_context,
                                WAGO_PICO2_CONTEXT_ABI_BYTES, &context_offset)) {
            return WAGO_PICO2_STATE;
        }
    }
    count = image->context_count != 0 ? image->context_count : 1;
    for (i = 0; i < count; ++i) {
        uint32_t context = image->context_count != 0 ? image->contexts[i] : image->context_address;
        uint32_t context_offset;
        uint32_t helper_address;
        uint32_t helper_offset;
        if (!wago_address_range(image->load_address, image->image_size, context,
                                WAGO_PICO2_CONTEXT_ABI_BYTES, &context_offset)) {
            return WAGO_PICO2_STATE;
        }
        helper_address = wago_get32(image->initial_image + context_offset +
                                    WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET);
        if (!wago_address_range(image->load_address, image->image_size, helper_address,
                                WAGO_PICO2_HELPER_TABLE_BYTES, &helper_offset)) {
            return WAGO_PICO2_STATE;
        }
    }
    runner->target = image->target;
    runner->maximum_payload = image->maximum_payload;
    runner->context_address = image->context_address;
    runner->start_address = image->start_address;
    runner->functions = image->functions;
    runner->function_count = image->function_count;
    runner->image = image->image;
    runner->initial_image = image->initial_image;
    runner->image_size = image->image_size;
    runner->invoker = &wago_pico2_direct_invoker;
    runner->initialized = 0;
    runner->started = 0;
    runner->image_address = image->load_address;
    runner->contexts = image->contexts;
    runner->context_count = image->context_count;
    runner->helpers = *helpers;
    return WAGO_PICO2_OK;
}

static const struct wago_pico2_invoker *wago_invoker(const struct wago_pico2_runner *runner) {
    return runner->invoker != NULL ? runner->invoker : &wago_pico2_direct_invoker;
}

static int wago_runner_valid(const struct wago_pico2_runner *runner) {
    return runner != NULL && runner->target != 0 && runner->maximum_payload != 0 &&
           runner->context_address != 0 &&
           (runner->function_count == 0 || runner->functions != NULL);
}

wago_pico2_code wago_pico2_runner_instantiate(struct wago_pico2_runner *runner) {
    const struct wago_pico2_invoker *invoker;
    wago_pico2_code code;
    if (!wago_runner_valid(runner) || runner->initialized) {
        return WAGO_PICO2_STATE;
    }
    invoker = wago_invoker(runner);
    if (invoker->instantiate == NULL) {
        return WAGO_PICO2_UNSUPPORTED;
    }
    code = invoker->instantiate(invoker->user, runner);
    if (code == WAGO_PICO2_OK) {
        runner->initialized = 1;
        runner->started = 0;
    }
    return code;
}

wago_pico2_code wago_pico2_runner_start(struct wago_pico2_runner *runner) {
    const struct wago_pico2_invoker *invoker;
    wago_pico2_code code;
    if (!wago_runner_valid(runner) || !runner->initialized || runner->started) {
        return WAGO_PICO2_STATE;
    }
    if (runner->start_address == 0) {
        runner->started = 1;
        return WAGO_PICO2_OK;
    }
    invoker = wago_invoker(runner);
    if (invoker->start == NULL) {
        return WAGO_PICO2_UNSUPPORTED;
    }
    code = invoker->start(invoker->user, runner->start_address, runner->context_address);
    if (code == WAGO_PICO2_OK) {
        runner->started = 1;
    }
    return code;
}

wago_pico2_code wago_pico2_runner_call(struct wago_pico2_runner *runner,
                                        uint32_t export_ordinal,
                                        const uint32_t *parameters,
                                        uint32_t parameter_slots,
                                        uint32_t *results,
                                        uint32_t result_slots) {
    const struct wago_pico2_invoker *invoker;
    const struct wago_pico2_function *function;
    uint32_t context;
    if (!wago_runner_valid(runner) || !runner->initialized || !runner->started ||
        export_ordinal >= runner->function_count) {
        return WAGO_PICO2_STATE;
    }
    function = &runner->functions[export_ordinal];
    if (function->address == 0 || function->parameter_slots != parameter_slots ||
        function->result_slots != result_slots ||
        (parameter_slots != 0 && parameters == NULL) ||
        (result_slots != 0 && results == NULL)) {
        return WAGO_PICO2_STATE;
    }
    invoker = wago_invoker(runner);
    if (invoker->call == NULL) {
        return WAGO_PICO2_UNSUPPORTED;
    }
    context = function->context != 0 ? function->context : runner->context_address;
    return invoker->call(invoker->user, function->address, context,
                         parameters, parameter_slots, results, result_slots);
}

wago_pico2_code wago_pico2_runner_cancel(struct wago_pico2_runner *runner) {
    const struct wago_pico2_invoker *invoker;
    if (!wago_runner_valid(runner) || !runner->initialized) {
        return WAGO_PICO2_STATE;
    }
    invoker = wago_invoker(runner);
    if (invoker->cancel == NULL) {
        return WAGO_PICO2_UNSUPPORTED;
    }
    return invoker->cancel(invoker->user, runner->context_address);
}

wago_pico2_code wago_pico2_runner_reset(struct wago_pico2_runner *runner) {
    const struct wago_pico2_invoker *invoker;
    wago_pico2_code code;
    if (!wago_runner_valid(runner) || !runner->initialized) {
        return WAGO_PICO2_STATE;
    }
    invoker = wago_invoker(runner);
    if (invoker->reset == NULL) {
        return WAGO_PICO2_UNSUPPORTED;
    }
    code = invoker->reset(invoker->user, runner);
    if (code == WAGO_PICO2_OK) {
        runner->initialized = 0;
        runner->started = 0;
    }
    return code;
}

static int wago_encode_response(uint8_t *response, uint32_t response_capacity,
                                uint16_t request_kind, uint32_t sequence,
                                wago_pico2_code code,
                                const uint8_t *payload, uint32_t payload_length,
                                uint32_t *response_length) {
    uint32_t total;
    if (payload_length > UINT32_MAX - WAGO_PICO2_TRANSPORT_HEADER_BYTES) {
        return WAGO_PICO2_DISPATCH_FRAME;
    }
    total = WAGO_PICO2_TRANSPORT_HEADER_BYTES + payload_length;
    if (total > response_capacity) {
        return WAGO_PICO2_DISPATCH_CAPACITY;
    }
    wago_put32(response + 0, WAGO_PICO2_TRANSPORT_MAGIC);
    wago_put16(response + 4, WAGO_PICO2_TRANSPORT_VERSION);
    wago_put16(response + 6, (uint16_t)(request_kind | WAGO_PICO2_TRANSPORT_RESPONSE_MASK));
    wago_put32(response + 8, sequence);
    wago_put32(response + 12, payload_length);
    wago_put32(response + 16, code);
    if (payload_length != 0) {
        wago_copy(response + WAGO_PICO2_TRANSPORT_HEADER_BYTES, payload, payload_length);
    }
    wago_put32(response + 20,
               wago_pico2_crc32(response, 20,
                                response + WAGO_PICO2_TRANSPORT_HEADER_BYTES,
                                payload_length));
    *response_length = total;
    return WAGO_PICO2_DISPATCH_OK;
}

int wago_pico2_dispatch(struct wago_pico2_endpoint *endpoint,
                        const uint8_t *request, uint32_t request_length,
                        uint8_t *response, uint32_t response_capacity,
                        uint32_t *response_length) {
    uint16_t kind;
    uint32_t sequence;
    uint32_t payload_length;
    const uint8_t *payload;
    const uint8_t *response_payload = NULL;
    uint32_t response_payload_length = 0;
    wago_pico2_code code = WAGO_PICO2_OK;
    if (endpoint == NULL || endpoint->runner == NULL || request == NULL ||
        response == NULL || response_length == NULL ||
        (endpoint->parameter_capacity != 0 && endpoint->parameter_slots == NULL) ||
        (endpoint->result_capacity != 0 && endpoint->result_slots == NULL) ||
        (endpoint->payload_capacity != 0 && endpoint->payload_scratch == NULL) ||
        endpoint->maximum_payload > UINT32_MAX - WAGO_PICO2_TRANSPORT_HEADER_BYTES ||
        request_length < WAGO_PICO2_TRANSPORT_HEADER_BYTES) {
        return WAGO_PICO2_DISPATCH_FRAME;
    }
    if (response_capacity < WAGO_PICO2_TRANSPORT_HEADER_BYTES) {
        return WAGO_PICO2_DISPATCH_CAPACITY;
    }
    if (wago_get32(request + 0) != WAGO_PICO2_TRANSPORT_MAGIC ||
        wago_get16(request + 4) != WAGO_PICO2_TRANSPORT_VERSION) {
        return WAGO_PICO2_DISPATCH_FRAME;
    }
    kind = wago_get16(request + 6);
    if (!wago_kind_valid(kind) || (kind & WAGO_PICO2_TRANSPORT_RESPONSE_MASK) != 0 ||
        wago_get32(request + 16) != WAGO_PICO2_OK) {
        return WAGO_PICO2_DISPATCH_FRAME;
    }
    sequence = wago_get32(request + 8);
    payload_length = wago_get32(request + 12);
    if (payload_length > endpoint->maximum_payload ||
        payload_length > UINT32_MAX - WAGO_PICO2_TRANSPORT_HEADER_BYTES ||
        WAGO_PICO2_TRANSPORT_HEADER_BYTES + payload_length != request_length) {
        return payload_length > endpoint->maximum_payload ?
               WAGO_PICO2_DISPATCH_CAPACITY : WAGO_PICO2_DISPATCH_FRAME;
    }
    payload = request + WAGO_PICO2_TRANSPORT_HEADER_BYTES;
    if (wago_pico2_crc32(request, 20, payload, payload_length) != wago_get32(request + 20)) {
        return WAGO_PICO2_DISPATCH_CHECKSUM;
    }

    switch (kind) {
    case WAGO_PICO2_HELLO: {
        uint32_t maximum_payload;
        if (payload_length != 0) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        if (endpoint->payload_capacity < WAGO_PICO2_TRANSPORT_HELLO_BYTES ||
            response_capacity < WAGO_PICO2_TRANSPORT_HEADER_BYTES + WAGO_PICO2_TRANSPORT_HELLO_BYTES) {
            return WAGO_PICO2_DISPATCH_CAPACITY;
        }
        maximum_payload = endpoint->runner->maximum_payload;
        if (maximum_payload > endpoint->maximum_payload) {
            maximum_payload = endpoint->maximum_payload;
        }
        wago_put32(endpoint->payload_scratch + 0, endpoint->runner->target);
        wago_put32(endpoint->payload_scratch + 4, WAGO_PICO2_CONTEXT_ABI_BYTES);
        wago_put32(endpoint->payload_scratch + 8, WAGO_PICO2_CALL_ABI_BYTES);
        wago_put32(endpoint->payload_scratch + 12, maximum_payload);
        response_payload = endpoint->payload_scratch;
        response_payload_length = WAGO_PICO2_TRANSPORT_HELLO_BYTES;
        break;
    }
    case WAGO_PICO2_INSTANTIATE:
        if (payload_length != 0) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        code = wago_pico2_runner_instantiate(endpoint->runner);
        break;
    case WAGO_PICO2_START:
        if (payload_length != 0) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        code = wago_pico2_runner_start(endpoint->runner);
        break;
    case WAGO_PICO2_CALL: {
        uint32_t export_ordinal;
        uint32_t parameter_slots;
        uint32_t result_slots;
        uint64_t expected;
        uint32_t i;
        if (payload_length < WAGO_PICO2_TRANSPORT_CALL_HEADER_BYTES) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        export_ordinal = wago_get32(payload + 0);
        parameter_slots = wago_get32(payload + 4);
        result_slots = wago_get32(payload + 8);
        expected = (uint64_t)WAGO_PICO2_TRANSPORT_CALL_HEADER_BYTES + (uint64_t)parameter_slots * 4u;
        if (expected != payload_length || parameter_slots > endpoint->parameter_capacity ||
            result_slots > endpoint->result_capacity) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        if ((uint64_t)result_slots * 4u > endpoint->payload_capacity ||
            (uint64_t)WAGO_PICO2_TRANSPORT_HEADER_BYTES + (uint64_t)result_slots * 4u > response_capacity) {
            return WAGO_PICO2_DISPATCH_CAPACITY;
        }
        for (i = 0; i < parameter_slots; ++i) {
            endpoint->parameter_slots[i] = wago_get32(payload + WAGO_PICO2_TRANSPORT_CALL_HEADER_BYTES + i * 4u);
        }
        for (i = 0; i < result_slots; ++i) {
            endpoint->result_slots[i] = 0;
        }
        code = wago_pico2_runner_call(endpoint->runner, export_ordinal,
                                      endpoint->parameter_slots, parameter_slots,
                                      endpoint->result_slots, result_slots);
        if (code == WAGO_PICO2_OK) {
            for (i = 0; i < result_slots; ++i) {
                wago_put32(endpoint->payload_scratch + i * 4u, endpoint->result_slots[i]);
            }
            response_payload = endpoint->payload_scratch;
            response_payload_length = result_slots * 4u;
        }
        break;
    }
    case WAGO_PICO2_CANCEL:
        if (payload_length != 0) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        code = wago_pico2_runner_cancel(endpoint->runner);
        break;
    case WAGO_PICO2_RESET:
        if (payload_length != 0) {
            return WAGO_PICO2_DISPATCH_FRAME;
        }
        code = wago_pico2_runner_reset(endpoint->runner);
        break;
    default:
        return WAGO_PICO2_DISPATCH_FRAME;
    }

    return wago_encode_response(response, response_capacity, kind, sequence,
                                code, response_payload, response_payload_length,
                                response_length);
}

static int wago_io_read_exact(const struct wago_pico2_io *io, uint8_t *destination, uint32_t length) {
    uint32_t done = 0;
    while (done < length) {
        int count = io->read(io->user, destination + done, length - done);
        if (count <= 0 || (uint32_t)count > length - done) {
            return WAGO_PICO2_DISPATCH_IO;
        }
        done += (uint32_t)count;
    }
    return WAGO_PICO2_DISPATCH_OK;
}

static int wago_io_write_exact(const struct wago_pico2_io *io, const uint8_t *source, uint32_t length) {
    uint32_t done = 0;
    while (done < length) {
        int count = io->write(io->user, source + done, length - done);
        if (count <= 0 || (uint32_t)count > length - done) {
            return WAGO_PICO2_DISPATCH_IO;
        }
        done += (uint32_t)count;
    }
    return WAGO_PICO2_DISPATCH_OK;
}

int wago_pico2_serve_once(struct wago_pico2_endpoint *endpoint,
                          const struct wago_pico2_io *io,
                          uint8_t *request, uint32_t request_capacity,
                          uint8_t *response, uint32_t response_capacity) {
    uint32_t payload_length;
    uint32_t request_length;
    uint32_t response_length;
    int result;
    if (endpoint == NULL || io == NULL || io->read == NULL || io->write == NULL ||
        request == NULL || response == NULL || request_capacity < WAGO_PICO2_TRANSPORT_HEADER_BYTES) {
        return WAGO_PICO2_DISPATCH_FRAME;
    }
    result = wago_io_read_exact(io, request, WAGO_PICO2_TRANSPORT_HEADER_BYTES);
    if (result != WAGO_PICO2_DISPATCH_OK) {
        return result;
    }
    payload_length = wago_get32(request + 12);
    if (payload_length > endpoint->maximum_payload ||
        payload_length > request_capacity - WAGO_PICO2_TRANSPORT_HEADER_BYTES) {
        return WAGO_PICO2_DISPATCH_CAPACITY;
    }
    request_length = WAGO_PICO2_TRANSPORT_HEADER_BYTES + payload_length;
    result = wago_io_read_exact(io, request + WAGO_PICO2_TRANSPORT_HEADER_BYTES, payload_length);
    if (result != WAGO_PICO2_DISPATCH_OK) {
        return result;
    }
    result = wago_pico2_dispatch(endpoint, request, request_length,
                                 response, response_capacity, &response_length);
    if (result != WAGO_PICO2_DISPATCH_OK) {
        return result;
    }
    return wago_io_write_exact(io, response, response_length);
}

static int wago_image_range(const struct wago_pico2_runner *runner,
                            uint32_t address, uint32_t length, uint32_t *offset) {
    return wago_address_range(runner->image_address, runner->image_size,
                              address, length, offset);
}

static wago_pico2_code wago_direct_restore(struct wago_pico2_runner *runner) {
    uint32_t i;
    uint32_t count;
    if (runner->image == NULL || runner->initial_image == NULL || runner->image_size == 0 ||
        runner->image_address == 0 || runner->helpers.f64 == 0 || runner->helpers.simd == 0 ||
        runner->helpers.i64 == 0 || runner->helpers.f32 == 0) {
        return WAGO_PICO2_STATE;
    }
#if UINTPTR_MAX == UINT32_MAX
    if ((uint32_t)(uintptr_t)runner->image != runner->image_address) {
        return WAGO_PICO2_STATE;
    }
#endif
    count = runner->context_count != 0 ? runner->context_count : 1;
    for (i = 0; i < count; ++i) {
        uint32_t context = runner->context_count != 0 ? runner->contexts[i] : runner->context_address;
        uint32_t context_offset;
        uint32_t helper_address;
        uint32_t helper_offset;
        if (!wago_image_range(runner, context, WAGO_PICO2_CONTEXT_ABI_BYTES,
                              &context_offset)) {
            return WAGO_PICO2_STATE;
        }
        helper_address = wago_get32(runner->initial_image + context_offset +
                                    WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET);
        if (!wago_image_range(runner, helper_address, WAGO_PICO2_HELPER_TABLE_BYTES,
                              &helper_offset)) {
            return WAGO_PICO2_STATE;
        }
    }
    wago_copy(runner->image, runner->initial_image, runner->image_size);
    for (i = 0; i < count; ++i) {
        uint32_t context = runner->context_count != 0 ? runner->contexts[i] : runner->context_address;
        uint32_t context_offset;
        uint32_t helper_address;
        uint32_t helper_offset;
        (void)wago_image_range(runner, context, WAGO_PICO2_CONTEXT_ABI_BYTES,
                               &context_offset);
        helper_address = wago_get32(runner->image + context_offset +
                                    WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET);
        (void)wago_image_range(runner, helper_address, WAGO_PICO2_HELPER_TABLE_BYTES,
                               &helper_offset);
        wago_put32(runner->image + helper_offset + WAGO_PICO2_HELPER_F64_OFFSET,
                   runner->helpers.f64);
        wago_put32(runner->image + helper_offset + WAGO_PICO2_HELPER_SIMD_OFFSET,
                   runner->helpers.simd);
        wago_put32(runner->image + helper_offset + WAGO_PICO2_HELPER_I64_OFFSET,
                   runner->helpers.i64);
        wago_put32(runner->image + helper_offset + WAGO_PICO2_HELPER_F32_OFFSET,
                   runner->helpers.f32);
    }
#if defined(__GNUC__) || defined(__clang__)
    __builtin___clear_cache((char *)runner->image, (char *)runner->image + runner->image_size);
#endif
    return WAGO_PICO2_OK;
}

static wago_pico2_code wago_direct_instantiate(void *user, struct wago_pico2_runner *runner) {
    (void)user;
    return wago_direct_restore(runner);
}

static wago_pico2_code wago_direct_start(void *user, uint32_t entry, uint32_t context) {
    (void)user;
#if UINTPTR_MAX == UINT32_MAX
    typedef uint32_t (*start_function)(uint32_t);
    uint32_t trap_cell = *(uint32_t *)(uintptr_t)(context + WAGO_PICO2_CONTEXT_TRAP_CELL_OFFSET);
    *(uint32_t *)(uintptr_t)trap_cell = 0;
    return ((start_function)(uintptr_t)entry)(context);
#else
    (void)entry;
    (void)context;
    return WAGO_PICO2_UNSUPPORTED;
#endif
}

static wago_pico2_code wago_direct_call(void *user, uint32_t entry, uint32_t context,
                                        const uint32_t *parameters, uint32_t parameter_slots,
                                        uint32_t *results, uint32_t result_slots) {
    (void)user;
    (void)parameter_slots;
    (void)result_slots;
#if UINTPTR_MAX == UINT32_MAX
    typedef uint32_t (*call_function)(const struct wago_pico2_call_abi *);
    struct wago_pico2_call_abi call;
    uint32_t trap_cell = *(uint32_t *)(uintptr_t)(context + WAGO_PICO2_CONTEXT_TRAP_CELL_OFFSET);
    *(uint32_t *)(uintptr_t)trap_cell = 0;
    call.context = context;
    call.parameters = (uint32_t)(uintptr_t)parameters;
    call.results = (uint32_t)(uintptr_t)results;
    return ((call_function)(uintptr_t)entry)(&call);
#else
    (void)entry;
    (void)context;
    (void)parameters;
    (void)results;
    return WAGO_PICO2_UNSUPPORTED;
#endif
}

static wago_pico2_code wago_direct_cancel(void *user, uint32_t context) {
    (void)user;
#if UINTPTR_MAX == UINT32_MAX
    uint32_t *cell = (uint32_t *)(uintptr_t)(context + WAGO_PICO2_CONTEXT_CANCEL_CELL_OFFSET);
    *cell = 1;
    return WAGO_PICO2_OK;
#else
    (void)context;
    return WAGO_PICO2_UNSUPPORTED;
#endif
}

static wago_pico2_code wago_direct_reset(void *user, struct wago_pico2_runner *runner) {
    (void)user;
    return wago_direct_restore(runner);
}

const struct wago_pico2_invoker wago_pico2_direct_invoker = {
    NULL,
    wago_direct_instantiate,
    wago_direct_start,
    wago_direct_call,
    wago_direct_cancel,
    wago_direct_reset,
};
