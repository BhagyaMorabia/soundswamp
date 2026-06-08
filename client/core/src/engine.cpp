#include "engine.h"
#include <iostream>
#include <chrono>

namespace soundswarm {

Engine::Engine(const EngineConfig& config) 
    : config_(config), isConnected_(false), running_(false) {
    
    network_ = std::make_unique<NetworkClient>();
    clockSync_ = std::make_unique<ClockSync>();
    jitterBuffer_ = std::make_unique<JitterBuffer>(config.sampleRate, config.channels);
    decoder_ = std::make_unique<Decoder>(config.sampleRate, config.channels);

    network_->setTCPMessageCallback([this](const std::string& msg) { handleTCPMessage(msg); });
    network_->setUDPPacketCallback([this](const uint8_t* data, size_t len) { handleUDPPacket(data, len); });
}

Engine::~Engine() {
    stop();
}

bool Engine::start() {
    if (isConnected_) return true;

    // 1. Start UDP receiver on any available port
    if (!network_->startUDP(0)) {
        if (onConnectionStatus_) onConnectionStatus_(false, "Failed to bind UDP socket");
        return false;
    }

    // 2. Connect TCP to Server
    if (!network_->connectTCP(config_.serverIp, config_.tcpPort)) {
        if (onConnectionStatus_) onConnectionStatus_(false, "Failed to connect to server");
        network_->stopUDP();
        return false;
    }

    std::string joinReq = "{\"type\": \"JOIN_REQUEST\", \"token\": \"" + config_.token + 
                          "\", \"device_name\": \"" + config_.deviceName + 
                          "\", \"platform\": \"" + config_.platform + "\"" +
                          ", \"udp_port\": " + std::to_string(network_->getBoundUDPPort()) + "}";
    
    if (!network_->sendTCPMessage(joinReq)) {
        if (onConnectionStatus_) onConnectionStatus_(false, "Failed to send join request");
        stop();
        return false;
    }

    running_ = true;
    maintenanceThread_ = std::thread(&Engine::maintenanceLoop, this);

    return true;
}

void Engine::stop() {
    running_ = false;
    if (maintenanceThread_.joinable()) {
        maintenanceThread_.join();
    }
    
    if (isConnected_ && network_) {
        network_->disconnectTCP();
        network_->stopUDP();
    }
    isConnected_ = false;
}

void Engine::readAudio(float* outPcm, size_t numFrames, int64_t playoutTimestampUs) {
    if (!isConnected_) {
        std::fill(outPcm, outPcm + (numFrames * config_.channels), 0.0f);
        return;
    }

    // Pull from the jitter buffer. It will handle the clock offset and target latency.
    jitterBuffer_->pull(outPcm, numFrames, playoutTimestampUs, clockSync_->getOffsetUs());
}

void Engine::setConnectionCallback(std::function<void(bool, const std::string&)> cb) {
    onConnectionStatus_ = std::move(cb);
}

void Engine::setJitterUpdateCallback(std::function<void(double)> cb) {
    onJitterUpdate_ = std::move(cb);
}

void Engine::handleTCPMessage(const std::string& jsonMsg) {
    std::string msgType = extractJsonString(jsonMsg, "\"type\"");
    
    if (msgType == "JOIN_ACCEPT") {
        clientId_ = extractJsonString(jsonMsg, "\"client_id\"");
        isConnected_ = true;
        if (onConnectionStatus_) onConnectionStatus_(true, "");
        
        // Send initial UDP packet so the server learns our UDP port
        std::vector<uint8_t> dummy(UDP_HEADER_SIZE);
        dummy[0] = PROTOCOL_VERSION;
        dummy[1] = static_cast<uint8_t>(PacketType::ClockSyncReply); // innocuous type
        network_->sendUDPPacket(config_.serverIp, config_.udpPort, dummy);

    } else if (msgType == "JOIN_REJECT") {
        std::string reason = extractJsonString(jsonMsg, "\"reason\"");
        if (onConnectionStatus_) onConnectionStatus_(false, reason);
        stop();

    } else if (msgType == "CLOCK_SYNC_PROBE") {
        int64_t clientRecvTs = clockSync_->getLocalTimeUs();
        int64_t serverSendTs = extractJsonInt(jsonMsg, "\"server_send_ts\"");
        
        int64_t clientSendTs = clockSync_->getLocalTimeUs();
        
        std::string reply = "{\"type\": \"CLOCK_SYNC_REPLY\", \"server_send_ts\": " + std::to_string(serverSendTs) + 
                            ", \"client_recv_ts\": " + std::to_string(clientRecvTs) + 
                            ", \"client_send_ts\": " + std::to_string(clientSendTs) + "}";
        network_->sendTCPMessage(reply);

    } else if (msgType == "CLOCK_OFFSET") {
        int64_t offset = extractJsonInt(jsonMsg, "\"offset_us\"");
        clockSync_->setOffset(offset);

    } else if (msgType == "SET_GLOBAL_LATENCY") {
        int64_t targetMs = extractJsonInt(jsonMsg, "\"target_ms\"");
        jitterBuffer_->setGlobalDelayMs(static_cast<double>(targetMs));
    }
}

void Engine::handleUDPPacket(const uint8_t* data, size_t length) {
    UDPPacket pkt;
    if (!UDPPacket::deserialize(data, length, pkt)) return;

    if (pkt.type == PacketType::Audio) {
        int64_t localRecvTs = clockSync_->getLocalTimeUs();
        
        std::vector<float> pcm;
        if (decoder_->decode(pkt.payload.data(), pkt.payload.size(), pcm)) {
            jitterBuffer_->push(pcm.data(), pcm.size() / config_.channels, 
                                pkt.timestampUs, localRecvTs, clockSync_->getOffsetUs());
        }
    } else if (pkt.type == PacketType::ClockSyncProbe) {
        // Fast UDP clock sync reply
        UDPPacket reply;
        reply.version = PROTOCOL_VERSION;
        reply.type = PacketType::ClockSyncReply;
        reply.seqNum = 0;
        reply.timestampUs = clockSync_->getLocalTimeUs();
        reply.channelMask = ChannelMask::StereoMix;
        
        auto rawReply = reply.serialize();
        network_->sendUDPPacket(config_.serverIp, config_.udpPort, rawReply);
    }
}

void Engine::maintenanceLoop() {
    int loops = 0;
    while (running_) {
        std::this_thread::sleep_for(std::chrono::seconds(1));
        if (!isConnected_) continue;
        loops++;

        // Every 3 seconds: Heartbeat
        if (loops % 3 == 0) {
            network_->sendTCPMessage("{\"type\": \"HEARTBEAT\"}");
        }

        // Every 5 seconds: Jitter Report
        if (loops % 5 == 0) {
            double p95 = jitterBuffer_->getP95JitterMs();
            network_->sendTCPMessage("{\"type\": \"JITTER_REPORT\", \"p95_ms\": " + std::to_string(p95) + "}");
            
            if (onJitterUpdate_) onJitterUpdate_(p95);
        }
    }
}

// Very basic JSON extractors to avoid large dependencies
std::string Engine::extractJsonString(const std::string& json, const std::string& key) {
    size_t pos = json.find(key);
    if (pos == std::string::npos) return "";
    
    // Find the colon after the key
    pos = json.find(":", pos + key.length());
    if (pos == std::string::npos) return "";
    
    // Find the opening quote of the string value
    size_t startQuote = json.find("\"", pos + 1);
    if (startQuote == std::string::npos) return "";
    
    // Find the closing quote of the string value
    size_t endQuote = json.find("\"", startQuote + 1);
    if (endQuote == std::string::npos) return "";
    
    return json.substr(startQuote + 1, endQuote - startQuote - 1);
}

int64_t Engine::extractJsonInt(const std::string& json, const std::string& key) {
    size_t pos = json.find(key);
    if (pos == std::string::npos) return 0;
    pos = json.find(":", pos + key.length());
    if (pos == std::string::npos) return 0;
    
    size_t i = pos + 1;
    while (i < json.length() && (json[i] == ' ' || json[i] == '\t')) i++;
    
    int64_t val = 0;
    bool negative = false;
    if (i < json.length() && json[i] == '-') {
        negative = true;
        i++;
    }
    
    while (i < json.length() && json[i] >= '0' && json[i] <= '9') {
        val = val * 10 + (json[i] - '0');
        i++;
    }
    
    return negative ? -val : val;
}

} // namespace soundswarm
