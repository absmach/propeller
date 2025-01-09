cmake_minimum_required(VERSION 3.20.0)

set(BOARD esp32s3_devkitc)

find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})
project(proplet_mqtt_client)

include_directories(${ZEPHYR_BASE}/include/zephyr)

list(APPEND ZEPHYR_EXTRA_MODULES ${CMAKE_CURRENT_SOURCE_DIR}/modules/wamr/wasm-micro-runtime/product-mini/platforms/zephyr)

# Add WAMR include paths
target_include_directories(app PRIVATE 
    ${CMAKE_CURRENT_SOURCE_DIR}/modules/wamr/wasm-micro-runtime/core/iwasm/include
)


target_sources(app PRIVATE src/main.c src/wasm_handler.c)

