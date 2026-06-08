package com.soundswarm.client

import android.util.Log

/**
 * SoundSwarmClient provides the Kotlin interface to the native C++ engine.
 */
class SoundSwarmClient {

    companion object {
        private const val TAG = "SoundSwarmClient"

        init {
            try {
                System.loadLibrary("soundswarm_jni")
                Log.i(TAG, "Successfully loaded native library")
            } catch (e: UnsatisfiedLinkError) {
                Log.e(TAG, "Failed to load native library", e)
            }
        }
    }

    /**
     * Connects to the SoundSwarm server and starts synchronized audio playback.
     * 
     * @param serverIp The IP address of the laptop server
     * @param tcpPort The TCP control port
     * @param udpPort The UDP stream port
     * @param token The cryptographic session token
     * @param deviceName The human-readable name of this phone
     * @return true if successfully connected and audio started
     */
    fun connect(serverIp: String, tcpPort: Int, udpPort: Int, token: String, deviceName: String): Boolean {
        Log.i(TAG, "Connecting to $serverIp...")
        return nativeConnect(serverIp, tcpPort, udpPort, token, deviceName)
    }

    /**
     * Disconnects from the server and stops audio playback.
     */
    fun disconnect() {
        Log.i(TAG, "Disconnecting...")
        nativeDisconnect()
    }

    // --- Native Methods ---
    private external fun nativeConnect(serverIp: String, tcpPort: Int, udpPort: Int, token: String, deviceName: String): Boolean
    private external fun nativeDisconnect()
}
