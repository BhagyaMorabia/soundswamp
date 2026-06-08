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

namespace soundswarm {

struct EngineConfig {
    std::string serverIp;
    int tcpPort;
    int udpPort;
    std::string token;
    std::string deviceName;
    std::string platform; // "android" or "ios"
    int sampleRate = 48000;
    int channels = 2;
};

class Engine {
public:
    Engine(const EngineConfig& config);
    ~Engine();

    // Connects to the server, completes handshake and clock sync
    bool start();

    // Disconnects from the server
    void stop();

    // The platform audio callback calls this to get synchronized audio frames.
    // It passes its local high-res timestamp indicating when the audio will physically play.
    void readAudio(float* outPcm, size_t numFrames, int64_t playoutTimestampUs);

    // Callbacks for UI state updates
    void setConnectionCallback(std::function<void(bool connected, const std::string& errorMsg)> cb);
    void setJitterUpdateCallback(std::function<void(double p95Ms)> cb);

private:
    void handleTCPMessage(const std::string& jsonMsg);
    void handleUDPPacket(const uint8_t* data, size_t length);

    // Send the periodic jitter report back to the server
    void sendJitterReport();

    EngineConfig config_;
    
    std::unique_ptr<NetworkClient> network_;
    std::unique_ptr<ClockSync> clockSync_;
    std::unique_ptr<JitterBuffer> jitterBuffer_;
    std::unique_ptr<Decoder> decoder_;

    std::string clientId_;
    std::atomic<bool> isConnected_;
    
    std::function<void(bool, const std::string&)> onConnectionStatus_;
    std::function<void(double)> onJitterUpdate_;

    // Periodic heartbeat/jitter thread
    std::atomic<bool> running_;
    std::thread maintenanceThread_;
    void maintenanceLoop();

    // Simple JSON value extractor (since standard C++ lacks JSON)
    static std::string extractJsonString(const std::string& json, const std::string& key);
    static int64_t extractJsonInt(const std::string& json, const std::string& key);
};

} // namespace soundswarm
