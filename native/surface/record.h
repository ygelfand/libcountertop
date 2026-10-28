#ifndef COUNTERTOP_RECORD_H
#define COUNTERTOP_RECORD_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif
int record_open(uint32_t width, uint32_t height, uint32_t rotation, uint32_t *out_width, uint32_t *out_height);
#ifdef __cplusplus
}
#endif

#endif
