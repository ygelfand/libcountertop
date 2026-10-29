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
ASR_BIN ?= $(OUT)/$(PREFIX)-asr
ASR := $(NATIVE)/asr
PIEASR := $(ASR)/third_party/pieasr
PIEASR_SOURCES := $(filter-out $(PIEASR)/src/kernels/c/tile.c $(PIEASR)/src/kernels/c/decode.c,$(shell find $(PIEASR)/src -name '*.c'))
ASR_FLAGS := -O2 -Wall -Werror -Wno-unused-function -std=gnu11 -ffp-contract=off -mfpu=neon -I$(ASR) -I$(PIEASR)/include -I$(PIEASR)/src
AIRPLAY_BIN ?= $(OUT)/$(PREFIX)-airplay
AIRPLAY := $(NATIVE)/airplay
UXPLAY := $(AIRPLAY)/uxplay
OPENSSL := $(AIRPLAY)/third_party/openssl
PLIST := $(AIRPLAY)/third_party/libplist
UXPLAY_SOURCES := $(wildcard $(UXPLAY)/*.c $(UXPLAY)/playfair/*.c $(UXPLAY)/llhttp/*.c $(UXPLAY)/mdnsd/*.c)
UXPLAY_INCLUDES := -I$(UXPLAY) -I$(UXPLAY)/playfair -I$(UXPLAY)/llhttp -I$(UXPLAY)/mdnsd -I$(OPENSSL)/include -I$(PLIST)/include
UXPLAY_FLAGS := -O2 -fPIC -w -D_GNU_SOURCE -DPLIST_210 -DPLIST_230 $(UXPLAY_INCLUDES)

.PHONY: all surface camera camshim asr airplay check-ndk test
all: surface camera camshim asr airplay
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

asr: check-ndk
	@mkdir -p $(dir $(ASR_BIN))
	$(NDK_CC) $(ASR_FLAGS) -o $(ASR_BIN) $(ASR)/asr.c $(PIEASR_SOURCES) -s -lm

airplay: check-ndk
	@mkdir -p $(dir $(AIRPLAY_BIN)) $(STUBS)/airplay
	@cd $(OPENSSL) && shasum -a 256 -c --quiet SHA256SUMS
	@cd $(PLIST) && shasum -a 256 -c --quiet SHA256SUMS
	$(NDK_CC) -shared -DLIBBINDER -Wl,-soname,libbinder.so -o $(STUBS)/airplay/libbinder.so $(CAMERA)/stubs.c
	@for f in $(UXPLAY_SOURCES); do \
		o=$(STUBS)/airplay/$$(echo $${f#$(UXPLAY)/} | tr / _ | sed 's/\.c$$/.o/'); \
		$(NDK_CC) $(UXPLAY_FLAGS) -c $$f -o $$o || exit 1; \
	done
	$(NDK_CC) $(UXPLAY_FLAGS) -c $(AIRPLAY)/third_party/alac/alac.c -o $(STUBS)/airplay/alac.o
	$(NDK_CC) $(FLAGS) $(UXPLAY_INCLUDES) -o $(AIRPLAY_BIN) $(AIRPLAY)/airplay.c $(AIRPLAY)/aac.c $(STUBS)/airplay/*.o \
		$(PLIST)/android-armv7/libplist-2.0.a $(OPENSSL)/android-armv7/libcrypto.a -s \
		-L$(STUBS)/airplay -lbinder -ldl -llog -lm -Wl,--allow-shlib-undefined

test:
	@mkdir -p $(OUT)
	$(or $(HOST_CC),cc) -O1 -g -Wall -Werror -fsanitize=undefined -fno-sanitize-recover=all -I$(NATIVE) -I$(SURFACE) -o $(OUT)/wire_test $(SURFACE)/wire.c $(SURFACE)/test/wire_test.c
	$(OUT)/wire_test
	$(or $(HOST_CXX),c++) -std=c++17 -O1 -g -Wall -Werror -fno-rtti -fsanitize=undefined -fno-sanitize-recover=all -I$(SURFACE) -o $(OUT)/record_refs_test $(SURFACE)/test/record_refs_test.cpp
	$(OUT)/record_refs_test
	$(or $(HOST_CXX),c++) -std=c++17 -O1 -g -Wall -Werror -fsanitize=undefined -fno-sanitize-recover=all -I$(SURFACE) -o $(OUT)/record_geometry_test $(SURFACE)/test/record_geometry_test.cpp
	$(OUT)/record_geometry_test
