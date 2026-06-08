#pragma once

#include <cstdint>
#include <vector>
#include <string>

namespace soundswarm {

constexpr uint8_t PROTOCOL_VERSION = 1;
constexpr size_t UDP_HEADER_SIZE = 17;
constexpr size_t MAX_PACKET_SIZE = 1400;

// UDP Packet Types
enum class PacketType : uint8_t {
    Audio = 1,
    ClockSyncProbe = 2,
    ClockSyncReply = 3
};

// Channel Masks
enum class ChannelMask : uint8_t {
    FrontLeft = 0,
    FrontRight = 1,
    Center = 2,
    LFE = 3,
    SurroundLeft = 4,
    SurroundRight = 5,
    BackLeft = 6,
    BackRight = 7,
    StereoMix = 255
};

// UDP Packet Layout
struct UDPPacket {
    uint8_t version;
    PacketType type;
    uint32_t seqNum;
    int64_t timestampUs;
    ChannelMask channelMask;
    std::vector<uint8_t> payload;

    // Deserializes a raw byte buffer into a UDPPacket
    static bool deserialize(const uint8_t* data, size_t length, UDPPacket& outPacket);
    
    // Serializes the UDPPacket into a byte buffer
    std::vector<uint8_t> serialize() const;
};

// TCP Message Types
enum class TCPMessageType {
    JoinRequest,
    JoinAccept,
    JoinReject,
    ClockSyncProbe,
    ClockSyncReply,
    ClockOffset,
    JitterReport,
    SetGlobalLatency,
    SetFrameSize,
    ClientList,
    Heartbeat,
    AppStateChange
};

// TCP messages are JSON strings prefixed with a 4-byte big-endian length.
// We will use a JSON library (e.g., nlohmann/json) in the implementation.

} // namespace soundswarm
