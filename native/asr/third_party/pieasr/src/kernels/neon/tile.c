#include <arm_neon.h>
#include <string.h>
#include "kernels/kernels.h"

static inline void mac16(int32x4_t a[4], int8x16_t w, int8x8_t x)
{
    const int16x8_t lo = vmull_s8(vget_low_s8(w), x);
    const int16x8_t hi = vmull_s8(vget_high_s8(w), x);
    a[0] = vaddw_s16(a[0], vget_low_s16(lo));
    a[1] = vaddw_s16(a[1], vget_high_s16(lo));
    a[2] = vaddw_s16(a[2], vget_low_s16(hi));
    a[3] = vaddw_s16(a[3], vget_high_s16(hi));
}

static inline void sums2(const int8_t *w, const int8_t *x0, const int8_t *x1, int G, int32_t start, int32x4_t a[4],
                         int32x4_t b[4])
{
    for (int i = 0; i < 4; i++) {
        a[i] = vdupq_n_s32(start);
        b[i] = vdupq_n_s32(start);
    }
    for (int k = 0; k < G; k++) {
        const int8x16_t wk = vld1q_s8(w + k * 16);
        mac16(a, wk, vdup_n_s8(x0[k]));
        mac16(b, wk, vdup_n_s8(x1[k]));
    }
}

static inline void sums1(const int8_t *w, const int8_t *x0, int G, int32_t start, int32x4_t a[4])
{
    for (int i = 0; i < 4; i++) {
        a[i] = vdupq_n_s32(start);
    }
    for (int k = 0; k < G; k++) {
        mac16(a, vld1q_s8(w + k * 16), vdup_n_s8(x0[k]));
    }
}

static inline void store(int32_t *acc, const int32x4_t a[4])
{
    for (int i = 0; i < 4; i++) {
        vst1q_s32(acc + i * 4, a[i]);
    }
}

void pq_tile16(const int8_t *w, const int8_t *x, int xstride, int T, int G, int32_t *acc)
{
    int32x4_t a[4], b[4];
    int t = 0;
    for (; t + 1 < T; t += 2) {
        sums2(w, x + (size_t)t * xstride, x + (size_t)(t + 1) * xstride, G, 0, a, b);
        store(acc + t * 16, a);
        store(acc + (t + 1) * 16, b);
    }
    if (t < T) {
        sums1(w, x + (size_t)t * xstride, G, 0, a);
        store(acc + t * 16, a);
    }
}

static inline void finish(int32_t *y, const int32x4_t a[4], const int32x4_t n[4], uint8_t sar)
{
    const int32x4_t shift = vdupq_n_s32(-(int32_t)sar);
    for (int i = 0; i < 4; i++) {
        const int32x4_t p = vshlq_s32(vmulq_s32(vshrq_n_s32(a[i], 6), n[i]), shift);
        vst1q_s32(y + i * 4, vqaddq_s32(vld1q_s32(y + i * 4), p));
    }
}

void pq_tile16_n(const int8_t *w, const int8_t *x, int xstride, int T, int G, const int16_t *n16, const uint8_t *sar,
                 int32_t *y32)
{
    const int16x8_t n_lo = vld1q_s16(n16), n_hi = vld1q_s16(n16 + 8);
    const int32x4_t n[4] = { vmovl_s16(vget_low_s16(n_lo)), vmovl_s16(vget_high_s16(n_lo)),
                             vmovl_s16(vget_low_s16(n_hi)), vmovl_s16(vget_high_s16(n_hi)) };
    int32x4_t a[4], b[4];
    int t = 0;
    for (; t + 1 < T; t += 2) {
        sums2(w, x + (size_t)t * xstride, x + (size_t)(t + 1) * xstride, G, 32, a, b);
        finish(y32 + t * 16, a, n, sar[t]);
        finish(y32 + (t + 1) * 16, b, n, sar[t + 1]);
    }
    if (t < T) {
        sums1(w, x + (size_t)t * xstride, G, 32, a);
        finish(y32 + t * 16, a, n, sar[t]);
    }
}
