#pragma once

#include "network.h"
#include "protocol.h"
#include "clock_sync.h"
#include "jitter_buffer.h"
#include "codec.h"

#include <memory>
#include <string>
#include <functional>
#include <atomic>
#include <thread>
#include <mutex>

namespace soundswarm {

struct EngineConfig {
    std::string serverIp;
    int tcpPort;
    int udpPort;
    std::string token;
    std::string deviceName;
    std::string platform; // "android" or "ios"
    int sampleRate = 48000;
    int channels   = 2;
};

class Engine {
public:
    Engine(const EngineConfig& config);
    ~Engine();

    // Connects to the server, completes handshake and clock sync.
    bool start();

    // Disconnects from the server.
    void stop();

    // The platform audio callback calls this to get synchronized audio frames.
    // Returns silence until the full TCP+clock-sync handshake is complete.
    void readAudio(float* outPcm, size_t numFrames, int64_t playoutTimestampUs);

    // Callbacks for UI state updates.
    void setConnectionCallback(std::function<void(bool connected, const std::string& errorMsg)> cb);
    void setJitterUpdateCallback(std::function<void(double p95Ms)> cb);

private:
    void handleTCPMessage(const std::string& jsonMsg);
    void handleUDPPacket(const uint8_t* data, size_t length);

    EngineConfig config_;

    std::unique_ptr<INetworkClient> network_;
    std::unique_ptr<ClockSync>     clockSync_;
    std::unique_ptr<JitterBuffer>  jitterBuffer_;
    std::unique_ptr<Decoder>       decoder_;

    std::string clientId_;
    uint32_t lastSeqNum_;

    // isConnected_: true after JOIN_ACCEPT (TCP authenticated).
    std::atomic<bool> isConnected_;

    // handshakeComplete_: true after the initial CLOCK_OFFSET message is received.
    // The audio thread gates on this to avoid playing audio with offsetUs=0 (F11 fix).
    std::atomic<bool> handshakeComplete_;

    // Pre-allocated buffer for zero-allocation audio decoding on the hot path
    std::vector<float> decodeBuffer_;

    std::function<void(bool, const std::string&)> onConnectionStatus_;
    std::function<void(double)>                    onJitterUpdate_;

    // Periodic heartbeat/jitter maintenance thread.
    std::atomic<bool> running_;
    std::thread       maintenanceThread_;
    std::mutex        clientMu_;
    void maintenanceLoop();
};

} // namespace soundswarm
