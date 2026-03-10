idf_component_register(
    SRCS
        "main.c"
        "bench.c"
        "metrics.c"
    INCLUDE_DIRS
        "."
    REQUIRES
        wamr
        esp_timer
        freertos
        heap
        esp_system
        pthread
)

# WAMR needs _POSIX_THREADS
target_compile_definitions(${COMPONENT_LIB} PRIVATE -D_REENT_SMALL=1)

