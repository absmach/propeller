/*
 * Memory Stress Workload – linear memory write/read
 *
 * Repeatedly writes and reads a 4KB scratchpad in WASM linear memory.
 * Tests WASM memory bandwidth and allocation pressure.
 * No malloc – uses static array to stress linear memory pages.
 *
 * Compile with wasi-sdk:
 *   clang --target=wasm32-wasi -O1 -nostdlib \
 *         -Wl,--no-entry -Wl,--export=main \
 *         -o mem_stress.wasm mem_stress.c
 */

#define ROUNDS     1000
#define CHUNK_SIZE 4096

static volatile unsigned char scratchpad[CHUNK_SIZE];

__attribute__((export_name("main")))
int main(int argc, char **argv)
{
    (void)argc; (void)argv;

    unsigned int checksum = 0;

    for (int r = 0; r < ROUNDS; r++) {
        /* Write phase: fill with a pattern derived from round number */
        for (int i = 0; i < CHUNK_SIZE; i++) {
            scratchpad[i] = (unsigned char)((r * 31 + i * 17) & 0xFF);
        }

        /* Read phase: accumulate checksum to prevent dead-code elimination */
        for (int i = 0; i < CHUNK_SIZE; i++) {
            checksum += scratchpad[i];
        }
    }

    return (int)(checksum & 0xFF);
}
