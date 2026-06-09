#include "audio_player.h"
#include <android/log.h>
#include "clock_sync.h"

#define LOG_TAG "SoundSwarm_AudioPlayer"
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)

namespace soundswarm {
namespace android {

AudioPlayer::AudioPlayer(std::shared_ptr<soundswarm::Engine> engine) : engine_(engine) {
}

AudioPlayer::~AudioPlayer() {
    stop();
}

bool AudioPlayer::start() {
    oboe::AudioStreamBuilder builder;
    builder.setDirection(oboe::Direction::Output)
           ->setPerformanceMode(oboe::PerformanceMode::LowLatency)
           ->setSharingMode(oboe::SharingMode::Exclusive)
           ->setFormat(oboe::AudioFormat::Float)
           ->setChannelCount(2)
           ->setSampleRate(48000)
           ->setDataCallback(this)
           ->setErrorCallback(this);

    oboe::Result result = builder.openStream(stream_);
    if (result != oboe::Result::OK) {
        LOGE("Failed to open Oboe stream: %s", oboe::convertToText(result));
        return false;
    }

    // F5 fix: 4 bursts (≈10–15ms) instead of 2 (≈4ms).
    // The jitter buffer provides mathematical synchronization so a larger hardware
    // buffer does not affect global latency — it only prevents underruns from
    // Android background CPU spikes (email sync, GC, etc.).
    stream_->setBufferSizeInFrames(stream_->getFramesPerBurst() * 4);

    result = stream_->requestStart();
    if (result != oboe::Result::OK) {
        LOGE("Failed to start Oboe stream: %s", oboe::convertToText(result));
        stream_->close();
        return false;
    }

    LOGI("Oboe stream started successfully. Burst size: %d", stream_->getFramesPerBurst());
    return true;
}

void AudioPlayer::stop() {
    if (stream_) {
        stream_->requestStop();
        stream_->close();
        stream_.reset();
    }
}

oboe::DataCallbackResult AudioPlayer::onAudioReady(oboe::AudioStream *oboeStream, void *audioData, int32_t numFrames) {
    float *floatData = static_cast<float *>(audioData);

    // Calculate physical playout timestamp
    // We get the timestamp of the frame that is currently at the DAC,
    // and extrapolate it to find out when the *first frame of this buffer* will hit the DAC.
    
    int64_t framePosition = 0;
    int64_t timeNanoseconds = 0;
    int64_t playoutTimestampUs = 0;
    
    auto result = oboeStream->getTimestamp(CLOCK_MONOTONIC, &framePosition, &timeNanoseconds);
    if (result == oboe::Result::OK) {
        // framePosition is the frame index that hit the DAC at timeNanoseconds
        // We want the time when oboeStream->getFramesWritten() will hit the DAC
        int64_t framesAhead = oboeStream->getFramesWritten() - framePosition;
        int64_t timeAheadNs = (framesAhead * 1000000000LL) / oboeStream->getSampleRate();
        int64_t targetTimeNs = timeNanoseconds + timeAheadNs;
        
        // Convert monotonic nanoseconds to microseconds (matching ClockSync::getLocalTimeUs)
        playoutTimestampUs = targetTimeNs / 1000LL;
    } else {
        // Fallback if timestamp query fails: estimate based on current time + buffer size
        int64_t latencyFrames = oboeStream->getBufferSizeInFrames();
        int64_t latencyUs = (latencyFrames * 1000000LL) / oboeStream->getSampleRate();
        playoutTimestampUs = ClockSync::getLocalTimeUs() + latencyUs;
    }

    // Pull synchronized audio from the core engine
    engine_->readAudio(floatData, numFrames, playoutTimestampUs);

    return oboe::DataCallbackResult::Continue;
}

void AudioPlayer::onErrorAfterClose(oboe::AudioStream *oboeStream, oboe::Result error) {
    LOGE("Oboe stream closed due to error: %s — attempting restart",
         oboe::convertToText(error));

    // F17 fix: Restart the stream after device changes (headphone plug/unplug,
    // Bluetooth connect/disconnect, etc.). Without this, audio stops permanently.
    //
    // Oboe recommends restarting from a separate thread to avoid deadlock inside
    // the error callback. We use a detached thread for simplicity; a production
    // app would use a more robust restart manager.
    std::thread restartThread([this]() {
        // Brief pause to let the OS settle after a device change.
        std::this_thread::sleep_for(std::chrono::milliseconds(200));
        stop();
        if (start()) {
            LOGI("Oboe stream restarted successfully after error.");
        } else {
            LOGE("Oboe stream restart failed.");
        }
    });
    restartThread.detach();
}

} // namespace android
} // namespace soundswarm
