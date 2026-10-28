#include <android/native_window.h>
#include <atomic>
#include <dlfcn.h>
#include <errno.h>
#include <media/NdkMediaCodec.h>
#include <media/NdkMediaFormat.h>
#include <poll.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>

#include "record.h"
#include "record_geometry.h"
#include "android_refs.h"

struct recording {
	AMediaCodec *codec;
	ANativeWindow *window;
	void *producer;
	void *display;
	int socket;
};

static std::atomic_flag recording_lock = ATOMIC_FLAG_INIT;

template <typename F> static F symbol(const char *name) {
	return reinterpret_cast<F>(dlsym(RTLD_DEFAULT, name));
}

#if __ANDROID_API__ >= 28
static uint32_t mirror_tx[64];
static pthread_once_t mirror_once = PTHREAD_ONCE_INIT;

static void init_mirror(void) {
	uint32_t from[64] = {};
	const float one = 1.0f;
	memcpy(reinterpret_cast<char *>(from) + 0x14, &one, sizeof one);
	from[0x28 / 4] = 0x40;
	auto copy = symbol<void (*)(void *, const void *)>("_ZN7android21SurfaceComposerClient11TransactionC1ERKS1_");
	copy(mirror_tx, from);
}
#endif

static void destroy(recording *r) {
	if (r->display) {
		auto end = symbol<void (*)(void *const *)>("_ZN7android21SurfaceComposerClient14destroyDisplayERKNS_2spINS_7IBinderEEE");
		if (end) end(&r->display);
		static_cast<android::IBinder *>(r->display)->decStrong(r);
	}
	if (r->codec) {
		AMediaCodec_stop(r->codec);
		AMediaCodec_delete(r->codec);
	}
	if (r->window) ANativeWindow_release(r->window);
	if (r->producer) static_cast<android::IGraphicBufferProducer *>(r->producer)->decStrong(r);
	if (r->socket >= 0) close(r->socket);
	free(r);
	recording_lock.clear();
}

static int mirror(recording *r, uint32_t width, uint32_t height, uint32_t w, uint32_t h, uint32_t rotation) {
	auto string = symbol<void (*)(void *, const char *)>("_ZN7android7String8C1EPKc");
	auto unstring = symbol<void (*)(void *)>("_ZN7android7String8D1Ev");
	auto create = symbol<void (*)(void **, const void *, bool)>("_ZN7android21SurfaceComposerClient13createDisplayERKNS_7String8Eb");
	if (!string || !unstring || !create) return -1;
	uint32_t name[8] = {};
	string(name, "countertop-desktop");
	create(&r->display, name, false);
	unstring(name);
	if (!r->display) return -1;
	const int32_t source[4] = {0, 0, static_cast<int32_t>(width), static_cast<int32_t>(height)};
	int32_t target[4];
	record_target(w, h, rotation, target);
#if __ANDROID_API__ >= 28
	auto copy = symbol<void (*)(void *, const void *)>("_ZN7android21SurfaceComposerClient11TransactionC1ERKS1_");
	auto apply = symbol<int (*)(void *, bool)>("_ZN7android21SurfaceComposerClient11Transaction5applyEb");
	auto surface = symbol<int (*)(void *, void *const *, void *const *)>("_ZN7android21SurfaceComposerClient11Transaction17setDisplaySurfaceERKNS_2spINS_7IBinderEEERKNS2_INS_22IGraphicBufferProducerEEE");
	auto stack = symbol<void (*)(void *, void *const *, uint32_t)>("_ZN7android21SurfaceComposerClient11Transaction20setDisplayLayerStackERKNS_2spINS_7IBinderEEEj");
	auto project = symbol<void (*)(void *, void *const *, uint32_t, const int32_t *, const int32_t *)>("_ZN7android21SurfaceComposerClient11Transaction20setDisplayProjectionERKNS_2spINS_7IBinderEEEjRKNS_4RectES9_");
	if (!copy || !apply || !surface || !stack || !project) return -1;
	pthread_once(&mirror_once, init_mirror);
	int status = surface(mirror_tx, &r->display, &r->producer);
	stack(mirror_tx, &r->display, 0);
	project(mirror_tx, &r->display, rotation, source, target);
	int applied = apply(mirror_tx, true);
	return status == 0 && applied == 0 ? 0 : -1;
#else
	auto open = symbol<void (*)()>("_ZN7android21SurfaceComposerClient21openGlobalTransactionEv");
	auto close = symbol<void (*)(bool)>("_ZN7android21SurfaceComposerClient22closeGlobalTransactionEb");
	auto surface = symbol<int (*)(const android::sp<android::IBinder> &, android::sp<android::IGraphicBufferProducer>)>("_ZN7android21SurfaceComposerClient17setDisplaySurfaceERKNS_2spINS_7IBinderEEENS1_INS_22IGraphicBufferProducerEEE");
	auto stack = symbol<void (*)(void *const *, uint32_t)>("_ZN7android21SurfaceComposerClient20setDisplayLayerStackERKNS_2spINS_7IBinderEEEj");
	auto project = symbol<void (*)(void *const *, uint32_t, const int32_t *, const int32_t *)>("_ZN7android21SurfaceComposerClient20setDisplayProjectionERKNS_2spINS_7IBinderEEEjRKNS_4RectES8_");
	if (!open || !close || !surface || !stack || !project) return -1;
	open();
	android::sp<android::IBinder> binder(static_cast<android::IBinder *>(r->display));
	android::sp<android::IGraphicBufferProducer> producer(static_cast<android::IGraphicBufferProducer *>(r->producer));
	int status = surface(binder, producer);
	stack(&r->display, 0);
	project(&r->display, rotation, source, target);
	close(true);
	return status;
#endif
}

static int write_all(int fd, const void *data, size_t size) {
	const char *p = static_cast<const char *>(data);
	while (size) {
		ssize_t n = send(fd, p, size, MSG_NOSIGNAL);
		if (n < 0 && errno == EINTR) continue;
		if (n <= 0) return -1;
		p += n;
		size -= n;
	}
	return 0;
}

static void *drain(void *arg) {
	recording *r = static_cast<recording *>(arg);
	for (;;) {
		pollfd p = {r->socket, POLLIN, 0};
		if (poll(&p, 1, 0) < 0 || (p.revents & (POLLHUP | POLLERR | POLLNVAL))) break;
		if (p.revents & POLLIN) {
			char command;
			if (recv(r->socket, &command, 1, 0) != 1) break;
#if __ANDROID_API__ >= 26
			if (command == 'k') {
				AMediaFormat *f = AMediaFormat_new();
				AMediaFormat_setInt32(f, "request-sync", 0);
				AMediaCodec_setParameters(r->codec, f);
				AMediaFormat_delete(f);
			}
#endif
		}
		AMediaCodecBufferInfo info = {};
		ssize_t index = AMediaCodec_dequeueOutputBuffer(r->codec, &info, 50000);
		if (index == AMEDIACODEC_INFO_OUTPUT_FORMAT_CHANGED || index == AMEDIACODEC_INFO_TRY_AGAIN_LATER) continue;
		if (index < 0) break;
		size_t capacity = 0;
		uint8_t *data = AMediaCodec_getOutputBuffer(r->codec, index, &capacity);
		int err = 0;
		if (!data || info.offset < 0 || info.size < 0 || static_cast<size_t>(info.offset) + info.size > capacity) err = -1;
		else if (info.size) {
			uint32_t header[4] = {static_cast<uint32_t>(info.size), info.flags,
				static_cast<uint32_t>(info.presentationTimeUs), static_cast<uint32_t>(info.presentationTimeUs >> 32)};
			err = write_all(r->socket, header, sizeof header);
			if (!err) err = write_all(r->socket, data + info.offset, info.size);
		}
		AMediaCodec_releaseOutputBuffer(r->codec, index, false);
		if (err || (info.flags & AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM)) break;
	}
	destroy(r);
	return NULL;
}

int record_open(uint32_t width, uint32_t height, uint32_t rotation, uint32_t *out_width, uint32_t *out_height) {
	if (!width || !height || width > 4096 || height > 4096 || rotation > 3) return -1;
	if (recording_lock.test_and_set()) return -1;
	uint32_t w = rotation & 1 ? height : width, h = rotation & 1 ? width : height;
	uint32_t max_w = w >= h ? 1280 : 720, max_h = w >= h ? 720 : 1280;
	if (w > max_w) { h = h * max_w / w; w = max_w; }
	if (h > max_h) { w = w * max_h / h; h = max_h; }
	w &= ~1u;
	h &= ~1u;
	recording *r = static_cast<recording *>(calloc(1, sizeof *r));
	if (!r) { recording_lock.clear(); return -1; }
	r->socket = -1;
	r->codec = AMediaCodec_createEncoderByType("video/avc");
	if (!r->codec) { destroy(r); return -1; }
	AMediaFormat *format = AMediaFormat_new();
	AMediaFormat_setString(format, AMEDIAFORMAT_KEY_MIME, "video/avc");
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_WIDTH, w);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_HEIGHT, h);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_COLOR_FORMAT, 0x7f000789);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_BIT_RATE, 2000000);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_FRAME_RATE, 15);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_I_FRAME_INTERVAL, 1);
	AMediaFormat_setInt32(format, "profile", 1);
	AMediaFormat_setInt32(format, "level", 0x200);
	AMediaFormat_setInt32(format, "repeat-previous-frame-after", 66667);
	AMediaFormat_setFloat(format, "max-fps-to-encoder", 15.0f);
	int status = AMediaCodec_configure(r->codec, format, NULL, NULL, AMEDIACODEC_CONFIGURE_FLAG_ENCODE);
	AMediaFormat_delete(format);
	if (status != AMEDIA_OK) { destroy(r); return -1; }
#if __ANDROID_API__ >= 26
	if (AMediaCodec_createInputSurface(r->codec, &r->window) != AMEDIA_OK || !r->window) { destroy(r); return -1; }
	auto producer = symbol<void (*)(void **, const void *)>("_ZNK7android7Surface25getIGraphicBufferProducerEv");
	if (!producer) { destroy(r); return -1; }
	producer(&r->producer, reinterpret_cast<char *>(r->window) - 2 * sizeof(void *));
#else
	void *lib = dlopen("libstagefright.so", RTLD_NOW | RTLD_LOCAL);
	auto input = lib ? reinterpret_cast<int (*)(void *, void **)>(dlsym(lib, "_ZN7android10MediaCodec18createInputSurfaceEPNS_2spINS_22IGraphicBufferProducerEEE")) : NULL;
	status = input ? input(*reinterpret_cast<void **>(r->codec), &r->producer) : -1;
	if (lib) dlclose(lib);
	if (status) { destroy(r); return -1; }
#endif
	if (!r->producer || AMediaCodec_start(r->codec) != AMEDIA_OK || mirror(r, width, height, w, h, rotation)) {
		destroy(r);
		return -1;
	}
	int pair[2];
	if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, pair)) { destroy(r); return -1; }
	r->socket = pair[0];
	const timeval timeout = {2, 0};
	setsockopt(r->socket, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof timeout);
	pthread_t thread;
	if (pthread_create(&thread, NULL, drain, r)) { close(pair[1]); destroy(r); return -1; }
	pthread_detach(thread);
	*out_width = w;
	*out_height = h;
	return pair[1];
}
