#include "protocol.h"
#include <cstring>
#include <stdexcept>

// Platform-independent endian swapping (network is big-endian)
#ifdef _WIN32
#include <winsock2.h>
#define be32toh ntohl
#define htobe32 htonl
#define be64toh ntohll
#define htobe64 htonll
#else
#include <arpa/inet.h>
#if defined(__APPLE__)
#include <libkern/OSByteOrder.h>
#define be64toh(x) OSSwapBigToHostInt64(x)
#define htobe64(x) OSSwapHostToBigInt64(x)
#define be32toh(x) OSSwapBigToHostInt32(x)
#else
#include <endian.h>
#endif
#endif

namespace soundswarm {

bool UDPPacket::deserialize(const uint8_t* data, size_t length, UDPPacket& outPacket) {
    if (length < UDP_HEADER_SIZE) return false;
    
    outPacket.version = data[0];
    if (outPacket.version != PROTOCOL_VERSION) return false;

    outPacket.type = static_cast<PacketType>(data[1]);

    uint32_t seqNetwork;
    std::memcpy(&seqNetwork, data + 2, sizeof(uint32_t));
    outPacket.seqNum = be32toh(seqNetwork);

    int64_t tsNetwork;
    std::memcpy(&tsNetwork, data + 6, sizeof(int64_t));
    outPacket.timestampUs = be64toh(tsNetwork);

    outPacket.channelMask = static_cast<ChannelMask>(data[14]);
    outPacket.codecFlag = static_cast<CodecFlag>(data[15]);

    uint16_t payloadLenNetwork;
    std::memcpy(&payloadLenNetwork, data + 16, sizeof(uint16_t));
    uint16_t payloadLen = ntohs(payloadLenNetwork);

    // Sanity check
    if (UDP_HEADER_SIZE + payloadLen > length) return false;

    outPacket.payload.assign(data + UDP_HEADER_SIZE, data + UDP_HEADER_SIZE + payloadLen);

    return true;
}

std::vector<uint8_t> UDPPacket::serialize() const {
    std::vector<uint8_t> buffer(UDP_HEADER_SIZE + payload.size());

    buffer[0] = version;
    buffer[1] = static_cast<uint8_t>(type);

    uint32_t seqNetwork = htobe32(seqNum);
    std::memcpy(buffer.data() + 2, &seqNetwork, sizeof(uint32_t));

    int64_t tsNetwork = htobe64(timestampUs);
    std::memcpy(buffer.data() + 6, &tsNetwork, sizeof(int64_t));

    buffer[14] = static_cast<uint8_t>(channelMask);
    buffer[15] = static_cast<uint8_t>(codecFlag);

    uint16_t payloadLenNetwork = htons(static_cast<uint16_t>(payload.size()));
    std::memcpy(buffer.data() + 16, &payloadLenNetwork, sizeof(uint16_t));

    if (!payload.empty()) {
        std::memcpy(buffer.data() + UDP_HEADER_SIZE, payload.data(), payload.size());
    }

    return buffer;
}

} // namespace soundswarm
