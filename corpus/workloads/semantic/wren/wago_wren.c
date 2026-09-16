#include <stdint.h>
#include <string.h>
#include <time.h>
#include "wren.h"

clock_t wago_clock(void) { return 0; }

static int had_error;
static void write_fn(WrenVM *vm, const char *text) { (void)vm; (void)text; }
static void error_fn(WrenVM *vm, WrenErrorType type, const char *module, int line, const char *message) {
    (void)vm; (void)module; (void)message; had_error = 100000 + (int)type * 10000 + line;
}

uint64_t wren_run(void) {
    static const char source[] =
        "class Box {\n"
        "  construct new(value) { _value = value }\n"
        "  bump(amount) { _value = _value + amount }\n"
        "  value { _value }\n"
        "}\n"
        "class Bench {\n"
        "  static run {\n"
        "    var boxes = []\n"
        "    for (i in 0...400) { boxes.add(Box.new(i * 3 + 1)) }\n"
        "    var total = 0\n"
        "    for (box in boxes) {\n"
        "      box.bump(7)\n"
        "      total = total + box.value\n"
        "    }\n"
        "    var transform = Fn.new { |x| x * x + 3 }\n"
        "    for (i in 1..80) { total = total + transform.call(i) }\n"
        "    return total + boxes.count * 1000000\n"
        "  }\n"
        "}\n";
    WrenConfiguration config; wrenInitConfiguration(&config);
    config.writeFn = write_fn; config.errorFn = error_fn; had_error = 0;
    WrenVM *vm = wrenNewVM(&config);
    if (!vm) return 1;
    if (wrenInterpret(vm, "main", source) != WREN_RESULT_SUCCESS || had_error) {
        uint64_t error = (uint64_t)had_error; wrenFreeVM(vm); return error;
    }
    wrenEnsureSlots(vm, 1); wrenGetVariable(vm, "main", "Bench", 0);
    WrenHandle *call = wrenMakeCallHandle(vm, "run");
    if (!call || wrenCall(vm, call) != WREN_RESULT_SUCCESS || had_error) { if (call) wrenReleaseHandle(vm, call); wrenFreeVM(vm); return 3; }
    double value = wrenGetSlotDouble(vm, 0);
    wrenReleaseHandle(vm, call); wrenFreeVM(vm);
    return (uint64_t)value;
}
