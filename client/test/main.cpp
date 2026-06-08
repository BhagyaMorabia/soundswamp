#include <iostream>
#include <thread>
#include <chrono>
#include <vector>
#include "engine.h"
#include "clock_sync.h"

using namespace soundswarm;

int main(int argc, char** argv) {
    std::string ip = "127.0.0.1";
    int tcpPort = 8081;
    int udpPort = 8082;
    std::string token = "debug_token"; // In a real app, this is scanned from the QR code

    if (argc > 1) ip = argv[1];
    if (argc > 2) tcpPort = std::stoi(argv[2]);
    if (argc > 3) udpPort = std::stoi(argv[3]);

    std::cout << "Starting SoundSwarm Test Client connecting to " << ip << ":" << tcpPort << std::endl;

    EngineConfig config;
    config.serverIp = ip;
    config.tcpPort = tcpPort;
    config.udpPort = udpPort;
    config.token = token;
    config.deviceName = "CLI Test Client";
    config.platform = "test";
    config.sampleRate = 48000;
    config.channels = 2;

    Engine engine(config);

    engine.setConnectionCallback([](bool connected, const std::string& error) {
        if (connected) {
            std::cout << "[CLIENT] Connected to server successfully!" << std::endl;
        } else {
            std::cout << "[CLIENT] Disconnected: " << error << std::endl;
        }
    });

    engine.setJitterUpdateCallback([](double p95) {
        std::cout << "[CLIENT] P95 Network Jitter: " << p95 << " ms" << std::endl;
    });

    if (!engine.start()) {
        std::cerr << "[CLIENT] Failed to start engine." << std::endl;
        return 1;
    }

    // Simulate an audio DAC pulling data at exactly 48kHz
    std::vector<float> dummyBuffer(480 * 2); // 10ms of stereo float
    
    std::cout << "[CLIENT] Engine running. Press Ctrl+C to stop." << std::endl;

    auto nextPlayoutTime = std::chrono::steady_clock::now();

    while (true) {
        nextPlayoutTime += std::chrono::milliseconds(10);
        std::this_thread::sleep_until(nextPlayoutTime);

        // Calculate playout timestamp in microseconds
        auto duration = nextPlayoutTime.time_since_epoch();
        int64_t playoutUs = std::chrono::duration_cast<std::chrono::microseconds>(duration).count();

        // Read synchronized audio from the engine
        engine.readAudio(dummyBuffer.data(), 480, playoutUs);
        
        // In this test client, we just drop the audio into the void. 
        // The fact that readAudio doesn't crash and the jitter updates fire 
        // verifies the pipeline is intact.
    }

    return 0;
}
