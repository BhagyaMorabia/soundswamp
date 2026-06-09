#include "clock_sync.h"

namespace soundswarm {

ClockSync::ClockSync() : offsetUs_(0) {
}

int64_t ClockSync::getLocalTimeUs() {
    auto now = std::chrono::steady_clock::now();
    auto duration = now.time_since_epoch();
    return std::chrono::duration_cast<std::chrono::microseconds>(duration).count();
}

void ClockSync::setOffset(int64_t offsetUs) {
    // memory_order_release: all prior writes in the network thread are visible
    // to any thread that subsequently loads with memory_order_acquire.
    offsetUs_.store(offsetUs, std::memory_order_release);
}

int64_t ClockSync::getOffsetUs() const {
    // memory_order_acquire: safe to call from the audio thread.
    // No mutex, no blocking, no priority inversion.
    return offsetUs_.load(std::memory_order_acquire);
}

int64_t ClockSync::getServerTimeUs() const {
    return getLocalTimeUs() + getOffsetUs();
}

} // namespace soundswarm
