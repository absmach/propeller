cmake_minimum_required(VERSION 3.20)

#
# Force WAMR to treat this build as Zephyr + Xtensa
# (Adjust target name as needed: "XTENSA", "esp32", etc.)
#
set(WAMR_BUILD_PLATFORM "zephyr" CACHE STRING "" FORCE)
set(WAMR_BUILD_TARGET "XTENSA" CACHE STRING "" FORCE)

#
# Minimal build settings
#
set(WAMR_BUILD_INTERP 1 CACHE BOOL "Enable WAMR interpreter" FORCE)
set(WAMR_BUILD_AOT 0 CACHE BOOL "Disable AOT" FORCE)
set(WAMR_BUILD_JIT 0 CACHE BOOL "Disable JIT" FORCE)
set(WAMR_BUILD_FAST_INTERP 1 CACHE BOOL "Enable fast interpreter" FORCE)
set(WAMR_BUILD_LIBC_BUILTIN 1 CACHE BOOL "Enable builtin libc" FORCE)
set(WAMR_BUILD_LIBC_WASI 1 CACHE BOOL "Enable WASI libc" FORCE)
# Disable pthread library in WAMR
set(WAMR_BUILD_LIB_PTHREAD 0 CACHE BOOL "" FORCE)


#
# Optional features you can uncomment if desired:
#
#set(WAMR_BUILD_MULTI_MODULE 1 CACHE BOOL "Enable multiple modules" FORCE)
#set(WAMR_BUILD_LIB_PTHREAD 1 CACHE BOOL "Enable pthread library" FORCE)
#set(WAMR_BUILD_LIB_WASI_THREADS 1 CACHE BOOL "Enable WASI threads library" FORCE)

#
# Add the official WAMR subdirectory
#
add_subdirectory(wasm-micro-runtime
    ${CMAKE_CURRENT_BINARY_DIR}/wamr_build
)

#
# Provide a function for linking WAMR to the Zephyr 'app' target.
# WAMR typically builds a static library named 'vmlib', 
# output from 'iwasm_static' with OUTPUT_NAME vmlib.
#
function(link_wamr_to_zephyr TARGET)
    target_link_libraries(${TARGET} PRIVATE vmlib)
endfunction()
