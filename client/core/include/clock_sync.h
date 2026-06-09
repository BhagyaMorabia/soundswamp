#pragma once

#include <cstdint>
#include <atomic>
#include <chrono>

namespace soundswarm {

// ClockSync maintains the offset between the phone's local monotonic clock
// and the server's clock. All reads and writes are lock-free using atomics,
// making it safe to call getOffsetUs() from the hardware audio thread without
// any risk of priority inversion or blocking.
class ClockSync {
public:
    ClockSync();

    // Returns the current local monotonic time in microseconds since boot.
    // Uses std::chrono::steady_clock, which maps to CLOCK_MONOTONIC on Android/Linux.
    // This matches the epoch used by Oboe's getTimestamp(CLOCK_MONOTONIC).
    static int64_t getLocalTimeUs();

    // Sets the clock offset received from the server (initial handshake or correction).
    // offsetUs = ServerTime - LocalTime.
    // Uses memory_order_release so the network thread's write is visible to the
    // audio thread immediately without a mutex.
    void setOffset(int64_t offsetUs);

    // Returns the current estimated server time in microseconds.
    int64_t getServerTimeUs() const;

    // Returns the current applied clock offset.
    // Uses memory_order_acquire — safe to call from the audio thread.
    int64_t getOffsetUs() const;

private:
    std::atomic<int64_t> offsetUs_;
};

} // namespace soundswarm
