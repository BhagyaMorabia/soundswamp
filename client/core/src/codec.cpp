#include "codec.h"
#include <opus.h>
#include <stdexcept>
#include <cstring>


namespace soundswarm {


Decoder::Decoder(int sampleRate, int channels)
    : sampleRate_(sampleRate), channels_(channels) {
    
    // We expect 48kHz for Opus
    if (sampleRate != 48000 && sampleRate != 24000 && sampleRate != 16000 && sampleRate != 12000 && sampleRate != 8000) {
        throw std::invalid_argument("Unsupported sample rate for Opus");
    }
    if (channels != 1 && channels != 2) {
        throw std::invalid_argument("Unsupported channel count for Opus");
    }

    int err = OPUS_OK;
    decoder_ = opus_decoder_create(sampleRate, channels, &err);
    if (err != OPUS_OK || !decoder_) {
        throw std::runtime_error("Failed to create Opus decoder: " + std::to_string(err));
    }

    // Allocate enough space for up to 120ms of audio (Opus max frame size)
    maxFrameSamples_ = (sampleRate * 120) / 1000;
}

Decoder::~Decoder() {
    if (decoder_) {
        opus_decoder_destroy(decoder_);
        decoder_ = nullptr;
    }
}

bool Decoder::decode(const uint8_t* opusData, size_t length, CodecFlag flag, std::vector<float>& outPcm) {
    if (flag == CodecFlag::PCM) {
        int pcmSamples = length / 2;
        outPcm.resize(pcmSamples);
        for (int i = 0; i < pcmSamples; ++i) {
            int16_t sample;
            std::memcpy(&sample, opusData + (i * 2), sizeof(int16_t));
            outPcm[i] = static_cast<float>(sample) / 32768.0f;
        }
        return true;
    }

    outPcm.resize(maxFrameSamples_ * channels_);
    int decodedSamples = opus_decode_float(
        decoder_,
        opusData,
        static_cast<opus_int32>(length),
        outPcm.data(),
        maxFrameSamples_,
        0 // No FEC for now
    );

    if (decodedSamples < 0) {
        // Fallback: The Go server might be running without CGO (opus_cgo tag)
        // In this mode, it sends raw 16-bit little-endian PCM instead of Opus.
        // We accept any length that is a multiple of bytes-per-frame.
        if (length > 0 && length % (channels_ * 2) == 0) {
            int pcmSamples = length / 2; // total float samples across all channels
            outPcm.resize(pcmSamples);
            for (int i = 0; i < pcmSamples; ++i) {
                int16_t sample;
                std::memcpy(&sample, opusData + (i * 2), sizeof(int16_t));
                outPcm[i] = static_cast<float>(sample) / 32768.0f;
            }
            return true;
        }

        return false;
    }

    outPcm.resize(decodedSamples * channels_);
    return true;
}

bool Decoder::decodeMissing(std::vector<float>& outPcm) {
    outPcm.resize(maxFrameSamples_ * channels_);

    // Passing nullptr to opusData triggers Packet Loss Concealment
    // We request a standard 10ms frame size for PLC
    int frameSize = sampleRate_ / 100; 

    int decodedSamples = opus_decode_float(
        decoder_,
        nullptr,
        0,
        outPcm.data(),
        frameSize,
        0
    );

    if (decodedSamples < 0) {
        return false;
    }

    outPcm.resize(decodedSamples * channels_);
    return true;
}

int Decoder::getFrameSamples() const {
    // Standard 10ms frame size
    return sampleRate_ / 100;
}

} // namespace soundswarm
