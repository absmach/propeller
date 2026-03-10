/*
 * Message-Passing Stress Workload – ring buffer producer/consumer
 *
 * Simulates a producer-consumer message queue inside a single WASM
 * instance using a statically allocated ring buffer.
 * Models workloads that shuttle messages through a bounded queue
 * (e.g., sensor event pipelines).
 *
 * Compile with wasi-sdk:
 *   clang --target=wasm32-wasi -O2 -nostdlib \
 *         -Wl,--no-entry -Wl,--export=main \
 *         -o msg_stress.wasm msg_stress.c
 */

#define QUEUE_SIZE  256
#define ITERATIONS  10000

/* Simple LCG PRNG for producing values */
static unsigned int lcg_next(unsigned int *state)
{
    *state = *state * 1103515245u + 12345u;
    return *state;
}

__attribute__((export_name("main")))
int main(int argc, char **argv)
{
    (void)argc; (void)argv;

    static volatile unsigned int queue[QUEUE_SIZE];
    int head = 0, tail = 0;
    unsigned int rng = 0xDEADBEEF;
    unsigned int consumed_sum = 0;

    for (int i = 0; i < ITERATIONS; i++) {
        /* Produce: enqueue if space available */
        int used = tail - head;
        if (used < QUEUE_SIZE) {
            queue[tail & (QUEUE_SIZE - 1)] = lcg_next(&rng);
            tail++;
        }

        /* Consume half the queue when it's at least half full */
        if ((tail - head) >= QUEUE_SIZE / 2) {
            int drain = QUEUE_SIZE / 4;
            while (drain-- > 0 && head < tail) {
                consumed_sum += queue[head & (QUEUE_SIZE - 1)];
                head++;
            }
        }
    }

    /* Drain remainder */
    while (head < tail) {
        consumed_sum += queue[head & (QUEUE_SIZE - 1)];
        head++;
    }

    return (int)(consumed_sum & 0xFF);
}
