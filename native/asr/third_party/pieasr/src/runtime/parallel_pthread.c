#include <pthread.h>
#include <stdatomic.h>
#include "runtime/parallel.h"

#define SPINS 200000

int pq_par_single;
uint32_t pq_par_calls, pq_par_wait_cycles;

static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t wake = PTHREAD_COND_INITIALIZER;
static pq_par_fn job_fn;
static void *job_ctx;
static int parts = PQ_PAR_PARTS;
static atomic_uint generation;
static atomic_int pending;
static atomic_int sleepers;
static atomic_int started;

static inline void relax(void)
{
#if defined(__arm__) || defined(__aarch64__)
    __asm__ volatile("yield");
#endif
}

static void *worker(void *arg)
{
    const int part = (int)(intptr_t)arg;
    unsigned seen = 0;
    for (;;) {
        unsigned g;
        int spins = 0;
        while ((g = atomic_load_explicit(&generation, memory_order_acquire)) == seen) {
            if (++spins < SPINS) {
                relax();
                continue;
            }
            pthread_mutex_lock(&mu);
            atomic_fetch_add(&sleepers, 1);
            while (atomic_load(&generation) == seen) {
                pthread_cond_wait(&wake, &mu);
            }
            atomic_fetch_sub(&sleepers, 1);
            pthread_mutex_unlock(&mu);
            spins = 0;
        }
        seen = g;
        job_fn(job_ctx, part, parts);
        atomic_fetch_sub_explicit(&pending, 1, memory_order_release);
    }
    return NULL;
}

void pq_par_set(int n)
{
    if (!atomic_load(&started) && n >= 1 && n <= PQ_PAR_PARTS) {
        parts = n;
    }
}

void pq_par_init(void)
{
    if (atomic_exchange(&started, 1)) {
        return;
    }
    for (int p = 1; p < parts; p++) {
        pthread_t t;
        pthread_create(&t, NULL, worker, (void *)(intptr_t)p);
        pthread_detach(t);
    }
}

void pq_par_run(pq_par_fn fn, void *ctx)
{
    pq_par_init();
    pq_par_calls++;
    if (parts == 1) {
        fn(ctx, 0, 1);
        return;
    }
    job_fn = fn;
    job_ctx = ctx;
    atomic_store_explicit(&pending, parts - 1, memory_order_relaxed);
    atomic_fetch_add_explicit(&generation, 1, memory_order_release);
    if (atomic_load(&sleepers) > 0) {
        pthread_mutex_lock(&mu);
        pthread_cond_broadcast(&wake);
        pthread_mutex_unlock(&mu);
    }
    fn(ctx, 0, parts);
    while (atomic_load_explicit(&pending, memory_order_acquire) > 0) {
        relax();
    }
}
