NATIVE := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
NDK ?= $(firstword $(ANDROID_NDK_HOME) $(ANDROID_NDK_LATEST_HOME) $(wildcard /opt/homebrew/Caskroom/android-ndk/*/AndroidNDK*.app/Contents/NDK))
NDK_HOST := $(if $(filter Darwin,$(shell uname -s)),darwin-x86_64,linux-x86_64)
PREFIX ?= countertop
OUT ?= $(CURDIR)/bin
NDK_CC := $(NDK)/toolchains/llvm/prebuilt/$(NDK_HOST)/bin/armv7a-linux-androideabi$(API)-clang
SURFACE_BIN ?= $(OUT)/$(PREFIX)-surface
CAMERA_BIN ?= $(OUT)/$(PREFIX)-camera
CAMSHIM_BIN ?= $(OUT)/lib$(PREFIX)-camshim.so
STUBS := $(OUT)/countertop-stubs/$(PREFIX)-api$(API)
FLAGS := -O2 -Wall -Werror -I$(NATIVE) -DCOUNTERTOP_PREFIX='"$(PREFIX)"'
SURFACE := $(NATIVE)/surface
CAMERA := $(NATIVE)/camera
CAMERA_LIBS := camera_client gui utils binder $(if $(filter 25,$(API)),stagefright)
CAMERA_GPU_LIBS := $(if $(filter 27 28,$(API)),-lEGL -lGLESv2)
SURFACE_LIBS := gui utils binder $(if $(filter 28,$(API)),ui)
CAMERA_SOURCES := $(CAMERA)/camera.c $(CAMERA)/turn.c $(CAMERA)/cam2.c $(if $(filter 25,$(API)),$(CAMERA)/still.c)
NDK_BIN := $(NDK)/toolchains/llvm/prebuilt/$(NDK_HOST)/bin
NDK_CXX := $(NDK_BIN)/armv7a-linux-androideabi$(API)-clang++
VOICE_BIN ?= $(OUT)/$(PREFIX)-voice
VOICE := $(NATIVE)/voice
NEEDLE := $(VOICE)/third_party/needle
WHISTLE_ENGINE := _ZN12_GLOBAL__N_19g_whistleE
WHISTLE_DEPTH := _ZN7whistle6Engine9set_depthEi
VOICE_FLAGS := -O2 -Wall -Werror -I$(NATIVE) -I$(VOICE) -I$(NEEDLE)
AIRPLAY_BIN ?= $(OUT)/$(PREFIX)-airplay
AIRPLAY := $(NATIVE)/airplay
UXPLAY := $(AIRPLAY)/uxplay
OPENSSL := $(AIRPLAY)/third_party/openssl
PLIST := $(AIRPLAY)/third_party/libplist
UXPLAY_SOURCES := $(wildcard $(UXPLAY)/*.c $(UXPLAY)/playfair/*.c $(UXPLAY)/llhttp/*.c $(UXPLAY)/mdnsd/*.c)
UXPLAY_INCLUDES := -I$(UXPLAY) -I$(UXPLAY)/playfair -I$(UXPLAY)/llhttp -I$(UXPLAY)/mdnsd -I$(OPENSSL)/include -I$(PLIST)/include
UXPLAY_FLAGS := -O2 -fPIC -w -D_GNU_SOURCE -DPLIST_210 -DPLIST_230 $(UXPLAY_INCLUDES)

.PHONY: all surface camera camshim voice airplay check-ndk test test-voice
all: surface camera camshim voice airplay
check-ndk:
	@test "$(API)" = 25 -o "$(API)" = 27 -o "$(API)" = 28 || { echo "supported native API levels: 25, 27, 28"; exit 1; }
	@test -x "$(NDK_CC)" || { echo "no NDK clang at $(NDK_CC); set NDK or ANDROID_NDK_HOME"; exit 1; }

surface: check-ndk
	@mkdir -p $(dir $(SURFACE_BIN)) $(STUBS)/surface
	$(NDK_CXX) $(FLAGS) -std=c++17 -fno-exceptions -fno-rtti -c $(SURFACE)/record.cpp -o $(STUBS)/surface/record.o
	@for lib in $(SURFACE_LIBS); do \
		up=$$(echo $$lib | tr a-z A-Z); \
		$(NDK_CC) -shared -DLIB$$up -Wl,-soname,lib$$lib.so -o $(STUBS)/surface/lib$$lib.so $(SURFACE)/stubs.c || exit 1; \
	done
	$(NDK_CC) $(FLAGS) -o $(SURFACE_BIN) $(SURFACE)/surface.c $(SURFACE)/video.c $(SURFACE)/gl.c $(SURFACE)/ui.c $(SURFACE)/drm.c $(SURFACE)/audio.c $(SURFACE)/wire.c $(STUBS)/surface/record.o \
		-L$(STUBS)/surface $(addprefix -l,$(SURFACE_LIBS)) -landroid -lmediandk -lEGL -lGLESv2 -llog -lm -ldl -Wl,--allow-shlib-undefined

camera: check-ndk
	@mkdir -p $(dir $(CAMERA_BIN)) $(STUBS)/camera
	@for lib in $(CAMERA_LIBS); do \
		up=$$(echo $$lib | tr a-z A-Z); \
		$(NDK_CC) -shared -DLIB$$up -Wl,-soname,lib$$lib.so -o $(STUBS)/camera/lib$$lib.so $(CAMERA)/stubs.c || exit 1; \
	done
	$(NDK_CC) $(FLAGS) -o $(CAMERA_BIN) $(CAMERA_SOURCES) \
		-L$(STUBS)/camera $(addprefix -l,$(CAMERA_LIBS)) -lcamera2ndk -lmediandk -landroid $(CAMERA_GPU_LIBS) -llog -lm -Wl,--allow-shlib-undefined

camshim: check-ndk
	@mkdir -p $(dir $(CAMSHIM_BIN))
	$(NDK_CC) $(FLAGS) -Wno-unused-parameter -shared -fPIC -o $(CAMSHIM_BIN) $(NATIVE)/camshim/camshim.c

voice: check-ndk
	@mkdir -p $(dir $(VOICE_BIN)) $(STUBS)/voice
	@cd $(NEEDLE) && shasum -a 256 -c --quiet SHA256SUMS
	$(NDK_BIN)/llvm-objcopy --globalize-symbol=$(WHISTLE_ENGINE) $(NEEDLE)/android-armv7/libneedle.a $(STUBS)/voice/libneedle.a
	@$(NDK_BIN)/llvm-nm -S $(STUBS)/voice/libneedle.a | grep -q ' 00000004 B $(WHISTLE_ENGINE)$$' || { echo "libneedle.a: $(WHISTLE_ENGINE) missing or resized"; exit 1; }
	@$(NDK_BIN)/llvm-nm $(STUBS)/voice/libneedle.a | grep -q ' [WT] $(WHISTLE_DEPTH)$$' || { echo "libneedle.a: $(WHISTLE_DEPTH) missing"; exit 1; }
	$(NDK_CXX) $(VOICE_FLAGS) -std=c++17 -o $(VOICE_BIN) $(VOICE)/voice.cpp $(STUBS)/voice/libneedle.a \
		-static-libstdc++ -s -llog -lm

airplay: check-ndk
	@mkdir -p $(dir $(AIRPLAY_BIN)) $(STUBS)/airplay
	@cd $(OPENSSL) && shasum -a 256 -c --quiet SHA256SUMS
	@cd $(PLIST) && shasum -a 256 -c --quiet SHA256SUMS
	$(NDK_CC) -shared -DLIBBINDER -Wl,-soname,libbinder.so -o $(STUBS)/airplay/libbinder.so $(CAMERA)/stubs.c
	@for f in $(UXPLAY_SOURCES); do \
		o=$(STUBS)/airplay/$$(echo $${f#$(UXPLAY)/} | tr / _ | sed 's/\.c$$/.o/'); \
		$(NDK_CC) $(UXPLAY_FLAGS) -c $$f -o $$o || exit 1; \
	done
	$(NDK_CC) $(FLAGS) $(UXPLAY_INCLUDES) -o $(AIRPLAY_BIN) $(AIRPLAY)/airplay.c $(AIRPLAY)/aac.c $(STUBS)/airplay/*.o \
		$(PLIST)/android-armv7/libplist-2.0.a $(OPENSSL)/android-armv7/libcrypto.a -s \
		-L$(STUBS)/airplay -lbinder -lmediandk -llog -lm -Wl,--allow-shlib-undefined

test:
	@mkdir -p $(OUT)
	$(or $(HOST_CC),cc) -O1 -g -Wall -Werror -fsanitize=undefined -fno-sanitize-recover=all -I$(NATIVE) -I$(SURFACE) -o $(OUT)/wire_test $(SURFACE)/wire.c $(SURFACE)/test/wire_test.c
	$(OUT)/wire_test
	$(or $(HOST_CXX),c++) -std=c++17 -O1 -g -Wall -Werror -fno-rtti -fsanitize=undefined -fno-sanitize-recover=all -I$(SURFACE) -o $(OUT)/record_refs_test $(SURFACE)/test/record_refs_test.cpp
	$(OUT)/record_refs_test
	$(or $(HOST_CXX),c++) -std=c++17 -O1 -g -Wall -Werror -fsanitize=undefined -fno-sanitize-recover=all -I$(SURFACE) -o $(OUT)/record_geometry_test $(SURFACE)/test/record_geometry_test.cpp
	$(OUT)/record_geometry_test

WHISTLE_MODEL_URL := https://huggingface.co/Cactus-Compute/whistle/resolve/ca5287601bef25af26dcf2e1b2bdc0843a7c19e5/whistle.cact
WHISTLE_MODEL_SHA256 := b6e02f048568ac5d01a2042556c658061e699acbc0aa2a1439f52f3d461dffeb

test-voice:
	@test "$$(uname -s)-$$(uname -m)" = Linux-x86_64 || { echo "test-voice needs linux-x86_64 with clang and libc++ 21"; exit 1; }
	@mkdir -p $(OUT)
	@cd $(NEEDLE) && sha256sum -c --quiet SHA256SUMS
	@test -f $(OUT)/whistle.cact || curl -sSfL -o $(OUT)/whistle.cact $(WHISTLE_MODEL_URL)
	@echo "$(WHISTLE_MODEL_SHA256)  $(OUT)/whistle.cact" | sha256sum -c --quiet
	objcopy --globalize-symbol=$(WHISTLE_ENGINE) $(NEEDLE)/linux-x86_64/libneedle.a $(OUT)/libneedle.a
	$(or $(HOST_CXX),clang++) -stdlib=libc++ -O2 -std=c++17 -Wall -Werror -I$(NEEDLE) -o $(OUT)/depth_test $(VOICE)/test/depth_test.cpp $(OUT)/libneedle.a -lpthread -lm
	$(OUT)/depth_test $(OUT)/whistle.cact $(VOICE)/test/speech.raw
