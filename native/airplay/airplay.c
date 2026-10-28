#include <android/log.h>
#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "airplay/aac.h"
#include "airplay/protocol.h"
#include "dnssd.h"
#include "raop.h"
#include "stream.h"

#define logi(...) __android_log_print(ANDROID_LOG_INFO, COUNTERTOP_PREFIX "-airplay", __VA_ARGS__)
#define logw(...) __android_log_print(ANDROID_LOG_WARN, COUNTERTOP_PREFIX "-airplay", __VA_ARGS__)
static pthread_mutex_t out = PTHREAD_MUTEX_INITIALIZER;
static raop_t *raop;

extern void ps_self(void **sret) __asm__("_ZN7android12ProcessState4selfEv");
extern void ps_start_pool(void *self) __asm__("_ZN7android12ProcessState15startThreadPoolEv");

static int write_all(const void *buf, size_t len) {
    const uint8_t *p = buf;
    while (len > 0) {
        ssize_t n = write(AIRPLAY_FD, p, len);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) return -1;
        p += n;
        len -= (size_t)n;
    }
    return 0;
}

static void send_msg(uint32_t kind, const void *head, uint32_t head_len, const void *body, uint32_t body_len) {
    struct airplay_header h = {kind, head_len + body_len};
    pthread_mutex_lock(&out);
    int bad = write_all(&h, sizeof(h)) || (head_len && write_all(head, head_len)) ||
              (body_len && write_all(body, body_len));
    pthread_mutex_unlock(&out);
    if (bad) {
        logw("daemon went away");
        _exit(0);
    }
}

static uint64_t mono_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000u + (uint64_t)ts.tv_nsec / 1000u;
}

static void video_process(void *cls, raop_ntp_t *ntp, video_decode_struct *d) {
    (void)cls;
    (void)ntp;
    if (d->data_len <= 0) return;
    struct airplay_frame f = {mono_us()};
    send_msg(MSG_FRAME, &f, sizeof(f), d->data, (uint32_t)d->data_len);
}

static void send_pcm(uint32_t rate, uint32_t channels, const int16_t *pcm, uint32_t samples) {
    struct airplay_audio a = {rate, channels};
    send_msg(MSG_AUDIO, &a, sizeof(a), pcm, samples * 2);
}

static void audio_process(void *cls, raop_ntp_t *ntp, audio_decode_struct *d) {
    (void)cls;
    (void)ntp;
    aac_decode(d->data, d->data_len, d->rtp_time);
}

static struct airplay_mirror mirror;

static void video_report_size(void *cls, float *ws, float *hs, float *w, float *h) {
    (void)cls;
    (void)ws;
    (void)hs;
    mirror.width = (uint32_t)*w;
    mirror.height = (uint32_t)*h;
}

static int video_set_codec(void *cls, video_codec_t codec) {
    (void)cls;
    if (codec != VIDEO_CODEC_H264) {
        logw("refusing codec %d", codec);
        return -1;
    }
    mirror.codec = CODEC_H264;
    send_msg(MSG_MIRROR, &mirror, sizeof(mirror), NULL, 0);
    return 0;
}

static void report_client_request(void *cls, char *id, char *model, char *name, bool *admit) {
    (void)cls;
    (void)id;
    logi("client %s (%s)", name ? name : "?", model ? model : "?");
    const char *n = name ? name : "";
    send_msg(MSG_CLIENT, NULL, 0, n, (uint32_t)strlen(n));
    *admit = true;
}

static void mirror_video_running(void *cls, bool on) {
    (void)cls;
    if (!on) send_msg(MSG_ENDED, NULL, 0, NULL, 0);
}

static void conn_destroy(void *cls) {
    (void)cls;
    send_msg(MSG_ENDED, NULL, 0, NULL, 0);
}

static void conn_init(void *cls) { (void)cls; }
static void conn_reset(void *cls, int reason) { (void)cls; logi("connection reset %d", reason); }
static void conn_feedback(void *cls) { (void)cls; }
static void video_reset(void *cls, reset_type_t t) { (void)cls; (void)t; }
static void video_pause(void *cls) { (void)cls; }
static void video_resume(void *cls) { (void)cls; }
static void audio_flush(void *cls) { (void)cls; aac_flush(); }
static void video_flush(void *cls) { (void)cls; }
static double audio_set_client_volume(void *cls) { (void)cls; return 0.0; }
static void audio_set_volume(void *cls, float v) { (void)cls; (void)v; }
static void audio_get_format(void *cls, unsigned char *ct, unsigned short *spf, bool *screen, bool *media, uint64_t *fmt) {
    (void)cls;
    logi("audio format ct=%d spf=%d screen=%d media=%d format=%llx", *ct, *spf, *screen, *media, (unsigned long long)*fmt);
    aac_open(*ct, send_pcm);
    if (!*screen) send_msg(MSG_SOUND, NULL, 0, NULL, 0);
}
static void audio_set_metadata(void *cls, const void *b, int n) { (void)cls; (void)b; (void)n; }
static void audio_set_coverart(void *cls, const void *b, int n) { (void)cls; (void)b; (void)n; }
static void audio_stop_coverart_rendering(void *cls) { (void)cls; }
static void audio_set_progress(void *cls, uint32_t *s, uint32_t *c, uint32_t *e) { (void)cls; (void)s; (void)c; (void)e; }
static void display_pin(void *cls, char *pin) { (void)cls; (void)pin; }
static void register_client(void *cls, const char *id, const char *pk, const char *name) { (void)cls; (void)id; (void)pk; (void)name; }
static bool check_register(void *cls, const char *pk) { (void)cls; (void)pk; return false; }
static const char *passwd(void *cls, int *len) { (void)cls; *len = 0; return NULL; }
static void export_dacp(void *cls, const char *a, const char *d) { (void)cls; (void)a; (void)d; }
static void on_video_play(void *cls, const char *loc, const float start) { (void)cls; (void)loc; (void)start; }
static void on_video_scrub(void *cls, const float p) { (void)cls; (void)p; }
static void on_video_rate(void *cls, const float r) { (void)cls; (void)r; }
static void on_video_stop(void *cls) { (void)cls; }
static void on_video_acquire_playback_info(void *cls, playback_info_t *p) { (void)cls; (void)p; }
static float on_video_playlist_remove(void *cls) { (void)cls; return 0; }
static void get_custom_profile(void *cls, const char **p) { (void)cls; *p = NULL; }

static void log_line(void *cls, int level, const char *msg) {
    (void)cls;
    if (level <= 4) logw("%s", msg);
    else if (level <= 6) logi("%s", msg);
}

static int parse_mac(const char *s, char out[6]) {
    unsigned v[6];
    if (sscanf(s, "%x:%x:%x:%x:%x:%x", &v[0], &v[1], &v[2], &v[3], &v[4], &v[5]) != 6) return -1;
    for (int i = 0; i < 6; i++) out[i] = (char)v[i];
    return 0;
}

static int read_all(void *buf, size_t len) {
    uint8_t *p = buf;
    while (len > 0) {
        ssize_t n = read(AIRPLAY_FD, p, len);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) return -1;
        p += n;
        len -= (size_t)n;
    }
    return 0;
}

static void listen_daemon(void) {
    for (;;) {
        struct airplay_header h;
        if (read_all(&h, sizeof(h))) return;
        if (h.length > AIRPLAY_MAX_PAYLOAD) return;
        char skip[256];
        uint32_t left = h.length;
        while (left > 0) {
            uint32_t n = left < sizeof(skip) ? left : (uint32_t)sizeof(skip);
            if (read_all(skip, n)) return;
            left -= n;
        }
        if (h.kind == MSG_DISCONNECT) raop_remove_known_connections(raop);
    }
}

int main(int argc, char **argv) {
    if (argc < 6) {
        fprintf(stderr, "usage: %s NAME MAC KEYFILE WIDTH HEIGHT\n", argv[0]);
        return 2;
    }
    const char *name = argv[1];
    char hw[6];
    if (parse_mac(argv[2], hw)) {
        fprintf(stderr, "bad mac %s\n", argv[2]);
        return 2;
    }
    signal(SIGPIPE, SIG_IGN);
    void *ps = NULL;
    ps_self(&ps);
    ps_start_pool(ps);

    raop_callbacks_t cb;
    memset(&cb, 0, sizeof(cb));
    cb.conn_init = conn_init;
    cb.conn_destroy = conn_destroy;
    cb.conn_reset = conn_reset;
    cb.conn_feedback = conn_feedback;
    cb.audio_process = audio_process;
    cb.video_process = video_process;
    cb.audio_flush = audio_flush;
    cb.video_flush = video_flush;
    cb.video_pause = video_pause;
    cb.video_resume = video_resume;
    cb.audio_set_client_volume = audio_set_client_volume;
    cb.audio_set_volume = audio_set_volume;
    cb.audio_get_format = audio_get_format;
    cb.video_report_size = video_report_size;
    cb.audio_set_metadata = audio_set_metadata;
    cb.audio_set_coverart = audio_set_coverart;
    cb.audio_stop_coverart_rendering = audio_stop_coverart_rendering;
    cb.audio_set_progress = audio_set_progress;
    cb.report_client_request = report_client_request;
    cb.display_pin = display_pin;
    cb.register_client = register_client;
    cb.check_register = check_register;
    cb.passwd = passwd;
    cb.export_dacp = export_dacp;
    cb.video_reset = video_reset;
    cb.video_set_codec = video_set_codec;
    cb.mirror_video_running = mirror_video_running;
    cb.on_video_play = on_video_play;
    cb.on_video_scrub = on_video_scrub;
    cb.on_video_rate = on_video_rate;
    cb.on_video_stop = on_video_stop;
    cb.on_video_playlist_remove = on_video_playlist_remove;
    cb.on_video_acquire_playback_info = on_video_acquire_playback_info;
    cb.get_custom_profile = get_custom_profile;

    raop = raop_init(&cb);
    if (!raop) return 1;
    raop_set_log_callback(raop, log_line, NULL);
    raop_set_log_level(raop, 6);
    if (raop_init2(raop, 0, argv[2], argv[3])) return 1;
    raop_set_plist(raop, "width", atoi(argv[4]));
    raop_set_plist(raop, "height", atoi(argv[5]));
    raop_set_plist(raop, "refreshRate", 60);
    raop_set_plist(raop, "maxFPS", 30);

    unsigned short tcp[2] = {0, 0}, udp[3] = {0, 0, 0};
    raop_set_tcp_ports(raop, tcp);
    raop_set_udp_ports(raop, udp);
    unsigned short port = raop_get_port(raop);
    if (raop_start_httpd(raop, &port) < 0) {
        logw("httpd failed");
        return 1;
    }
    raop_set_port(raop, port);

    int err = 0;
    dnssd_t *dnssd = dnssd_init(name, (int)strlen(name), hw, sizeof(hw), 0, &err);
    if (err || !dnssd) {
        logw("dnssd_init %d", err);
        return 1;
    }
    raop_set_dnssd(raop, dnssd);
    if ((err = dnssd_register_raop(dnssd, port))) logw("register raop %d", err);
    if ((err = dnssd_register_airplay(dnssd, port))) logw("register airplay %d", err);
    logi("listening as \"%s\" on port %d", name, port);

    struct airplay_ready r = {AIRPLAY_VERSION, port};
    send_msg(MSG_READY, &r, sizeof(r), NULL, 0);
    listen_daemon();
    dnssd_unregister_airplay(dnssd);
    dnssd_unregister_raop(dnssd);
    return 0;
}
