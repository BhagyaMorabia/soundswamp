#pragma once

#include <vector>
#include <array>
#include <deque>
#include <atomic>
#include <mutex>
#include <cstdint>
#include <algorithm>

namespace soundswarm {

// AudioFrame holds a single decoded PCM frame with its server-side capture timestamp.
struct AudioFrame {
    std::vector<float> pcm;          // interleaved float32 samples
    int64_t captureTimestampUs = 0;  // server microsecond capture time
    bool valid = false;
};

// JitterBuffer — lock-free SPSC ring between the UDP network thread (push)
// and the hardware audio thread (pull).
//
// Threading model:
//   push()           — called only by the UDP receiver thread (single producer)
//   pull()           — called only by the Oboe audio callback thread (single consumer)
//   setGlobalDelayMs() — called by the TCP message handler (safe: single atomic store)
//   getP95JitterMs() — called by the maintenance thread (reads transit time window)
//
// The ring uses a power-of-two capacity with atomic head/tail indices.
// The producer writes to head, the consumer reads from tail. No mutex needed
// between producer and consumer.
//
// A separate lightweight mutex (transitMu_) protects the transit time measurement
// window. It is touched only by push() (writer) and getP95JitterMs() (reporter),
// never by the audio thread, so it cannot cause priority inversion.
class JitterBuffer {
public:
    // Ring capacity: 128 frames × 10ms = 1.28 seconds of buffer space.
    // Must be a power of two for the fast modulo trick.
    static constexpr size_t RING_CAPACITY = 128;
    static constexpr size_t RING_MASK     = RING_CAPACITY - 1;

    // Maximum number of transit-time samples kept for P95 calculation.
    static constexpr size_t MAX_TRANSIT_SAMPLES = 200;

    JitterBuffer(int sampleRate, int channels);

    // Push decoded PCM frames with their original server capture timestamp.
    //   pcm                — pointer to interleaved float32 samples
    //   numFrames          — number of audio frames (samples / channels)
    //   captureTimestampUs — server time when this audio was captured (µs)
    //   localRecvUs        — local steady_clock time when packet was received (µs)
    //   clockOffsetUs      — current (serverTime - localTime) offset (µs)
    // Called from the UDP receiver thread (single producer).
    void push(const float* pcm, size_t numFrames,
              int64_t captureTimestampUs,
              int64_t localRecvUs,
              int64_t clockOffsetUs);

    // Pull PCM data for the audio device callback.
    //   outPcm             — output buffer to fill (interleaved float32)
    //   numFrames          — number of frames requested
    //   localPlayoutUs     — local steady_clock time when this audio will hit DAC (µs)
    //   clockOffsetUs      — current (serverTime - localTime) offset (µs)
    // Returns number of frames written (always == numFrames; silence if underrun).
    // Called from the Oboe audio thread (single consumer).
    size_t pull(float* outPcm, size_t numFrames,
                int64_t localPlayoutUs,
                int64_t clockOffsetUs);

    // Update the global playback delay target broadcast by the server.
    // Thread-safe: uses an atomic store.
    void setGlobalDelayMs(double delayMs);

    // Returns the 95th-percentile transit time in milliseconds, computed from
    // the most recent MAX_TRANSIT_SAMPLES measurements.
    // Called from the maintenance thread.
    double getP95JitterMs() const;

private:
    int sampleRate_;
    int channels_;

    // --- SPSC ring buffer ---
    // head_ is written only by the producer (push).
    // tail_ is written only by the consumer (pull).
    // Both are read by the other side with appropriate memory ordering.
    std::array<AudioFrame, RING_CAPACITY> ring_;
    alignas(64) std::atomic<size_t> head_{0}; // producer writes here
    alignas(64) std::atomic<size_t> tail_{0}; // consumer reads here

    // Partial-frame read offset within the current head frame.
    size_t frameReadOffset_{0};

    // --- Playback target ---
    // globalDelayUs_ is the server-mandated target buffering delay.
    // Written by TCP handler, read by audio thread (atomic).
    std::atomic<int64_t> globalDelayUs_{30000}; // default 30ms

    // --- P95 transit time measurement ---
    // transitTimes_ is a sliding window of one-way transit times in µs.
    // Protected by transitMu_ (only touched by push + getP95JitterMs,
    // never by the audio thread).
    mutable std::mutex transitMu_;
    std::deque<int64_t> transitTimes_;
};

} // namespace soundswarm
