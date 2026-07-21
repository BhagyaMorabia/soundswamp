#pragma once

#include <oboe/Oboe.h>
#include <memory>
#include <thread>
#include <chrono>
#include "engine.h"

namespace soundswarm {
namespace android {

class AudioPlayer : public oboe::AudioStreamDataCallback, public oboe::AudioStreamErrorCallback {
public:
    AudioPlayer(std::shared_ptr<soundswarm::Engine> engine);
    ~AudioPlayer();

    bool start();
    void stop();

    // oboe::AudioStreamDataCallback
    oboe::DataCallbackResult onAudioReady(oboe::AudioStream *oboeStream, void *audioData, int32_t numFrames) override;

    // oboe::AudioStreamErrorCallback
    void onErrorAfterClose(oboe::AudioStream *oboeStream, oboe::Result error) override;

private:
    std::shared_ptr<soundswarm::Engine> engine_;
    std::shared_ptr<oboe::AudioStream> stream_;
    std::atomic<bool> isRestarting_{false};
    std::mutex restartMu_;
};

} // namespace android
} // namespace soundswarm
