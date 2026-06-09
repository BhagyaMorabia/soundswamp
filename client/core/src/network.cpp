#include "network.h"
#include <iostream>
#include <cstring>
#include <stdexcept>

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <fcntl.h>
#define closesocket close
#endif

namespace soundswarm {

NetworkClient::NetworkClient() 
    : tcpSocket_(-1), udpSocket_(-1), boundUdpPort_(0), tcpRunning_(false), udpRunning_(false) {
#ifdef _WIN32
    WSADATA wsaData;
    WSAStartup(MAKEWORD(2, 2), &wsaData);
#endif
}

NetworkClient::~NetworkClient() {
    disconnectTCP();
    stopUDP();
#ifdef _WIN32
    WSACleanup();
#endif
}

bool NetworkClient::connectTCP(const std::string& ip, int port) {
    tcpSocket_ = socket(AF_INET, SOCK_STREAM, 0);
    if (tcpSocket_ < 0) return false;

    struct sockaddr_in serverAddr{};
    serverAddr.sin_family = AF_INET;
    serverAddr.sin_port = htons(port);
    inet_pton(AF_INET, ip.c_str(), &serverAddr.sin_addr);

    // Set non-blocking
#ifdef _WIN32
    u_long mode = 1;
    ioctlsocket(tcpSocket_, FIONBIO, &mode);
#else
    int flags = fcntl(tcpSocket_, F_GETFL, 0);
    fcntl(tcpSocket_, F_SETFL, flags | O_NONBLOCK);
#endif

    connect(tcpSocket_, (struct sockaddr*)&serverAddr, sizeof(serverAddr));

    fd_set fdset;
    FD_ZERO(&fdset);
    FD_SET(tcpSocket_, &fdset);
    struct timeval tv;
    tv.tv_sec = 3;  // 3 second timeout
    tv.tv_usec = 0;

    if (select(tcpSocket_ + 1, nullptr, &fdset, nullptr, &tv) == 1) {
        int so_error;
        socklen_t len = sizeof(so_error);
        getsockopt(tcpSocket_, SOL_SOCKET, SO_ERROR, reinterpret_cast<char*>(&so_error), &len);
        if (so_error == 0) {
            // Restore blocking mode
#ifdef _WIN32
            mode = 0;
            ioctlsocket(tcpSocket_, FIONBIO, &mode);
#else
            fcntl(tcpSocket_, F_SETFL, flags);
#endif
            tcpRunning_ = true;
            tcpThread_ = std::thread(&NetworkClient::tcpReadLoop, this);
            return true;
        }
    }

    // Timeout or connection error
    closesocket(tcpSocket_);
    tcpSocket_ = -1;
    return false;
}

void NetworkClient::disconnectTCP() {
    tcpRunning_ = false;
    if (tcpSocket_ >= 0) {
#ifdef _WIN32
        shutdown(tcpSocket_, SD_BOTH);
#else
        shutdown(tcpSocket_, SHUT_RDWR);
#endif
        closesocket(tcpSocket_);
        tcpSocket_ = -1;
    }
    if (tcpThread_.joinable()) {
        tcpThread_.join();
    }
}

bool NetworkClient::sendTCPMessage(const std::string& jsonMsg) {
    if (tcpSocket_ < 0) return false;

    uint32_t len = htonl(static_cast<uint32_t>(jsonMsg.length()));

    std::vector<uint8_t> buffer(4 + jsonMsg.length());
    std::memcpy(buffer.data(), &len, 4);
    std::memcpy(buffer.data() + 4, jsonMsg.data(), jsonMsg.length());

    // F15 fix: serialize concurrent sends from the maintenance thread and the
    // TCP callback thread. Without this mutex, partial writes can interleave
    // and corrupt the length-prefix framing on the Go server.
    std::lock_guard<std::mutex> lock(tcpSendMu_);

#if defined(__linux__) || defined(__ANDROID__)
    int sent = send(tcpSocket_, reinterpret_cast<const char*>(buffer.data()), buffer.size(), MSG_NOSIGNAL);
#else
    int sent = send(tcpSocket_, reinterpret_cast<const char*>(buffer.data()), buffer.size(), 0);
#endif
    return sent == static_cast<int>(buffer.size());
}

void NetworkClient::setTCPMessageCallback(std::function<void(const std::string&)> cb) {
    tcpCb_ = std::move(cb);
}

void NetworkClient::tcpReadLoop() {
    while (tcpRunning_) {
        uint32_t lenNetwork;
        int bytesRead = recv(tcpSocket_, reinterpret_cast<char*>(&lenNetwork), 4, 0);
        if (bytesRead <= 0) break;

        uint32_t len = ntohl(lenNetwork);
        if (len > 65536) break; // Sanity check

        std::vector<char> buffer(len);
        int totalRead = 0;
        while (totalRead < len) {
            int r = recv(tcpSocket_, buffer.data() + totalRead, len - totalRead, 0);
            if (r <= 0) {
                tcpRunning_ = false;
                break;
            }
            totalRead += r;
        }

        if (tcpRunning_ && tcpCb_) {
            tcpCb_(std::string(buffer.data(), len));
        }
    }
    tcpRunning_ = false;
}

bool NetworkClient::startUDP(int localPort) {
    udpSocket_ = socket(AF_INET, SOCK_DGRAM, 0);
    if (udpSocket_ < 0) return false;

    // Fix C: Allow socket reuse and bind to specific interfaces on mobile
    int opt = 1;
    setsockopt(udpSocket_, SOL_SOCKET, SO_REUSEADDR, reinterpret_cast<const char*>(&opt), sizeof(opt));

    struct sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons(localPort);

    if (bind(udpSocket_, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        closesocket(udpSocket_);
        udpSocket_ = -1;
        return false;
    }

    struct sockaddr_in boundAddr{};
    socklen_t len = sizeof(boundAddr);
    getsockname(udpSocket_, (struct sockaddr*)&boundAddr, &len);
    boundUdpPort_ = ntohs(boundAddr.sin_port);

    udpRunning_ = true;
    udpThread_ = std::thread(&NetworkClient::udpReadLoop, this);
    return true;
}

void NetworkClient::stopUDP() {
    udpRunning_ = false;
    if (udpSocket_ >= 0) {
        closesocket(udpSocket_);
        udpSocket_ = -1;
    }
    if (udpThread_.joinable()) {
        udpThread_.join();
    }
}

bool NetworkClient::sendUDPPacket(const std::string& ip, int port, const std::vector<uint8_t>& data) {
    if (udpSocket_ < 0) return false;

    struct sockaddr_in destAddr{};
    destAddr.sin_family = AF_INET;
    destAddr.sin_port = htons(port);
    inet_pton(AF_INET, ip.c_str(), &destAddr.sin_addr);

    int sent = sendto(udpSocket_, reinterpret_cast<const char*>(data.data()), data.size(), 0,
                      (struct sockaddr*)&destAddr, sizeof(destAddr));
    return sent == static_cast<int>(data.size());
}

void NetworkClient::setUDPPacketCallback(std::function<void(const uint8_t*, size_t)> cb) {
    udpCb_ = std::move(cb);
}

void NetworkClient::udpReadLoop() {
    // F16 fix: buffer must be at least MaxPacketSize (17 header + 4096 payload = 4113).
    // The old 2048-byte buffer silently truncated large Opus frames.
    std::vector<uint8_t> buffer(4200);
    while (udpRunning_) {
        struct sockaddr_in srcAddr{};
        socklen_t srcLen = sizeof(srcAddr);
        
        int bytesRead = recvfrom(udpSocket_, reinterpret_cast<char*>(buffer.data()), buffer.size(), 0,
                                 (struct sockaddr*)&srcAddr, &srcLen);
        
        if (bytesRead > 0 && udpCb_) {
            udpCb_(buffer.data(), bytesRead);
        } else if (bytesRead < 0) {
            // Error or socket closed
            break;
        }
    }
    udpRunning_ = false;
}

int NetworkClient::getBoundUDPPort() const {
    return boundUdpPort_;
}

} // namespace soundswarm
