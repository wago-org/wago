add_library(wago_pico2_transport STATIC
    wago_pico2.c
    wago_pico2_pico_sdk.c)

target_include_directories(wago_pico2_transport PUBLIC
    ${CMAKE_CURRENT_LIST_DIR})

target_compile_features(wago_pico2_transport PUBLIC c_std_11)
target_compile_definitions(wago_pico2_transport PUBLIC WAGO_PICO2_PICO_SDK=1)
target_link_libraries(wago_pico2_transport PUBLIC pico_stdlib)
