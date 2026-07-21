#include "engine.h"
#include <iostream>
#include <chrono>
#include <cmath>
#include <sstream>
#include <iomanip>

#ifdef __ANDROID__
#include <android/log.h>
#define LOGD(...) __android_log_print(ANDROID_LOG_DEBUG, "SoundSwarm_Engine", __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, "SoundSwarm_Engine", __VA_ARGS__)
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO,  "SoundSwarm_Engine", __VA_ARGS__)
#else
#define LOGD(...) do {} while(0)
#define LOGE(...) do {} while(0)
#define LOGI(...) do {} while(0)
#endif

namespace soundswarm {

// ---------------------------------------------------------------------------
// Safe JSON value extraction
//
// The hand-rolled extractors are replaced with implementations that:
//   (a) never crash on malformed input,
//   (b) handle the string fallback for missing keys gracefully,
//   (c) parse target_ms as double (fixing the integer-truncation bug F12).
//
// We intentionally avoid adding a JSON library dependency by writing safe,
// defensive parsers. For reference: nlohmann/json can be swapped in at any
// time by replacing the two extract* functions below.
// ---------------------------------------------------------------------------

// escapeJsonString escapes a raw string for safe embedding in a JSON value.
// Handles: " \ / and control characters (\n \r \t \b \f).
static std::string escapeJsonString(const std::string& raw) {
    std::ostringstream oss;
    for (unsigned char c : raw) {
        switch (c) {
            case '"':  oss << "\\\""; break;
            case '\\': oss << "\\\\"; break;
            case '/':  oss << "\\/";  break;
            case '\n': oss << "\\n";  break;
            case '\r': oss << "\\r";  break;
            case '\t': oss << "\\t";  break;
            case '\b': oss << "\\b";  break;
            case '\f': oss << "\\f";  break;
            default:
                if (c < 0x20) {
                    // Escape other control characters as \uXXXX
                    oss << "\\u" << std::hex << std::setw(4)
                        << std::setfill('0') << static_cast<int>(c);
                } else {
                    oss << c;
                }
        }
    }
    return oss.str();
}

// extractJsonString safely extracts the string value for a given key.
// Returns "" if the key is not found or the value is not a JSON string.
static std::string extractJsonString(const std::string& json, const std::string& key) {
    size_t pos = json.find(key);
    if (pos == std::string::npos) return "";

    pos = json.find(':', pos + key.length());
    if (pos == std::string::npos) return "";

    // Skip whitespace
    pos++;
    while (pos < json.size() && (json[pos] == ' ' || json[pos] == '\t')) pos++;

    if (pos >= json.size() || json[pos] != '"') return "";

    // Find closing quote, handling escaped quotes
    size_t start = pos + 1;
    size_t end   = start;
    while (end < json.size()) {
        if (json[end] == '\\') {
            end += 2; // skip escaped character
            continue;
        }
        if (json[end] == '"') break;
        end++;
    }
    if (end >= json.size()) return "";
    return json.substr(start, end - start);
}

// extractJsonInt safely extracts an integer value for a given key.
// Returns 0 if not found. Does not access out-of-bounds memory.
static int64_t extractJsonInt(const std::string& json, const std::string& key) {
    size_t pos = json.find(key);
    if (pos == std::string::npos) return 0;
    pos = json.find(':', pos + key.length());
    if (pos == std::string::npos) return 0;

    size_t i = pos + 1;
    while (i < json.size() && (json[i] == ' ' || json[i] == '\t')) i++;

    bool negative = false;
    if (i < json.size() && json[i] == '-') { negative = true; i++; }

    int64_t val = 0;
    while (i < json.size() && json[i] >= '0' && json[i] <= '9') {
        val = val * 10 + (json[i] - '0');
        i++;
    }
    return negative ? -val : val;
}

// extractJsonDouble safely extracts a floating-point value for a given key.
// Handles integers, decimals, and scientific notation.
// Returns 0.0 if not found.
static double extractJsonDouble(const std::string& json, const std::string& key) {
    size_t pos = json.find(key);
    if (pos == std::string::npos) return 0.0;
    pos = json.find(':', pos + key.length());
    if (pos == std::string::npos) return 0.0;

    size_t i = pos + 1;
    while (i < json.size() && (json[i] == ' ' || json[i] == '\t')) i++;

    // Find end of number token
    size_t start = i;
    while (i < json.size() && (json[i] == '-' || json[i] == '+' ||
                                json[i] == '.' || json[i] == 'e' || json[i] == 'E' ||
                                (json[i] >= '0' && json[i] <= '9'))) {
        i++;
    }
    if (i == start) return 0.0;

    try {
        return std::stod(json.substr(start, i - start));
    } catch (...) {
        return 0.0;
    }
}

// ---------------------------------------------------------------------------
// Engine implementation
// ---------------------------------------------------------------------------

Engine::Engine(const EngineConfig& config)
    : config_(config),
      isConnected_(false),
      handshakeComplete_(false),
      running_(false) {

    network_      = std::make_unique<BsdNetworkClient>();
    clockSync_    = std::make_unique<ClockSync>();
    jitterBuffer_ = std::make_unique<JitterBuffer>(config.sampleRate, config.channels);
    decoder_      = std::make_unique<Decoder>(config.sampleRate, config.channels);

    network_->setTCPMessageCallback([this](const std::string& msg) { handleTCPMessage(msg); });
    network_->setUDPPacketCallback([this](const uint8_t* data, size_t len) { handleUDPPacket(data, len); });
    lastSeqNum_ = 0;
}

Engine::~Engine() {
    stop();
}

bool Engine::start() {
    if (isConnected_) return true;

    // Pre-allocate decode buffer to prevent allocation on the hot UDP receive thread
    decodeBuffer_.resize(48000 * 2); // Capacious enough for very large PLC frames
    
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

    // 3. Send JOIN_REQUEST with all fields properly escaped (F13 fix).
    // The Go server uses snake_case JSON tags (type, token, device_name, platform, udp_port).
    std::string joinReq =
        "{\"type\": \"JOIN_REQUEST\""
        ", \"token\": \""       + escapeJsonString(config_.token)      + "\""
        ", \"device_name\": \"" + escapeJsonString(config_.deviceName) + "\""
        ", \"platform\": \""    + escapeJsonString(config_.platform)   + "\""
        ", \"udp_port\": "      + std::to_string(network_->getBoundUDPPort()) +
        "}";

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

    if (network_) {
        network_->disconnectTCP();
        network_->stopUDP();
    }
    handshakeComplete_ = false;
    isConnected_ = false;
}

void Engine::readAudio(float* outPcm, size_t numFrames, int64_t playoutTimestampUs) {
    // Do not pull audio until the full TCP handshake (clock sync) is complete.
    // Before that, clockSync_->getOffsetUs() == 0, which would cause all frames
    // to appear at the wrong server time and get discarded immediately (F11 fix).
    if (!handshakeComplete_.load(std::memory_order_acquire)) {
        std::fill(outPcm, outPcm + (numFrames * config_.channels), 0.0f);
        return;
    }

    size_t written = jitterBuffer_->pull(outPcm, numFrames,
                                         playoutTimestampUs,
                                         clockSync_->getOffsetUs());
}

void Engine::setConnectionCallback(std::function<void(bool, const std::string&)> cb) {
    onConnectionStatus_ = std::move(cb);
}

void Engine::setJitterUpdateCallback(std::function<void(double)> cb) {
    onJitterUpdate_ = std::move(cb);
}

// ---------------------------------------------------------------------------
// handleTCPMessage — called from the TCP read thread
// ---------------------------------------------------------------------------
void Engine::handleTCPMessage(const std::string& jsonMsg) {
    std::string msgType = extractJsonString(jsonMsg, "\"type\"");

    if (msgType == "JOIN_ACCEPT") {
        {
            std::lock_guard<std::mutex> lock(clientMu_);
            clientId_ = extractJsonString(jsonMsg, "\"client_id\"");
        }
        // Note: we set isConnected_ here so the maintenance loop knows we're
        // authenticated, but we deliberately do NOT set handshakeComplete_ yet.
        // The audio thread will stay silent until clock sync finishes (F11 fix).
        isConnected_ = true;
        LOGI("JOIN_ACCEPT: client_id=%s", clientId_.c_str());

        if (onConnectionStatus_) onConnectionStatus_(true, "");

        // Send a valid dummy UDP packet so the server has our source port confirmed.
        // (The server already registered our port from the JOIN_REQUEST UDP port field,
        // but this packet confirms the phone's actual ephemeral outbound source port
        // in case NAT remapped it — common on some Android hotspot configurations.)
        UDPPacket dummy;
        dummy.version     = PROTOCOL_VERSION;
        dummy.type        = PacketType::ClockSyncReply;
        dummy.seqNum      = 0;
        dummy.timestampUs = clockSync_->getLocalTimeUs();
        dummy.channelMask = ChannelMask::StereoMix;
        network_->sendUDPPacket(config_.serverIp, config_.udpPort, dummy.serialize());

    } else if (msgType == "JOIN_REJECT") {
        std::string reason = extractJsonString(jsonMsg, "\"reason\"");
        LOGE("JOIN_REJECT: %s", reason.c_str());
        if (onConnectionStatus_) onConnectionStatus_(false, reason);
        stop();

    } else if (msgType == "CLOCK_SYNC_PROBE") {
        // Respond as fast as possible — minimize processing between timestamps.
        int64_t clientRecvTs  = clockSync_->getLocalTimeUs();
        int64_t serverSendTs  = extractJsonInt(jsonMsg, "\"server_send_ts\"");
        int64_t clientSendTs  = clockSync_->getLocalTimeUs();

        std::string reply =
            "{\"type\": \"CLOCK_SYNC_REPLY\""
            ", \"server_send_ts\": " + std::to_string(serverSendTs) +
            ", \"client_recv_ts\": " + std::to_string(clientRecvTs) +
            ", \"client_send_ts\": " + std::to_string(clientSendTs) +
            "}";
        network_->sendTCPMessage(reply);

    } else if (msgType == "CLOCK_OFFSET") {
        int64_t offset = extractJsonInt(jsonMsg, "\"offset_us\"");
        clockSync_->setOffset(offset);
        LOGI("CLOCK_OFFSET applied: %lld us", (long long)offset);

        // The server sends CLOCK_OFFSET as the last message of the initial
        // handshake. Only now is it safe to begin pulling audio (F11 fix).
        handshakeComplete_.store(true, std::memory_order_release);

    } else if (msgType == "SET_GLOBAL_LATENCY") {
        // Parse as double — the server sends float64, e.g. "target_ms": 47.5 (F12 fix).
        double targetMs = extractJsonDouble(jsonMsg, "\"target_ms\"");
        LOGI("SET_GLOBAL_LATENCY: %.2f ms", targetMs);
        jitterBuffer_->setGlobalDelayMs(targetMs);

    } else if (msgType == "SET_FRAME_SIZE") {
        // Future: reconfigure encoder frame size for iOS background fallback.
        int64_t frameMs = extractJsonInt(jsonMsg, "\"frame_ms\"");
        LOGI("SET_FRAME_SIZE: %lld ms (acknowledged)", (long long)frameMs);
    }
}

// ---------------------------------------------------------------------------
// handleUDPPacket — called from the UDP receiver thread
// ---------------------------------------------------------------------------
void Engine::handleUDPPacket(const uint8_t* data, size_t length) {
    UDPPacket pkt;
    if (!UDPPacket::deserialize(data, length, pkt)) return;

    if (pkt.type == PacketType::Audio) {
        int64_t localRecvTs = clockSync_->getLocalTimeUs();

        // Implement Packet Loss Concealment (PLC)
        if (lastSeqNum_ != 0 && pkt.seqNum > lastSeqNum_ + 1) {
            uint32_t lostPackets = pkt.seqNum - lastSeqNum_ - 1;
            // Cap PLC frames to avoid spinning too long if seqNum jumps wildly
            if (lostPackets > 10) lostPackets = 10;
            
            for (uint32_t i = 1; i <= lostPackets; ++i) {
                int decodedSamples = decoder_->decodeMissing(decodeBuffer_);
                if (decodedSamples > 0) {
                    // Interpolate the timestamp for the missing packet
                    int64_t frameDurationUs = (decoder_->getFrameSamples() * 1000000LL) / config_.sampleRate;
                    int64_t interpolatedTs = pkt.timestampUs - (lostPackets - i + 1) * frameDurationUs;
                    
                    jitterBuffer_->push(decodeBuffer_.data(),
                                        static_cast<size_t>(decodedSamples),
                                        interpolatedTs,
                                        localRecvTs,
                                        clockSync_->getOffsetUs());
                    LOGI("PLC inserted missing frame seq=%u ts=%lld", lastSeqNum_ + i, (long long)interpolatedTs);
                }
            }
        }
        lastSeqNum_ = pkt.seqNum;

        int decodedSamples = decoder_->decode(pkt.payloadData, pkt.payloadSize, pkt.codecFlag, decodeBuffer_);

        if (decodedSamples > 0) {
            jitterBuffer_->push(decodeBuffer_.data(),
                                static_cast<size_t>(decodedSamples),
                                pkt.timestampUs,
                                localRecvTs,
                                clockSync_->getOffsetUs());
        }

    } else if (pkt.type == PacketType::ClockSyncProbe) {
        // Fast UDP clock sync path (not used in the current server but handled
        // defensively for future use).
        UDPPacket reply;
        reply.version     = PROTOCOL_VERSION;
        reply.type        = PacketType::ClockSyncReply;
        reply.seqNum      = 0;
        reply.timestampUs = clockSync_->getLocalTimeUs();
        reply.channelMask = ChannelMask::StereoMix;
        auto rawReply = reply.serialize();
        network_->sendUDPPacket(config_.serverIp, config_.udpPort, rawReply);
    }
}

// ---------------------------------------------------------------------------
// maintenanceLoop — runs on its own thread, never the audio thread
// ---------------------------------------------------------------------------
void Engine::maintenanceLoop() {
    int loops = 0;
    while (running_) {
        std::this_thread::sleep_for(std::chrono::seconds(1));
        if (!isConnected_) continue;
        loops++;
        // Every 2 seconds: Send TCP and UDP Heartbeat
        if (loops % 2 == 0) {
            network_->sendTCPMessage("{\"type\": \"HEARTBEAT\"}");

            // Send UDP KeepAlive to punch through NAT/Firewalls
            {
                std::lock_guard<std::mutex> lock(clientMu_);
                if (!clientId_.empty()) {
                    UDPPacket keepAlive;
                    keepAlive.version = PROTOCOL_VERSION;
                    keepAlive.type = PacketType::KeepAlive;
                    keepAlive.seqNum = 0;
                    keepAlive.timestampUs = clockSync_->getLocalTimeUs();
                    keepAlive.channelMask = ChannelMask::StereoMix;
                    keepAlive.payloadData = reinterpret_cast<const uint8_t*>(clientId_.c_str());
                    keepAlive.payloadSize = clientId_.size();
                    network_->sendUDPPacket(config_.serverIp, config_.udpPort, keepAlive.serialize());
                }
            }
        }

        // Every 5 seconds: Jitter report
        if (loops % 5 == 0) {
            double p95 = jitterBuffer_->getP95JitterMs();

            // Build JSON with precise float formatting (2 decimal places).
            std::ostringstream oss;
            oss << "{\"type\": \"JITTER_REPORT\", \"p95_ms\": "
                << std::fixed << std::setprecision(2) << p95 << "}";
            network_->sendTCPMessage(oss.str());

            if (onJitterUpdate_) onJitterUpdate_(p95);
        }
    }
}

} // namespace soundswarm
