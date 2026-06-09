#include "jitter_buffer.h"
#ifdef __ANDROID__
#include <android/log.h>
#endif

#define LOG_TAG "JitterBuffer"
#ifdef __ANDROID__
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#else
#define LOGI(...)
#endif

namespace soundswarm {

JitterBuffer::JitterBuffer(int sampleRate, int channels)
    : sampleRate_(sampleRate), channels_(channels) {
    LOGI("JitterBuffer initialized: %d Hz, %d channels", sampleRate, channels);
}

void JitterBuffer::push(const float* pcm, size_t numFrames, int64_t /*captureTimestampUs*/, 
                        int64_t /*localRecvTimestampUs*/, int64_t /*currentClockOffsetUs*/) {
    std::lock_guard<std::mutex> lock(mutex_);

    AudioFrame frame;
    frame.pcm.assign(pcm, pcm + (numFrames * channels_));
    frames_.push_back(std::move(frame));
}

size_t JitterBuffer::pull(float* outPcm, size_t numFrames, 
                          int64_t /*localPlayoutTimestampUs*/, int64_t /*currentClockOffsetUs*/) {
    std::lock_guard<std::mutex> lock(mutex_);

    size_t samplesNeeded = numFrames * channels_;
    size_t outOffset = 0;

    while (outOffset < samplesNeeded && !frames_.empty()) {
        AudioFrame& f = frames_.front();
        size_t availableSamples = f.pcm.size() - frameReadOffset_;
        size_t toCopy = std::min(availableSamples, samplesNeeded - outOffset);

        std::copy(f.pcm.begin() + frameReadOffset_, 
                  f.pcm.begin() + frameReadOffset_ + toCopy, 
                  outPcm + outOffset);

        outOffset += toCopy;
        frameReadOffset_ += toCopy;

        if (frameReadOffset_ >= f.pcm.size()) {
            frames_.pop_front();
            frameReadOffset_ = 0;
        }
    }

    if (outOffset < samplesNeeded) {
        std::fill(outPcm + outOffset, outPcm + samplesNeeded, 0.0f);
    }

    return numFrames;
}

void JitterBuffer::setGlobalDelayMs(double /*delayMs*/) {
}

double JitterBuffer::getP95JitterMs() const {
    return 0.0;
}

} // namespace soundswarm
