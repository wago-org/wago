add_library(wago_pico2_transport STATIC
    wago_pico2.c
    wago_pico2_pico_sdk.c)

target_include_directories(wago_pico2_transport PUBLIC
    ${CMAKE_CURRENT_LIST_DIR})

target_compile_features(wago_pico2_transport PUBLIC c_std_11)
target_compile_definitions(wago_pico2_transport PUBLIC WAGO_PICO2_PICO_SDK=1)
target_link_libraries(wago_pico2_transport PUBLIC pico_stdlib)

# Attach C and GNU ld files produced by RenderPico2FirmwareC and
# RenderPico2FirmwareLinkerScript. The linker fragment reserves the mutable
# generated-code image at the exact SRAM base used during compilation.
function(wago_pico2_add_generated_image target generated_c generated_linker_script)
    target_sources(${target} PRIVATE ${generated_c})
    target_link_options(${target} PRIVATE "-Wl,-T,${generated_linker_script}")
    set_property(TARGET ${target} APPEND PROPERTY LINK_DEPENDS ${generated_linker_script})
endfunction()

# Build the exact Go semantic helpers as a relocatable TinyGo object. Use
# tinygo_target=pico2 for Cortex-M33 and tinygo_target=riscv-qemu for the
# freestanding RV32IM helper object used by Hazard3 builds. All TinyGo runtime
# symbols except the four helper entries are localized, avoiding collisions with
# the Pico SDK's reset/IRQ/runtime boundary; --gc-sections removes unreachable
# localized startup code at final link.
function(wago_pico2_add_tinygo_helpers target tinygo_target)
    find_program(WAGO_PICO2_TINYGO tinygo REQUIRED)
    find_program(WAGO_PICO2_OBJCOPY NAMES llvm-objcopy REQUIRED)
    file(GLOB_RECURSE WAGO_PICO2_HELPER_GO_SOURCES CONFIGURE_DEPENDS
        ${CMAKE_CURRENT_LIST_DIR}/../../src/core/runtime/embedded32/*.go
        ${CMAKE_CURRENT_LIST_DIR}/helpers/*.go)
    set(raw_object ${CMAKE_CURRENT_BINARY_DIR}/${target}-${tinygo_target}-helpers-raw.o)
    set(helper_object ${CMAKE_CURRENT_BINARY_DIR}/${target}-${tinygo_target}-helpers.o)
    add_custom_command(
        OUTPUT ${helper_object}
        COMMAND ${WAGO_PICO2_TINYGO} build
            -target=${tinygo_target} -scheduler=none -panic=trap
            -o ${raw_object} ./firmware/pico2/helpers
        COMMAND ${WAGO_PICO2_OBJCOPY}
            --keep-global-symbol=wago_embedded32_f64
            --keep-global-symbol=wago_embedded32_simd_abi
            --keep-global-symbol=wago_embedded32_i64
            --keep-global-symbol=wago_embedded32_f32
            ${raw_object} ${helper_object}
        DEPENDS ${WAGO_PICO2_HELPER_GO_SOURCES}
        WORKING_DIRECTORY ${CMAKE_CURRENT_LIST_DIR}/../..
        VERBATIM)
    set_source_files_properties(${helper_object} PROPERTIES
        GENERATED TRUE EXTERNAL_OBJECT TRUE)
    target_sources(${target} PRIVATE ${helper_object})
    target_link_options(${target} PRIVATE -Wl,--gc-sections)
endfunction()
