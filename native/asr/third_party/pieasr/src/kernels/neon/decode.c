#include <arm_neon.h>
#include <string.h>
#include "kernels/kernels.h"

static inline int8x8x2_t table16(const uint32_t *lut)
{
    int8_t t[16];
    for (int j = 0; j < 16; j++) {
        t[j] = (int8_t)(lut[j] & 0xff);
    }
    const int8x16_t v = vld1q_s8(t);
    return (int8x8x2_t){ { vget_low_s8(v), vget_high_s8(v) } };
}

static inline int8x16_t look16(int8x8x2_t t, uint8x16_t idx)
{
    return vcombine_s8(vtbl2_s8(t, vreinterpret_s8_u8(vget_low_u8(idx))),
                       vtbl2_s8(t, vreinterpret_s8_u8(vget_high_u8(idx))));
}

static inline int8x16_t look4(int8x8_t t, uint8x16_t idx)
{
    return vcombine_s8(vtbl1_s8(t, vreinterpret_s8_u8(vget_low_u8(idx))),
                       vtbl1_s8(t, vreinterpret_s8_u8(vget_high_u8(idx))));
}

void pq_decode4(const uint8_t *codes, int nbytes, const uint32_t *lut, int8_t *out)
{
    const int8x8x2_t t = table16(lut);
    const uint8x16_t low = vdupq_n_u8(15);
    int i = 0;
    for (; i + 16 <= nbytes; i += 16) {
        const uint8x16_t c = vld1q_u8(codes + i);
        const int8x16x2_t v = { { look16(t, vandq_u8(c, low)), look16(t, vshrq_n_u8(c, 4)) } };
        vst2q_s8(out + 2 * i, v);
    }
    for (; i < nbytes; i++) {
        out[2 * i] = (int8_t)(lut[codes[i]] & 0xff);
        out[2 * i + 1] = (int8_t)((lut[codes[i]] >> 8) & 0xff);
    }
}

void pq_decode2(const uint8_t *codes, int nbytes, const uint32_t *lut, int8_t *out)
{
    int8_t t4[8] = { 0 };
    for (int j = 0; j < 4; j++) {
        t4[j] = (int8_t)(lut[j] & 0xff);
    }
    const int8x8_t t = vld1_s8(t4);
    const uint8x16_t three = vdupq_n_u8(3);
    int i = 0;
    for (; i + 16 <= nbytes; i += 16) {
        const uint8x16_t c = vld1q_u8(codes + i);
        const int8x16x4_t v = { { look4(t, vandq_u8(c, three)), look4(t, vandq_u8(vshrq_n_u8(c, 2), three)),
                                  look4(t, vandq_u8(vshrq_n_u8(c, 4), three)), look4(t, vshrq_n_u8(c, 6)) } };
        vst4q_s8(out + 4 * i, v);
    }
    for (; i < nbytes; i++) {
        memcpy(out + 4 * i, &lut[codes[i]], 4);
    }
}

void pq_decode2_il(const uint8_t *codes, int nbytes, const uint32_t *lut, int8_t *out)
{
    pq_decode2(codes, nbytes, lut, out);
}
