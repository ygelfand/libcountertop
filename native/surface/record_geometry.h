#ifndef COUNTERTOP_RECORD_GEOMETRY_H
#define COUNTERTOP_RECORD_GEOMETRY_H

#include <stdint.h>

static inline void record_target(uint32_t width, uint32_t height, uint32_t rotation, int32_t frame[4]) {
	frame[0] = frame[1] = 0;
	frame[2] = static_cast<int32_t>(rotation & 1 ? height : width);
	frame[3] = static_cast<int32_t>(rotation & 1 ? width : height);
}

#endif
