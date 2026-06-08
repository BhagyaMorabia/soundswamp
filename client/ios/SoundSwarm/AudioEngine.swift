import Foundation
import AVFoundation

class AudioEngineManager {
    private let audioEngine = AVAudioEngine()
    private let playerNode = AVAudioPlayerNode()
    private let wrapper: SoundSwarmWrapper
    private var isPlaying = false
    
    // The C++ engine uses steady_clock, which on iOS maps to mach_absolute_time.
    // We need the mach_timebase_info to convert.
    private var timebaseInfo = mach_timebase_info_data_t()

    init(wrapper: SoundSwarmWrapper) {
        self.wrapper = wrapper
        mach_timebase_info(&timebaseInfo)
        setupAudioEngine()
    }

    private func setupAudioEngine() {
        let audioSession = AVAudioSession.sharedInstance()
        do {
            // Optimize for low latency playback
            try audioSession.setCategory(.playback, mode: .default, policy: .default, options: [.mixWithOthers])
            try audioSession.setPreferredIOBufferDuration(0.01) // 10ms preferred buffer
            try audioSession.setPreferredSampleRate(48000.0)
            try audioSession.setActive(true)
        } catch {
            print("Failed to set up AVAudioSession: \(error)")
        }

        audioEngine.attach(playerNode)

        let format = AVAudioFormat(standardFormatWithSampleRate: 48000.0, channels: 2)!
        audioEngine.connect(playerNode, to: audioEngine.mainMixerNode, format: format)
        
        // Use a custom render block to pull audio precisely when the hardware needs it
        playerNode.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] (buffer, time) in
            self?.renderAudio(buffer: buffer, time: time)
        }
    }

    private func renderAudio(buffer: AVAudioPCMBuffer, time: AVAudioTime) {
        guard let floatChannelData = buffer.floatChannelData else { return }
        let numFrames = Int(buffer.frameLength)
        
        // Interleaved buffer for C++ Engine
        var interleavedPcm = [Float](repeating: 0.0, count: numFrames * 2)

        // Calculate physical playout timestamp using mach_absolute_time
        // AVAudioTime.hostTime is in mach absolute time units
        var playoutTimestampUs: Int64 = 0
        if time.isHostTimeValid {
            let machTime = time.hostTime
            let nanos = (machTime * UInt64(timebaseInfo.numer)) / UInt64(timebaseInfo.denom)
            playoutTimestampUs = Int64(nanos / 1000)
        } else {
            // Fallback if host time is invalid
            let now = ProcessInfo.processInfo.systemUptime
            playoutTimestampUs = Int64(now * 1_000_000)
        }

        // Pull synchronized audio from C++
        wrapper.readAudio(&interleavedPcm, numFrames: numFrames, playoutTimestampUs: playoutTimestampUs)

        // De-interleave for AVAudioPCMBuffer (which uses non-interleaved channels)
        let leftChannel = floatChannelData[0]
        let rightChannel = floatChannelData[1]
        
        for i in 0..<numFrames {
            leftChannel[i] = interleavedPcm[i * 2]
            rightChannel[i] = interleavedPcm[i * 2 + 1]
        }
    }

    func start() -> Bool {
        guard !isPlaying else { return true }
        do {
            try audioEngine.start()
            playerNode.play()
            isPlaying = true
            return true
        } catch {
            print("Failed to start AVAudioEngine: \(error)")
            return false
        }
    }

    func stop() {
        if isPlaying {
            playerNode.stop()
            audioEngine.stop()
            isPlaying = false
        }
    }
}
