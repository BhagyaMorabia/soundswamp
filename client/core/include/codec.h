#pragma once

#include <vector>
#include <cstdint>
#include <string>
#include "protocol.h"

struct OpusDecoder;

namespace soundswarm {

// Decoder wraps libopus for decoding audio frames.
class Decoder {
public:
    Decoder(int sampleRate, int channels);
    ~Decoder();

    // Prevent copy
    Decoder(const Decoder&) = delete;
    Decoder& operator=(const Decoder&) = delete;

    // Decodes an audio packet into interleaved PCM float32.
    // Returns the number of decoded samples per channel, or < 0 on error.
    int decode(const uint8_t* opusData, size_t length, CodecFlag flag, std::vector<float>& outPcm);

    // Decodes a missing packet to trigger Packet Loss Concealment (PLC).
    // Returns the number of decoded samples per channel, or < 0 on error.
    int decodeMissing(std::vector<float>& outPcm);

    int getFrameSamples() const;

private:
    OpusDecoder* decoder_;
    int sampleRate_;
    int channels_;
    int maxFrameSamples_;
    CodecFlag lastCodecFlag_ = CodecFlag::Opus;
};

} // namespace soundswarm
