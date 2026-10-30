#include <dlfcn.h>
#include <errno.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "protocol.h"

typedef void (*decode_cb)(const char *, const void *);
typedef int (*json_cb)(const char *, const char *, const char *, const char *, int, int);

// PryonMultichannelAudioFormat is 144 bytes, passed and returned by value.
typedef struct {
    uint32_t w[36];
} audio_format;

static struct {
    int (*version)(void);
    int (*set_decode)(decode_cb, int);
    int (*set_json)(json_cb);
    int (*model_new)(const char *, const char *, const char *);
    int (*model_delete)(const char *);
    audio_format (*format_default)(void);
    int (*decoder_new)(const char *, const char *, const char *, audio_format, const char *);
    int (*decoder_delete)(const char *);
    int (*push)(const char *, int64_t, const int16_t *, int, const void *);
} api;

typedef struct {
    char id[64];
    char aux[PRYON_MAX_AUX];
    int64_t at;
    int used;
} word_t;

static word_t words[PRYON_MAX_WORDS];
static pthread_mutex_t out = PTHREAD_MUTEX_INITIALIZER;

static int read_full(int fd, void *p, size_t n)
{
    char *c = p;
    while (n > 0) {
        const ssize_t r = read(fd, c, n);
        if (r < 0 && errno == EINTR) {
            continue;
        }
        if (r <= 0) {
            return -1;
        }
        c += r;
        n -= (size_t)r;
    }
    return 0;
}

static int write_all(int fd, const void *p, size_t n)
{
    const char *c = p;
    while (n > 0) {
        const ssize_t w = write(fd, c, n);
        if (w < 0 && errno == EINTR) {
            continue;
        }
        if (w <= 0) {
            return -1;
        }
        c += w;
        n -= (size_t)w;
    }
    return 0;
}

static void send_message(uint32_t kind, const void *head, size_t head_len, const char *tail)
{
    const size_t tail_len = tail ? strlen(tail) : 0;
    struct pryon_header h = {kind, (uint32_t)(head_len + tail_len)};
    pthread_mutex_lock(&out);
    write_all(PRYON_FD, &h, sizeof(h));
    if (head_len) {
        write_all(PRYON_FD, head, head_len);
    }
    if (tail_len) {
        write_all(PRYON_FD, tail, tail_len);
    }
    pthread_mutex_unlock(&out);
}

static void failed(const char *what)
{
    send_message(PRYON_FAILED, NULL, 0, what);
}

static void on_decode(const char *id, const void *event)
{
    (void)id;
    (void)event;
}

static long long field(const char *json, const char *key)
{
    const char *at = strstr(json, key);
    return at ? strtoll(at + strlen(key), NULL, 10) : 0;
}

static int on_json(const char *id, const char *a, const char *b, const char *c, int d, int e)
{
    (void)a;
    (void)b;
    (void)d;
    (void)e;
    if (!id || !c || !strstr(c, "\"kwDetectionType\":\"Accept\"")) {
        return 0;
    }
    const char *score = strstr(c, "\"kwClassificationScore\":");
    struct pryon_detected det = {0};
    if (score) {
        det.score = (uint32_t)(strtod(score + strlen("\"kwClassificationScore\":"), NULL) * 1e6);
    }
    det.start = (uint32_t)field(c, "\"kwSampleStartIndex\":");
    det.end = (uint32_t)field(c, "\"kwSampleEndIndex\":");
    send_message(PRYON_DETECTED, &det, sizeof(det), id);
    return 0;
}

static void *need(void *lib, const char *name)
{
    void *s = dlsym(lib, name);
    if (!s) {
        fprintf(stderr, "pryon: missing %s\n", name);
    }
    return s;
}

static int open_library(const char *path)
{
    void *lib = dlopen(path, RTLD_NOW);
    if (!lib) {
        fprintf(stderr, "pryon: %s\n", dlerror());
        return -1;
    }
    api.version = need(lib, "PryonApi_GetDecodeEventCallbackVersion");
    api.set_decode = need(lib, "PryonApi_SetDecodeEventCallback");
    api.set_json = need(lib, "PryonApi_SetJsondataCallback");
    api.model_new = need(lib, "PryonModelSet_New");
    api.model_delete = need(lib, "PryonModelSet_Delete");
    api.format_default = need(lib, "PryonDecoder_NewPryonMultichannelAudioFormat_Default");
    api.decoder_new = need(lib, "PryonDecoder_NewSpotterAudioDecoder");
    api.decoder_delete = need(lib, "PryonDecoder_Delete");
    api.push = need(lib, "PryonDecoder_PushAudioEventSamples");
    if (!api.version || !api.set_decode || !api.set_json || !api.model_new || !api.model_delete ||
        !api.format_default || !api.decoder_new || !api.decoder_delete || !api.push) {
        return -1;
    }
    const int v = api.version();
    if (v != 1) {
        fprintf(stderr, "pryon: decode callback version %d, want 1\n", v);
        return -1;
    }
    if (api.set_decode(on_decode, v) != 0 || api.set_json(on_json) != 0) {
        fprintf(stderr, "pryon: callback registration refused\n");
        return -1;
    }
    return 0;
}

static word_t *find(const char *id)
{
    for (int i = 0; i < PRYON_MAX_WORDS; i++) {
        if (words[i].used && !strcmp(words[i].id, id)) {
            return &words[i];
        }
    }
    return NULL;
}

static void unload(const char *id)
{
    word_t *w = find(id);
    if (!w) {
        return;
    }
    api.decoder_delete(w->id);
    api.model_delete(w->id);
    w->used = 0;
}

static int decoder(const word_t *w)
{
    return api.decoder_new(w->id, w->id, "pryon", api.format_default(), w->aux);
}

static void load(char *payload, uint32_t n)
{
    const char *id = payload;
    const size_t id_len = strnlen(payload, n);
    if (id_len == 0 || id_len >= sizeof(words[0].id) || id_len + 1 >= n) {
        failed("load: malformed");
        return;
    }
    const char *manifest = payload + id_len + 1;
    const size_t manifest_len = strnlen(manifest, n - id_len - 1);
    if (id_len + 1 + manifest_len + 1 >= n) {
        failed("load: malformed");
        return;
    }
    const char *aux = manifest + manifest_len + 1;
    if (strlen(aux) >= sizeof(words[0].aux)) {
        failed("load: malformed");
        return;
    }
    unload(id);
    word_t *w = NULL;
    for (int i = 0; i < PRYON_MAX_WORDS && !w; i++) {
        if (!words[i].used) {
            w = &words[i];
        }
    }
    struct pryon_loaded r = {0};
    if (!w) {
        r.status = -2;
    } else if (api.model_new(id, manifest, "") != 0) {
        r.status = -3;
    } else {
        memcpy(w->id, id, id_len + 1);
        strcpy(w->aux, aux);
        if (decoder(w) != 0) {
            api.model_delete(id);
            r.status = -4;
        } else {
            w->at = 0;
            w->used = 1;
        }
    }
    send_message(PRYON_LOADED, &r, sizeof(r), id);
}

static void feed(const int16_t *samples)
{
    for (int i = 0; i < PRYON_MAX_WORDS; i++) {
        word_t *w = &words[i];
        if (!w->used) {
            continue;
        }
        if (api.push(w->id, w->at, samples, PRYON_PERIOD, NULL) != 0) {
            api.decoder_delete(w->id);
            if (decoder(w) != 0) {
                failed(w->id);
                api.model_delete(w->id);
                w->used = 0;
                continue;
            }
            w->at = 0;
            continue;
        }
        w->at += PRYON_PERIOD;
    }
}

int main(int argc, char **argv)
{
    if (argc < 2) {
        fprintf(stderr, "usage: %s LIBPRYON\n", argv[0]);
        return 2;
    }
    if (open_library(argv[1]) != 0) {
        return 1;
    }
    struct pryon_ready ready = {PRYON_VERSION};
    send_message(PRYON_READY, &ready, sizeof(ready), NULL);

    static char payload[PRYON_MAX_PAYLOAD + 1];
    static int16_t period[PRYON_PERIOD];
    size_t filled = 0;
    for (;;) {
        struct pryon_header h;
        if (read_full(PRYON_FD, &h, sizeof(h)) != 0 || h.length > PRYON_MAX_PAYLOAD ||
            read_full(PRYON_FD, payload, h.length) != 0) {
            break;
        }
        payload[h.length] = 0;
        switch (h.kind) {
        case PRYON_LOAD:
            load(payload, h.length);
            break;
        case PRYON_UNLOAD:
            unload(payload);
            break;
        case PRYON_AUDIO: {
            const int16_t *s = (const int16_t *)payload;
            size_t count = h.length / sizeof(int16_t);
            while (count > 0) {
                const size_t take = PRYON_PERIOD - filled < count ? PRYON_PERIOD - filled : count;
                memcpy(period + filled, s, take * sizeof(int16_t));
                filled += take;
                s += take;
                count -= take;
                if (filled == PRYON_PERIOD) {
                    feed(period);
                    filled = 0;
                }
            }
            break;
        }
        default:
            failed("unknown message");
        }
    }
    _exit(0);
}
