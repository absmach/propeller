cmake_minimum_required(VERSION 3.16)

# Point to the WAMR ESP-IDF component from the existing submodule
set(EXTRA_COMPONENT_DIRS
    "${CMAKE_CURRENT_SOURCE_DIR}/../../embed-proplet/modules/wamr/wasm-micro-runtime/build-scripts/esp-idf"
)

include($ENV{IDF_PATH}/tools/cmake/project.cmake)
project(wasm_stress_bench)
