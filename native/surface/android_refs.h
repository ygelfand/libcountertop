#ifndef COUNTERTOP_ANDROID_REFS_H
#define COUNTERTOP_ANDROID_REFS_H

#include <stdint.h>

namespace android {
class RefBase {
public:
	void incStrong(const void *) const;
	void decStrong(const void *) const;
protected:
	RefBase();
	virtual ~RefBase();
	virtual void onFirstRef();
	virtual void onLastStrongRef(const void *);
	virtual bool onIncStrongAttempted(uint32_t, const void *);
	virtual void onLastWeakRef(const void *);
private:
	class weakref_impl;
	weakref_impl *const mRefs;
};
static_assert(sizeof(RefBase) == 2 * sizeof(void *));
class IBinder : public virtual RefBase {};
class IGraphicBufferProducer : public virtual RefBase {};
template <typename T> class sp {
public:
	T *pointer;
	explicit sp(T *p) : pointer(p) { if (pointer) pointer->incStrong(this); }
	sp(const sp &other) : sp(other.pointer) {}
	~sp() { if (pointer) pointer->decStrong(this); }
};
}

#endif
