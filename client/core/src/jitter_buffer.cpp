#include "jitter_buffer.h"
#include <cmath>

namespace soundswarm {

constexpr size_t MAX_TRANSIT_WINDOW = 500; // 5 seconds at 10ms per packet
constexpr int64_t MAX_BUFFER_US = 2000000; // 2 seconds max buffering

JitterBuffer::JitterBuffer(int sampleRate, int channels)
    : sampleRate_(sampleRate), channels_(channels), globalDelayUs_(50000), frameReadOffset_(0) {
}

void JitterBuffer::push(const float* pcm, size_t numFrames, int64_t captureTimestampUs, 
                        int64_t localRecvTimestampUs, int64_t currentClockOffsetUs) {
    std::lock_guard<std::mutex> lock(mutex_);

    // 1. Calculate transit time for jitter tracking
    int64_t serverRecvTimeUs = localRecvTimestampUs - currentClockOffsetUs;
    int64_t transitUs = serverRecvTimeUs - captureTimestampUs;
    if (transitUs < 0) transitUs = 0; // Clock skew anomaly
    
    recordTransitTime(transitUs / 1000.0);

    // 2. Insert frame in order (handling slight UDP out-of-order)
    AudioFrame frame;
    frame.captureTimestampUs = captureTimestampUs;
    frame.pcm.assign(pcm, pcm + (numFrames * channels_));

    // Fast path: append if it's the newest packet
    if (frames_.empty() || captureTimestampUs > frames_.back().captureTimestampUs) {
        frames_.push_back(std::move(frame));
    } else {
        // Insert sorted
        auto it = std::lower_bound(frames_.begin(), frames_.end(), captureTimestampUs,
            [](const AudioFrame& f, int64_t ts) {
                return f.captureTimestampUs < ts;
            });
        
        // Discard exact duplicates
        if (it != frames_.end() && it->captureTimestampUs == captureTimestampUs) {
            return;
        }
        frames_.insert(it, std::move(frame));
    }

    // Limit maximum buffer size to prevent infinite growth on stall
    while (!frames_.empty()) {
        int64_t bufferSpanUs = frames_.back().captureTimestampUs - frames_.front().captureTimestampUs;
        if (bufferSpanUs > MAX_BUFFER_US) {
            frames_.pop_front();
            frameReadOffset_ = 0;
        } else {
            break;
        }
    }
}

size_t JitterBuffer::pull(float* outPcm, size_t numFrames, 
                          int64_t localPlayoutTimestampUs, int64_t currentClockOffsetUs) {
    std::lock_guard<std::mutex> lock(mutex_);

    int64_t serverPlayoutTimeUs = localPlayoutTimestampUs - currentClockOffsetUs;
    int64_t targetCaptureTimeUs = serverPlayoutTimeUs - globalDelayUs_;

    size_t framesWritten = 0;
    size_t samplesNeeded = numFrames * channels_;

    // Discard frames that are completely too old
    while (!frames_.empty()) {
        int64_t frameEndTsUs = frames_.front().captureTimestampUs + 
            (frames_.front().pcm.size() / channels_ * 1000000LL / sampleRate_);
            
        // If the end of this frame is strictly before our target capture time, drop it
        if (frameEndTsUs <= targetCaptureTimeUs) {
            frames_.pop_front();
            frameReadOffset_ = 0;
        } else {
            break;
        }
    }

    // Fast-forward inside the first frame if target is within it
    if (!frames_.empty()) {
        int64_t frameStartTsUs = frames_.front().captureTimestampUs;
        if (targetCaptureTimeUs > frameStartTsUs) {
            int64_t diffUs = targetCaptureTimeUs - frameStartTsUs;
            size_t skipFrames = (diffUs * sampleRate_) / 1000000;
            size_t skipSamples = skipFrames * channels_;
            
            if (skipSamples > frames_.front().pcm.size()) {
                skipSamples = frames_.front().pcm.size();
            }

            if (skipSamples > frameReadOffset_) {
                frameReadOffset_ = skipSamples;
            }
        }
    }

    // Fill the output buffer
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
        framesWritten += toCopy / channels_;

        if (frameReadOffset_ >= f.pcm.size()) {
            frames_.pop_front();
            frameReadOffset_ = 0;
        }
    }

    // Fill remainder with zeros (underrun)
    if (outOffset < samplesNeeded) {
        std::fill(outPcm + outOffset, outPcm + samplesNeeded, 0.0f);
        // Note: we don't count these as framesWritten to indicate underrun if needed,
        // but for safety we'll return numFrames and just output silence.
        framesWritten = numFrames; 
    }

    return framesWritten;
}

void JitterBuffer::setGlobalDelayMs(double delayMs) {
    std::lock_guard<std::mutex> lock(mutex_);
    globalDelayUs_ = static_cast<int64_t>(delayMs * 1000.0);
}

void JitterBuffer::recordTransitTime(double transitMs) {
    transitTimesMs_.push_back(transitMs);
    if (transitTimesMs_.size() > MAX_TRANSIT_WINDOW) {
        transitTimesMs_.pop_front();
    }
}

double JitterBuffer::getP95JitterMs() const {
    std::lock_guard<std::mutex> lock(mutex_);
    if (transitTimesMs_.empty()) return 0.0;

    std::vector<double> sorted(transitTimesMs_.begin(), transitTimesMs_.end());
    std::sort(sorted.begin(), sorted.end());

    size_t idx = static_cast<size_t>(std::ceil(sorted.size() * 0.95)) - 1;
    if (idx >= sorted.size()) idx = sorted.size() - 1;

    return sorted[idx];
}

} // namespace soundswarm
