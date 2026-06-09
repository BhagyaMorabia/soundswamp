#include "jitter_buffer.h"

#include <algorithm>
#include <cstring>
#include <vector>

#ifdef __ANDROID__
#include <android/log.h>
#define LOG_TAG "JitterBuffer"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO,  LOG_TAG, __VA_ARGS__)
#define LOGW(...) __android_log_print(ANDROID_LOG_WARN,  LOG_TAG, __VA_ARGS__)
#define LOGD(...) __android_log_print(ANDROID_LOG_DEBUG, LOG_TAG, __VA_ARGS__)
#else
#define LOGI(...)
#define LOGW(...)
#define LOGD(...)
#endif

namespace soundswarm {

JitterBuffer::JitterBuffer(int sampleRate, int channels)
    : sampleRate_(sampleRate),
      channels_(channels),
      frameReadOffset_(0) {

    // Pre-allocate PCM vectors inside each ring slot so push() never allocates
    // on the hot path after warmup.
    int defaultFrameSamples = (sampleRate / 100) * channels; // 10ms
    for (auto& slot : ring_) {
        slot.pcm.reserve(defaultFrameSamples * 4); // headroom for larger frames
        slot.valid = false;
    }

    LOGI("JitterBuffer initialized: %d Hz, %d ch, ring=%zu slots",
         sampleRate, channels, RING_CAPACITY);
}

// ---------------------------------------------------------------------------
// push() — called by the UDP network thread (single producer)
// ---------------------------------------------------------------------------
void JitterBuffer::push(const float* pcm, size_t numFrames,
                        int64_t captureTimestampUs,
                        int64_t localRecvUs,
                        int64_t clockOffsetUs) {

    size_t head = head_.load(std::memory_order_relaxed);
    size_t tail = tail_.load(std::memory_order_acquire);

    // Check for ring full (producer is one slot behind the consumer's tail).
    if (head - tail >= RING_CAPACITY) {
        // Ring full: drop oldest frame by advancing tail under the producer's control
        // is NOT safe (only consumer may advance tail). Instead, drop this incoming
        // frame and log a warning.
        LOGW("JitterBuffer overflow: dropping frame ts=%lld", (long long)captureTimestampUs);
        return;
    }

    AudioFrame& slot = ring_[head & RING_MASK];
    size_t sampleCount = numFrames * static_cast<size_t>(channels_);
    slot.pcm.resize(sampleCount);
    std::memcpy(slot.pcm.data(), pcm, sampleCount * sizeof(float));
    slot.captureTimestampUs = captureTimestampUs;
    slot.valid = true;

    // Publish the new frame to the consumer with a release fence.
    head_.store(head + 1, std::memory_order_release);

    // --- Transit time measurement (for P95 jitter calculation) ---
    // One-way transit: how long this audio spent in flight from the server
    // capture moment to our local receive moment.
    //
    // serverCaptureTimeInLocalUs = captureTimestampUs - clockOffsetUs
    // (clockOffsetUs = serverTime - localTime, so localTime = serverTime - offset)
    int64_t serverCaptureInLocalUs = captureTimestampUs - clockOffsetUs;
    int64_t transitUs = localRecvUs - serverCaptureInLocalUs;

    // Discard impossible values (negative transit or absurdly large).
    if (transitUs >= 0 && transitUs < 2000000LL) { // cap at 2 seconds
        std::lock_guard<std::mutex> lk(transitMu_);
        transitTimes_.push_back(transitUs);
        if (transitTimes_.size() > MAX_TRANSIT_SAMPLES) {
            transitTimes_.pop_front();
        }
    }
}

// ---------------------------------------------------------------------------
// pull() — called by the Oboe audio thread (single consumer)
// ---------------------------------------------------------------------------
size_t JitterBuffer::pull(float* outPcm, size_t numFrames,
                          int64_t localPlayoutUs,
                          int64_t clockOffsetUs) {

    const size_t samplesNeeded = numFrames * static_cast<size_t>(channels_);

    // Convert local playout time to server time.
    // serverPlayoutUs = localPlayoutUs + clockOffsetUs
    int64_t serverPlayoutUs = localPlayoutUs + clockOffsetUs;

    // The global delay is how much buffering we apply:
    // we play audio whose capture timestamp equals (serverPlayoutUs - globalDelayUs_).
    int64_t targetCaptureUs = serverPlayoutUs - globalDelayUs_.load(std::memory_order_acquire);

    size_t outOffset = 0;

    while (outOffset < samplesNeeded) {
        size_t tail = tail_.load(std::memory_order_relaxed);
        size_t head = head_.load(std::memory_order_acquire);

        if (tail == head) {
            // Buffer underrun — output silence and stop.
            break;
        }

        AudioFrame& frame = ring_[tail & RING_MASK];

        // Discard frames that are too old (more than 2 frames behind the target).
        // 2 frames of leeway (≈20ms at 10ms frame size) prevents excessive discarding.
        int64_t frameDurationUs = static_cast<int64_t>(
            (frame.pcm.size() / channels_) * 1000000LL / sampleRate_);

        if (frame.captureTimestampUs < targetCaptureUs - (frameDurationUs * 2)) {
            // This frame is too old — skip it.
            tail_.store(tail + 1, std::memory_order_release);
            frameReadOffset_ = 0;
            LOGD("pull: dropping stale frame ts=%lld target=%lld",
                 (long long)frame.captureTimestampUs, (long long)targetCaptureUs);
            continue;
        }

        // If the next frame is in the future (buffer is running ahead),
        // we output silence to stay on the playback timeline.
        if (frame.captureTimestampUs > targetCaptureUs + frameDurationUs) {
            // Too far ahead — output silence for now.
            break;
        }

        // Copy samples from this frame.
        size_t available = frame.pcm.size() - frameReadOffset_;
        size_t toCopy    = std::min(available, samplesNeeded - outOffset);

        std::memcpy(outPcm + outOffset,
                    frame.pcm.data() + frameReadOffset_,
                    toCopy * sizeof(float));

        outOffset       += toCopy;
        frameReadOffset_ += toCopy;

        if (frameReadOffset_ >= frame.pcm.size()) {
            // Frame fully consumed — release it to the producer.
            frame.valid = false;
            tail_.store(tail + 1, std::memory_order_release);
            frameReadOffset_ = 0;
        }
    }

    // Fill any remaining output with silence (underrun concealment).
    if (outOffset < samplesNeeded) {
        std::fill(outPcm + outOffset, outPcm + samplesNeeded, 0.0f);
        if (outOffset == 0) {
            LOGD("pull: full underrun (buffer empty or too far ahead)");
        }
    }

    return numFrames;
}

// ---------------------------------------------------------------------------
// setGlobalDelayMs() — called by the TCP message handler
// ---------------------------------------------------------------------------
void JitterBuffer::setGlobalDelayMs(double delayMs) {
    int64_t delayUs = static_cast<int64_t>(delayMs * 1000.0);
    globalDelayUs_.store(delayUs, std::memory_order_release);
    LOGI("JitterBuffer global delay set to %.1f ms (%lld us)", delayMs, (long long)delayUs);
}

// ---------------------------------------------------------------------------
// getP95JitterMs() — called by the maintenance thread
// ---------------------------------------------------------------------------
double JitterBuffer::getP95JitterMs() const {
    std::lock_guard<std::mutex> lk(transitMu_);

    if (transitTimes_.empty()) {
        return 0.0;
    }

    // Copy transit times and sort to find 95th percentile.
    std::vector<int64_t> sorted(transitTimes_.begin(), transitTimes_.end());
    std::sort(sorted.begin(), sorted.end());

    // P95 index: 95% of values are at or below this index.
    size_t idx = static_cast<size_t>(sorted.size() * 0.95);
    if (idx >= sorted.size()) idx = sorted.size() - 1;

    return static_cast<double>(sorted[idx]) / 1000.0; // µs → ms
}

} // namespace soundswarm
