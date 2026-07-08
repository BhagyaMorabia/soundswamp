#pragma once

#include <string>
#include <functional>
#include <thread>
#include <atomic>
#include <vector>
#include <cstdint>
#include <mutex>

namespace soundswarm {

class INetworkClient {
public:
    virtual ~INetworkClient() = default;

    // TCP Control
    virtual bool connectTCP(const std::string& ip, int port) = 0;
    virtual void disconnectTCP() = 0;

    // Thread-safe TCP send
    virtual bool sendTCPMessage(const std::string& jsonMsg) = 0;
    virtual void setTCPMessageCallback(std::function<void(const std::string&)> cb) = 0;

    // UDP Audio
    virtual bool startUDP(int localPort) = 0;
    virtual void stopUDP() = 0;
    virtual bool sendUDPPacket(const std::string& ip, int port, const std::vector<uint8_t>& data) = 0;
    virtual void setUDPPacketCallback(std::function<void(const uint8_t*, size_t)> cb) = 0;

    virtual int getBoundUDPPort() const = 0;
};

class BsdNetworkClient : public INetworkClient {
public:
    BsdNetworkClient();
    ~BsdNetworkClient() override;

    // TCP Control
    bool connectTCP(const std::string& ip, int port) override;
    void disconnectTCP() override;

    // Thread-safe TCP send — may be called from multiple threads.
    // Serialized internally with a mutex (F15 fix).
    bool sendTCPMessage(const std::string& jsonMsg) override;
    void setTCPMessageCallback(std::function<void(const std::string&)> cb) override;

    // UDP Audio
    bool startUDP(int localPort) override;
    void stopUDP() override;
    bool sendUDPPacket(const std::string& ip, int port, const std::vector<uint8_t>& data) override;
    void setUDPPacketCallback(std::function<void(const uint8_t*, size_t)> cb) override;

    int getBoundUDPPort() const override;

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
