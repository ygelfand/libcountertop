#pragma once
#include <stdint.h>

#define AIRPLAY_VERSION 4

#define AIRPLAY_PORT 7000
#define AIRPLAY_MIRROR_PORT 7100
#define AIRPLAY_DATA_PORT 6000
#define AIRPLAY_CONTROL_PORT 6001
#define AIRPLAY_TIMING_PORT 7011
#define AIRPLAY_FD 3
#define AIRPLAY_MAX_PAYLOAD (4 << 20)

#define MSG_READY 'R'
#define MSG_CLIENT 'C'
#define MSG_MIRROR 'M'
#define MSG_FRAME 'V'
#define MSG_ENDED 'E'
#define MSG_DISCONNECT 'X'
#define MSG_AUDIO 'A'
#define MSG_SOUND 'S'

#define CODEC_H264 1
#define CODEC_H265 2

struct airplay_header {
    uint32_t kind;
    uint32_t length;
};

struct airplay_ready {
    uint32_t version;
    uint32_t port;
};

struct airplay_mirror {
    uint32_t codec;
    uint32_t width;
    uint32_t height;
};

struct airplay_audio {
    uint32_t rate;
    uint32_t channels;
    int32_t due_us;
};

struct airplay_frame {
    uint64_t at_us;
};
