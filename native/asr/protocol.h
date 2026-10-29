#pragma once
#include <stdint.h>

#define ASR_VERSION 1
#define ASR_FD 3
#define ASR_RATE 16000
#define ASR_MAX_PAYLOAD (1 << 20)

#define ASR_READY 'R'
#define ASR_PHRASES 'P'
#define ASR_AUDIO 'A'
#define ASR_END 'E'
#define ASR_PARTIAL 'p'
#define ASR_FINAL 'W'
#define ASR_FAILED 'F'

struct asr_header {
    uint32_t kind;
    uint32_t length;
};

struct asr_ready {
    uint32_t version;
    uint32_t threads;
    uint32_t weights;
};

struct asr_text {
    uint32_t audio_ms;
    uint32_t took_ms;
};
