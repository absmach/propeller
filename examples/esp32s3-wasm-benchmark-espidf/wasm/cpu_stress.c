/*
 * CPU Stress Workload – FNV-1a hash loop
 *
 * Performs 1,000,000 iterations of the FNV-1a 32-bit hash function.
 * This is purely register/ALU bound – no memory allocation, no I/O.
 *
 * Compile with wasi-sdk:
 *   clang --target=wasm32-wasi -O2 -nostdlib \
 *         -Wl,--no-entry -Wl,--export=main \
 *         -o cpu_stress.wasm cpu_stress.c
 *
 * Or via the provided build.sh script.
 */

#define ITERATIONS 1000000
#define FNV_OFFSET 2166136261u
#define FNV_PRIME  16777619u

__attribute__((export_name("main")))
int main(int argc, char **argv)
{
    (void)argc; (void)argv;

    volatile unsigned int h = FNV_OFFSET;
    for (unsigned int i = 0; i < ITERATIONS; i++) {
        h ^= i;
        h *= FNV_PRIME;
    }
    /* Return low byte of hash to prevent dead-code elimination */
    return (int)(h & 0xFF);
}
