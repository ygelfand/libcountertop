#pragma once
#include <stdint.h>

#define PRYON_VERSION 2
#define PRYON_FD 3
#define PRYON_RATE 16000
#define PRYON_PERIOD 320
#define PRYON_MAX_PAYLOAD (1 << 16)
#define PRYON_MAX_WORDS 8
#define PRYON_MAX_AUX 512

#define PRYON_READY 'R'
#define PRYON_LOAD 'L'
#define PRYON_UNLOAD 'U'
#define PRYON_AUDIO 'A'
#define PRYON_LOADED 'K'
#define PRYON_DETECTED 'D'
#define PRYON_FAILED 'F'

struct pryon_header {
    uint32_t kind;
    uint32_t length;
};

struct pryon_ready {
    uint32_t version;
};

struct pryon_loaded {
    int32_t status;
};

struct pryon_detected {
    uint32_t score;
    uint32_t start;
    uint32_t end;
};
