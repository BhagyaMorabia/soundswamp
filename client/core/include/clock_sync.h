#pragma once

#include <cstdint>
#include <mutex>
#include <chrono>

namespace soundswarm {

class ClockSync {
public:
    ClockSync();

    // Returns the current local monotonic time in microseconds.
    // This is used for generating ClientRecvTs and ClientSendTs in sync replies,
    // and for calculating transit times locally.
    static int64_t getLocalTimeUs();

    // Sets the clock offset received from the server (either initial or correction).
    // offsetUs = ServerTime - LocalTime
    void setOffset(int64_t offsetUs);

    // Returns the current estimated server time in microseconds.
    // Equivalent to getLocalTimeUs() + getOffsetUs().
    int64_t getServerTimeUs() const;

    // Returns the current applied clock offset.
    int64_t getOffsetUs() const;

private:
    int64_t offsetUs_;
    mutable std::mutex mutex_;
};

} // namespace soundswarm
