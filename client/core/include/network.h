#pragma once

#include <string>
#include <functional>
#include <thread>
#include <atomic>
#include <vector>
#include <cstdint>
#include <mutex>

namespace soundswarm {

class NetworkClient {
public:
    NetworkClient();
    ~NetworkClient();

    // TCP Control
    bool connectTCP(const std::string& ip, int port);
    void disconnectTCP();

    // Thread-safe TCP send — may be called from multiple threads.
    // Serialized internally with a mutex (F15 fix).
    bool sendTCPMessage(const std::string& jsonMsg);
    void setTCPMessageCallback(std::function<void(const std::string&)> cb);

    // UDP Audio
    bool startUDP(int localPort);
    void stopUDP();
    bool sendUDPPacket(const std::string& ip, int port, const std::vector<uint8_t>& data);
    void setUDPPacketCallback(std::function<void(const uint8_t*, size_t)> cb);

    int getBoundUDPPort() const;

private:
    void tcpReadLoop();
    void udpReadLoop();

    int tcpSocket_;
    int udpSocket_;
    int boundUdpPort_;

    std::atomic<bool> tcpRunning_;
    std::atomic<bool> udpRunning_;

    std::thread tcpThread_;
    std::thread udpThread_;

    // Serializes concurrent TCP sends from the maintenance thread and the
    // TCP message callback thread (F15 fix).
    std::mutex tcpSendMu_;

    std::function<void(const std::string&)>    tcpCb_;
    std::function<void(const uint8_t*, size_t)> udpCb_;
};

} // namespace soundswarm
