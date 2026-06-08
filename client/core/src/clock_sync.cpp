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
    std::lock_guard<std::mutex> lock(mutex_);
    offsetUs_ = offsetUs;
}

int64_t ClockSync::getServerTimeUs() const {
    return getLocalTimeUs() + getOffsetUs();
}

int64_t ClockSync::getOffsetUs() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return offsetUs_;
}

} // namespace soundswarm
