#include <assert.h>
#include <stdint.h>

#include "record_geometry.h"

int main() {
	for (uint32_t rotation = 0; rotation < 4; rotation++) {
		uint32_t width = rotation & 1 ? 1152 : 720;
		uint32_t height = rotation & 1 ? 720 : 1152;
		int32_t frame[4] = {};
		record_target(width, height, rotation, frame);
		assert(frame[0] == 0 && frame[1] == 0);
		assert(frame[2] == 720 && frame[3] == 1152);
		uint32_t projected_width = rotation & 1 ? frame[3] : frame[2];
		uint32_t projected_height = rotation & 1 ? frame[2] : frame[3];
		assert(projected_width == width && projected_height == height);
	}
	return 0;
}
