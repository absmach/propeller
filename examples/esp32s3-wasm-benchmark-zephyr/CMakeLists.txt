cmake_minimum_required(VERSION 3.20.0)

set(BOARD esp32s3_devkitc)

find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})
project(esp32s3_wasm_benchmark_zephyr)

set(WAMR_BUILD_PLATFORM "zephyr")
set(WAMR_BUILD_TARGET "XTENSA")

if (NOT DEFINED WAMR_BUILD_INTERP)
  set(WAMR_BUILD_INTERP 1)
endif ()

if (NOT DEFINED WAMR_BUILD_AOT)
  set(WAMR_BUILD_AOT 1)
endif ()

if (NOT DEFINED WAMR_BUILD_LIBC_BUILTIN)
  set(WAMR_BUILD_LIBC_BUILTIN 1)
endif ()

if (NOT DEFINED WAMR_BUILD_LIBC_WASI)
  set(WAMR_BUILD_LIBC_WASI 0)
endif ()

# Use system allocator (malloc/free) so all allocations show in heap stats.
set(WAMR_BUILD_GLOBAL_HEAP_POOL 0)

set(WAMR_ROOT_DIR ${CMAKE_CURRENT_SOURCE_DIR}/../../embed-proplet/modules/wamr/wasm-micro-runtime)
include(${WAMR_ROOT_DIR}/build-scripts/runtime_lib.cmake)

list(APPEND ZEPHYR_EXTRA_MODULES ${WAMR_ROOT_DIR}/product-mini/platforms/zephyr)

include_directories(${ZEPHYR_BASE}/include/zephyr)

target_include_directories(app PRIVATE
    ${WAMR_ROOT_DIR}/core/iwasm/include
    src
)

zephyr_library_named(wamr_lib)
zephyr_library_sources(${WAMR_RUNTIME_LIB_SOURCE})
zephyr_library_app_memory(wamr_partition)

target_sources(app PRIVATE
    src/main.c
    src/bench.c
    src/metrics.c
)

target_link_libraries(app PRIVATE wamr_lib)
