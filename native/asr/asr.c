#include <errno.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "pieasr/pieasr.h"
#include "protocol.h"
#include "runtime/parallel.h"

#define BEAM 4
#define BONUS 2.0f
#define FRAME_MS 40
#define LOG_ZERO (-23.025850929940457f)
#define SLICE_TAIL 280
#define KEEP_SAMPLES (ASR_RATE * 4)
#define MAX_TOKENS 4096

typedef struct {
    pieasr_model_t *m;
    pieasr_fbank_t *fb;
    pieasr_beam_t *beam;
    pieasr_trie_t *trie;
    const char *const *pieces;
    float feats[(PIEASR_CHUNK_IN + 8) * PIEASR_MEL];
    int16_t *buf;
    size_t len, cap;
    int64_t start, written, f0;
    int chunks;
    int16_t tok[MAX_TOKENS];
    int32_t fr[MAX_TOKENS];
    float lp[MAX_TOKENS];
    int ntok, sent;
} stream_t;

static double now_ms(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1e3 + ts.tv_nsec / 1e6;
}

static void *zalloc(size_t n)
{
    return calloc(1, n);
}

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

static int send_msg(uint32_t kind, const void *head, size_t hn, const void *body, size_t bn)
{
    const struct asr_header h = {kind, (uint32_t)(hn + bn)};
    return write_all(ASR_FD, &h, sizeof h) || write_all(ASR_FD, head, hn) || write_all(ASR_FD, body, bn) ? -1 : 0;
}

static void failed(const char *what)
{
    fprintf(stderr, "%s\n", what);
    send_msg(ASR_FAILED, NULL, 0, what, strlen(what));
}

static void *read_file(const char *path, size_t *size)
{
    FILE *f = fopen(path, "rb");
    if (!f) {
        return NULL;
    }
    fseek(f, 0, SEEK_END);
    const long n = ftell(f);
    fseek(f, 0, SEEK_SET);
    void *p = n > 0 ? malloc((size_t)n) : NULL;
    if (!p || fread(p, 1, (size_t)n, f) != (size_t)n) {
        free(p);
        fclose(f);
        return NULL;
    }
    fclose(f);
    *size = (size_t)n;
    return p;
}

typedef struct {
    char *p;
    size_t len, cap;
} text_t;

static void put(text_t *t, const char *s, size_t n)
{
    if (t->len + n + 1 > t->cap) {
        t->cap = (t->len + n + 1) * 2;
        t->p = realloc(t->p, t->cap);
    }
    memcpy(t->p + t->len, s, n);
    t->len += n;
    t->p[t->len] = 0;
}

static void put_str(text_t *t, const char *s)
{
    put(t, s, strlen(s));
}

static void put_piece(text_t *t, const char *piece, int json)
{
    for (const unsigned char *c = (const unsigned char *)piece; *c; c++) {
        if (c[0] == 0xE2 && c[1] == 0x96 && c[2] == 0x81) {
            put(t, " ", 1);
            c += 2;
        } else if (json && (*c == '"' || *c == '\\')) {
            const char esc[2] = {'\\', (char)*c};
            put(t, esc, 2);
        } else if (json && *c < 0x20) {
            char u[8];
            snprintf(u, sizeof u, "\\u%04x", *c);
            put_str(t, u);
        } else {
            put(t, (const char *)c, 1);
        }
    }
}

static void plain(const stream_t *s, text_t *t)
{
    for (int i = 0; i < s->ntok; i++) {
        put_piece(t, s->pieces[s->tok[i]], 0);
    }
}

static void take(stream_t *s, int upto)
{
    const int room = MAX_TOKENS - s->ntok;
    if (room > 0) {
        s->ntok += pieasr_beam_take_scored(s->beam, upto, s->tok + s->ntok, s->fr + s->ntok, s->lp + s->ntok, room);
    }
}

static void run_chunk(stream_t *s, const float *x)
{
    static float enc[PIEASR_OUT_T * PIEASR_DIM];
    pieasr_encode_chunk(s->m, x, enc);
    pieasr_beam_chunk(s->beam, s->m, enc, PIEASR_OUT_T);
    s->chunks++;
    take(s, pieasr_beam_settled(s->beam));
}

static void partial(stream_t *s)
{
    if (s->ntok == s->sent) {
        return;
    }
    s->sent = s->ntok;
    text_t t = {0};
    plain(s, &t);
    const struct asr_text h = {(uint32_t)(s->written * 1000 / ASR_RATE), 0};
    send_msg(ASR_PARTIAL, &h, sizeof h, t.p, t.len);
    free(t.p);
}

static const int16_t *at(const stream_t *s, int64_t sample)
{
    return s->buf + (sample - s->start);
}

static void compact(stream_t *s)
{
    const int64_t b = s->f0 ? (int64_t)PIEASR_FBANK_SHIFT * (s->f0 - 1) : 0;
    if (b - s->start < KEEP_SAMPLES) {
        return;
    }
    const size_t drop = (size_t)(b - s->start);
    memmove(s->buf, s->buf + drop, sizeof(int16_t) * (s->len - drop));
    s->len -= drop;
    s->start += (int64_t)drop;
}

static void run_ready(stream_t *s)
{
    for (;;) {
        const int64_t b = s->f0 ? (int64_t)PIEASR_FBANK_SHIFT * (s->f0 - 1) : 0;
        const int64_t e = (int64_t)PIEASR_FBANK_SHIFT * (s->f0 + PIEASR_CHUNK_IN - 1) + SLICE_TAIL;
        if (s->written < e) {
            break;
        }
        pieasr_fbank_compute_pcm16(s->fb, at(s, b), (int)(e - b), s->feats);
        run_chunk(s, s->feats + (s->f0 ? PIEASR_MEL : 0));
        s->f0 += PIEASR_CHUNK_STEP;
    }
    compact(s);
    partial(s);
}

static void audio(stream_t *s, const int16_t *pcm, size_t n)
{
    if (s->len + n > s->cap) {
        s->cap = (s->len + n) * 2;
        s->buf = realloc(s->buf, sizeof(int16_t) * s->cap);
    }
    memcpy(s->buf + s->len, pcm, sizeof(int16_t) * n);
    s->len += n;
    s->written += (int64_t)n;
    run_ready(s);
}

static void restart(stream_t *s)
{
    pieasr_reset(s->m);
    pieasr_beam_reset(s->beam, s->m);
    pieasr_beam_set_phrases(s->beam, s->trie);
    s->len = 0;
    s->start = s->written = s->f0 = 0;
    s->chunks = s->ntok = s->sent = 0;
}

static void finish(stream_t *s)
{
    const double t0 = now_ms();
    const int64_t n = s->written, b = s->f0 ? (int64_t)PIEASR_FBANK_SHIFT * (s->f0 - 1) : 0;
    const int nf = pieasr_fbank_num_frames((int)n), nchunks = nf / PIEASR_CHUNK_STEP + 1;
    const int j0 = s->f0 ? 1 : 0;
    const int rf = n > b ? pieasr_fbank_num_frames((int)(n - b)) : 0;
    const int64_t total = j0 + ((int64_t)nchunks * PIEASR_CHUNK_STEP - s->f0) + PIEASR_CHUNK_IN + 8;
    float *rest = malloc(sizeof(float) * (size_t)total * PIEASR_MEL);
    if (!rest) {
        failed("no memory for the end of an utterance");
        restart(s);
        return;
    }
    for (int64_t i = 0; i < total * PIEASR_MEL; i++) {
        rest[i] = LOG_ZERO;
    }
    if (rf > 0) {
        pieasr_fbank_compute_pcm16(s->fb, at(s, b), (int)(n - b), rest);
    }
    for (int64_t i = (int64_t)(j0 + nf - s->f0) * PIEASR_MEL; i < total * PIEASR_MEL; i++) {
        rest[i] = LOG_ZERO;
    }
    const int64_t f0 = s->f0;
    while (s->chunks < nchunks) {
        run_chunk(s, rest + (j0 + (int64_t)s->chunks * PIEASR_CHUNK_STEP - f0) * PIEASR_MEL);
    }
    free(rest);
    pieasr_beam_finish(s->beam);
    take(s, 1 << 30);

    text_t j = {0};
    put_str(&j, "{\"text\":\"");
    for (int i = 0; i < s->ntok; i++) {
        put_piece(&j, s->pieces[s->tok[i]], 1);
    }
    put_str(&j, "\",\"tokens\":[");
    for (int i = 0; i < s->ntok; i++) {
        char num[48];
        put_str(&j, i ? ",{\"p\":\"" : "{\"p\":\"");
        put_piece(&j, s->pieces[s->tok[i]], 1);
        snprintf(num, sizeof num, "\",\"ms\":%d,\"prob\":%.3f}", s->fr[i] * FRAME_MS, expf(s->lp[i]));
        put_str(&j, num);
    }
    put_str(&j, "]}");
    const struct asr_text h = {(uint32_t)(n * 1000 / ASR_RATE), (uint32_t)(now_ms() - t0)};
    send_msg(ASR_FINAL, &h, sizeof h, j.p, j.len);
    free(j.p);
    restart(s);
}

static void phrases(stream_t *s, char *text)
{
    pieasr_trie_t *t = pieasr_trie_new(zalloc, free);
    if (!t) {
        failed("no memory for the phrases");
        return;
    }
    int added = 0, skipped = 0;
    char *save = NULL;
    for (char *line = strtok_r(text, "\n", &save); line; line = strtok_r(NULL, "\n", &save)) {
        if (!*line) {
            continue;
        }
        if (pieasr_trie_add_text(t, s->m, line, BONUS) < 0) {
            fprintf(stderr, "phrase \"%s\": a word the vocabulary cannot spell\n", line);
            skipped++;
        } else {
            added++;
        }
    }
    pieasr_trie_build(t);
    pieasr_beam_set_phrases(s->beam, t);
    if (s->trie) {
        pieasr_trie_free(s->trie);
    }
    s->trie = t;
    fprintf(stderr, "phrases: %d added, %d skipped, %d trie nodes\n", added, skipped, pieasr_trie_nodes(t));
}

int main(int argc, char **argv)
{
    if (argc != 3) {
        fprintf(stderr, "usage: %s <weights> <threads>\n", argv[0]);
        return 2;
    }
    static stream_t s;
    size_t size = 0;
    void *weights = read_file(argv[1], &size);
    if (!weights) {
        failed("the weights could not be read");
        return 1;
    }
    const int threads = atoi(argv[2]);
    pq_par_set(threads);
    pieasr_opt_int = 1;
    pieasr_opt_i16 = PIEASR_I16_ALL;
    pieasr_opt_q8 = 2;
    s.m = zalloc(pieasr_model_size());
    if (!s.m || pieasr_init(s.m, weights, size, zalloc)) {
        failed("the weights could not be loaded");
        return 1;
    }
    s.fb = zalloc(pieasr_fbank_size());
    pieasr_fbank_init(s.fb);
    s.beam = pieasr_beam_new(BEAM, zalloc);
    if (!s.beam) {
        failed("no memory for the search");
        return 1;
    }
    s.pieces = pieasr_tokens(s.m);
    restart(&s);
    const struct asr_ready r = {ASR_VERSION, (uint32_t)threads, (uint32_t)size};
    if (send_msg(ASR_READY, &r, sizeof r, NULL, 0)) {
        return 1;
    }
    char *payload = malloc(ASR_MAX_PAYLOAD + 1);
    for (;;) {
        struct asr_header h;
        if (read_full(ASR_FD, &h, sizeof h)) {
            return 0;
        }
        if (h.length > ASR_MAX_PAYLOAD || read_full(ASR_FD, payload, h.length)) {
            failed("a message too large or cut short");
            return 1;
        }
        payload[h.length] = 0;
        switch (h.kind) {
        case ASR_PHRASES:
            phrases(&s, payload);
            break;
        case ASR_AUDIO:
            audio(&s, (const int16_t *)payload, h.length / sizeof(int16_t));
            break;
        case ASR_END:
            finish(&s);
            break;
        default:
            failed("an unknown message");
        }
    }
}
