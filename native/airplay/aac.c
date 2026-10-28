#include "aac.h"

#include "third_party/alac/alac.h"

#include <android/log.h>
#include <dlfcn.h>
#include <media/NdkMediaCodec.h>
#include <media/NdkMediaFormat.h>
#include <pthread.h>
#include <string.h>

#define TAG COUNTERTOP_PREFIX "-airplay"
#define logw(...) __android_log_print(ANDROID_LOG_WARN, TAG, __VA_ARGS__)
#define logi(...) __android_log_print(ANDROID_LOG_INFO, TAG, __VA_ARGS__)

#define CT_ALAC 2
#define CT_AAC_LC 4
#define CT_AAC_ELD 8
#define RATE 44100
#define CHANNELS 2
#define ALAC_FRAME 352
#define ALAC_MOST 4096

#define NDK_CALLS(X)                                                                                         \
    X(AMediaCodec_createDecoderByType)                                                                       \
    X(AMediaCodec_configure)                                                                                 \
    X(AMediaCodec_start)                                                                                     \
    X(AMediaCodec_stop)                                                                                      \
    X(AMediaCodec_delete)                                                                                    \
    X(AMediaCodec_dequeueInputBuffer)                                                                        \
    X(AMediaCodec_getInputBuffer)                                                                            \
    X(AMediaCodec_queueInputBuffer)                                                                          \
    X(AMediaCodec_dequeueOutputBuffer)                                                                       \
    X(AMediaCodec_getOutputBuffer)                                                                           \
    X(AMediaCodec_releaseOutputBuffer)                                                                       \
    X(AMediaCodec_getOutputFormat)                                                                           \
    X(AMediaFormat_new)                                                                                      \
    X(AMediaFormat_delete)                                                                                   \
    X(AMediaFormat_setString)                                                                                \
    X(AMediaFormat_setInt32)                                                                                 \
    X(AMediaFormat_setBuffer)                                                                                \
    X(AMediaFormat_getInt32)

#define NDK_FIELD(f) __typeof__(f) *f;
static struct {
    NDK_CALLS(NDK_FIELD)
} ndk;
static int ndk_state;

static int load_ndk(void) {
    if (ndk_state) return ndk_state > 0 ? 0 : -1;
    ndk_state = -1;
    void *lib = dlopen("libmediandk.so", RTLD_NOW);
    if (!lib) {
        logw("no libmediandk, AAC is not decoded");
        return -1;
    }
#define NDK_LOAD(f)                                                                                          \
    if (!(ndk.f = (__typeof__(f) *)dlsym(lib, #f))) {                                                        \
        logw("libmediandk has no %s", #f);                                                                   \
        return -1;                                                                                           \
    }
    NDK_CALLS(NDK_LOAD)
    ndk_state = 1;
    return 0;
}

static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
static AMediaCodec *codec;
static alac_file *alac;
static int16_t alac_pcm[ALAC_FRAME * CHANNELS * 2];
static unsigned char alac_in[ALAC_MOST * 2];
static int open_ct;
static pcm_sink out;
static uint32_t out_rate = RATE, out_channels = CHANNELS;

static void drain(void) {
    for (;;) {
        AMediaCodecBufferInfo info;
        ssize_t i = ndk.AMediaCodec_dequeueOutputBuffer(codec, &info, 0);
        if (i == AMEDIACODEC_INFO_OUTPUT_FORMAT_CHANGED) {
            AMediaFormat *f = ndk.AMediaCodec_getOutputFormat(codec);
            int32_t v;
            if (ndk.AMediaFormat_getInt32(f, "sample-rate", &v)) out_rate = (uint32_t)v;
            if (ndk.AMediaFormat_getInt32(f, "channel-count", &v)) out_channels = (uint32_t)v;
            ndk.AMediaFormat_delete(f);
            logi("audio decodes to %u Hz x%u", out_rate, out_channels);
            continue;
        }
        if (i == AMEDIACODEC_INFO_OUTPUT_BUFFERS_CHANGED) continue;
        if (i < 0) return;
        size_t size;
        uint8_t *b = ndk.AMediaCodec_getOutputBuffer(codec, (size_t)i, &size);
        if (b && info.size > 0) out(out_rate, out_channels, (const int16_t *)(b + info.offset), (uint32_t)info.size / 2);
        ndk.AMediaCodec_releaseOutputBuffer(codec, (size_t)i, false);
    }
}

static void drop(void) {
    if (alac) {
        alac_free(alac);
        alac = NULL;
    }
    if (!codec) return;
    ndk.AMediaCodec_stop(codec);
    ndk.AMediaCodec_delete(codec);
    codec = NULL;
}

static int start_alac(void) {
    alac = alac_create(16, CHANNELS);
    if (!alac) return -1;
    alac->setinfo_max_samples_per_frame = ALAC_FRAME;
    alac->setinfo_7a = 0;
    alac->setinfo_sample_size = 16;
    alac->setinfo_rice_historymult = 40;
    alac->setinfo_rice_initialhistory = 10;
    alac->setinfo_rice_kmodifier = 14;
    alac->setinfo_7f = CHANNELS;
    alac->setinfo_80 = 255;
    alac->setinfo_82 = 0;
    alac->setinfo_86 = 0;
    alac->setinfo_8a_rate = RATE;
    alac_allocate_buffers(alac);
    logi("ALAC decoder open");
    return 0;
}

static int alac_fits(const uint8_t *f, int len) {
    if (len < 7 || len > ALAC_MOST) return 0;
    if ((f[0] >> 5) > 1) return 0;
    if (!((f[2] >> 4) & 1)) return 1;
    uint32_t samples = (uint32_t)(f[2] & 0x01) << 31 | (uint32_t)f[3] << 23 | (uint32_t)f[4] << 15 |
                       (uint32_t)f[5] << 7 | (uint32_t)f[6] >> 1;
    return samples > 0 && samples <= ALAC_FRAME;
}

static void decode_alac(const uint8_t *data, int len) {
    if (!alac_fits(data, len)) return;
    if (!alac && start_alac() < 0) return;
    memset(alac_in, 0, sizeof(alac_in));
    memcpy(alac_in, data, (size_t)len);
    int size = (int)sizeof(alac_pcm);
    alac_decode_frame(alac, alac_in, alac_pcm, &size);
    if (size > 0 && size <= (int)sizeof(alac_pcm)) out(RATE, CHANNELS, alac_pcm, (uint32_t)size / 2);
}

static int start(void) {
    static const uint8_t eld[] = {0xf8, 0xe8, 0x50, 0x00};
    static const uint8_t lc[] = {0x12, 0x10};
    const uint8_t *csd;
    size_t csd_len;
    int32_t profile;
    switch (open_ct) {
    case CT_AAC_ELD:
        csd = eld, csd_len = sizeof(eld), profile = 39;
        break;
    case CT_AAC_LC:
        csd = lc, csd_len = sizeof(lc), profile = 2;
        break;
    default:
        return -1;
    }
    if (load_ndk() < 0) return -1;
    AMediaCodec *c = ndk.AMediaCodec_createDecoderByType("audio/mp4a-latm");
    if (!c) {
        logw("no AAC decoder");
        return -1;
    }
    AMediaFormat *f = ndk.AMediaFormat_new();
    ndk.AMediaFormat_setString(f, "mime", "audio/mp4a-latm");
    ndk.AMediaFormat_setInt32(f, "sample-rate", RATE);
    ndk.AMediaFormat_setInt32(f, "channel-count", CHANNELS);
    ndk.AMediaFormat_setInt32(f, "aac-profile", profile);
    ndk.AMediaFormat_setInt32(f, "is-adts", 0);
    ndk.AMediaFormat_setBuffer(f, "csd-0", (void *)csd, csd_len);
    media_status_t st = ndk.AMediaCodec_configure(c, f, NULL, NULL, 0);
    ndk.AMediaFormat_delete(f);
    if (st == AMEDIA_OK) st = ndk.AMediaCodec_start(c);
    if (st != AMEDIA_OK) {
        ndk.AMediaCodec_delete(c);
        logw("AAC decoder for type %d failed: %d", open_ct, st);
        return -1;
    }
    codec = c;
    out_rate = RATE, out_channels = CHANNELS;
    logi("AAC decoder open for type %d", open_ct);
    return 0;
}

int aac_open(int ct, pcm_sink sink) {
    if (ct != CT_AAC_ELD && ct != CT_AAC_LC && ct != CT_ALAC) {
        logw("no decoder for audio type %d", ct);
        return -1;
    }
    pthread_mutex_lock(&lock);
    if (open_ct != ct) drop();
    open_ct = ct;
    out = sink;
    pthread_mutex_unlock(&lock);
    return 0;
}

void aac_decode(const uint8_t *data, int len, uint32_t rtp_time) {
    pthread_mutex_lock(&lock);
    if (len > 0 && open_ct == CT_ALAC) {
        decode_alac(data, len);
        pthread_mutex_unlock(&lock);
        return;
    }
    if (len <= 0 || (!codec && start() < 0)) {
        pthread_mutex_unlock(&lock);
        return;
    }
    ssize_t i = ndk.AMediaCodec_dequeueInputBuffer(codec, 20000);
    if (i >= 0) {
        size_t size;
        uint8_t *b = ndk.AMediaCodec_getInputBuffer(codec, (size_t)i, &size);
        size_t n = (size_t)len < size ? (size_t)len : size;
        if (b) memcpy(b, data, n);
        ndk.AMediaCodec_queueInputBuffer(codec, (size_t)i, 0, n, (uint64_t)rtp_time * 1000000u / RATE, 0);
    }
    drain();
    pthread_mutex_unlock(&lock);
}

void aac_flush(void) {
    pthread_mutex_lock(&lock);
    drop();
    pthread_mutex_unlock(&lock);
}
