#pragma once

#include <vector>
#include <cstdint>
#include <string>

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

    // Decodes an Opus packet into interleaved PCM float32.
    // Returns true on success, false on error.
    bool decode(const uint8_t* opusData, size_t length, std::vector<float>& outPcm);

    // Decodes a missing packet to trigger Packet Loss Concealment (PLC).
    bool decodeMissing(std::vector<float>& outPcm);

    int getFrameSamples() const;

private:
    OpusDecoder* decoder_;
    int sampleRate_;
    int channels_;
    int maxFrameSamples_; 
};

} // namespace soundswarm
