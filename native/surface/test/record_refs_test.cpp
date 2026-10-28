#include <assert.h>
#include <stdio.h>

#include "android_refs.h"

class PlatformRefBase {
public:
	virtual ~PlatformRefBase() = default;
	void *refs = nullptr;
};

class PlatformBinder : public virtual PlatformRefBase {
public:
	virtual void transact() {}
};

class PlatformInterface : public virtual PlatformRefBase {
public:
	virtual void as_binder() {}
};

class PlatformProducer : public PlatformInterface {
public:
	virtual void dequeue() {}
};

static const void *expected;
static int references;

void android::RefBase::incStrong(const void *) const {
	assert(this == expected);
	references++;
}

void android::RefBase::decStrong(const void *) const {
	assert(this == expected);
	references--;
	assert(references >= 0);
}

template <typename Declared, typename Platform> static void check() {
	Platform object;
	expected = static_cast<PlatformRefBase *>(&object);
	Declared *declared = reinterpret_cast<Declared *>(&object);
	assert(static_cast<android::RefBase *>(declared) == expected);
	{
		android::sp<Declared> first(declared);
		assert(references == 1);
		{
			android::sp<Declared> copy(first);
			assert(references == 2);
		}
		assert(references == 1);
	}
	assert(references == 0);
}

int main() {
	check<android::IBinder, PlatformBinder>();
	check<android::IGraphicBufferProducer, PlatformProducer>();
	puts("ok");
}
