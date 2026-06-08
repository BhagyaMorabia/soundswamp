#pragma once

#include <vector>
#include <mutex>
#include <deque>
#include <cstdint>
#include <algorithm>

namespace soundswarm {

struct AudioFrame {
    int64_t captureTimestampUs;
    std::vector<float> pcm;
};

// JitterBuffer manages incoming audio frames, calculates network jitter,
// and handles synchronized playout based on a global target delay.
class JitterBuffer {
public:
    JitterBuffer(int sampleRate, int channels);

    // Push decoded PCM frames with their original server capture timestamp.
    // localRecvTimestampUs is the local system time when the packet was received over UDP.
    // currentClockOffsetUs is the current estimated (serverTime - localTime).
    void push(const float* pcm, size_t numFrames, int64_t captureTimestampUs, 
              int64_t localRecvTimestampUs, int64_t currentClockOffsetUs);

    // Pull PCM data for the audio device callback.
    // localPlayoutTimestampUs is the exact local time when these samples will hit the speaker.
    // currentClockOffsetUs is the current estimated (serverTime - localTime).
    size_t pull(float* outPcm, size_t numFrames, 
                int64_t localPlayoutTimestampUs, int64_t currentClockOffsetUs);

    // Update the global playback delay target set by the server.
    void setGlobalDelayMs(double delayMs);

    // Calculate the 95th percentile of network transit times over the recent window.
    double getP95JitterMs() const;

private:
    int sampleRate_;
    int channels_;
    int64_t globalDelayUs_;

    std::deque<AudioFrame> frames_;
    size_t frameReadOffset_; // offset within the first frame's PCM vector
    mutable std::mutex mutex_;

    // Jitter tracking (window of last ~500 packets / 5 seconds)
    std::deque<double> transitTimesMs_;
    void recordTransitTime(double transitMs);
};

} // namespace soundswarm
