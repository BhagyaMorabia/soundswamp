package com.soundswarm.client

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.util.Log

/**
 * SoundSwarmClient provides the Kotlin interface to the native C++ engine.
 *
 * Key requirement: ALL native socket calls (TCP connect, UDP bind) must happen
 * AFTER bindProcessToNetwork() is called with the active Wi-Fi network handle.
 *
 * Without this, Android 10+ silently routes raw POSIX sockets through the 5G/LTE
 * cellular modem when the connected Wi-Fi has "No Internet Access" (as hotspots do).
 * The TCP connection succeeds (local subnet IP routes via Wi-Fi anyway), but the
 * UDP audio stream gets sent through the cell tower and never reaches the laptop.
 * (F6 fix)
 */
class SoundSwarmClient(private val context: Context) {

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
     * This method:
     *   1. Requests the current Wi-Fi network from the OS
     *   2. Binds the entire process to that network (all C++ sockets route via Wi-Fi)
     *   3. Boots the native engine
     *   4. Calls onResult(success, errorMsg) — NOT on the main thread
     *
     * @param serverIp   The IP address of the laptop server
     * @param tcpPort    The TCP control port
     * @param udpPort    The UDP stream port
     * @param token      The cryptographic session token
     * @param deviceName The human-readable name of this phone
     * @param onResult   Callback invoked with (success: Boolean, errorMsg: String)
     */
    fun connect(
        serverIp: String,
        tcpPort: Int,
        udpPort: Int,
        token: String,
        deviceName: String,
        onResult: (Boolean, String) -> Unit
    ) {
        Log.i(TAG, "Requesting Wi-Fi network binding before connecting to $serverIp...")

        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .build()

        cm.requestNetwork(request, object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                Log.i(TAG, "Wi-Fi network available — binding process")

                val bound = cm.bindProcessToNetwork(network)
                if (!bound) {
                    Log.e(TAG, "bindProcessToNetwork failed")
                    onResult(false, "Failed to bind to Wi-Fi network")
                    cm.unregisterNetworkCallback(this)
                    return
                }

                Log.i(TAG, "Process bound to Wi-Fi — booting native engine")
                val success = nativeConnect(serverIp, tcpPort, udpPort, token, deviceName)
                Log.i(TAG, "nativeConnect result: $success")
                onResult(success, if (success) "" else "Native engine failed to start")
                // Keep callback registered so the binding stays alive for the session.
            }

            override fun onUnavailable() {
                Log.e(TAG, "No Wi-Fi network available")
                onResult(false, "Wi-Fi unavailable. Is the phone connected to the hotspot?")
            }

            override fun onLost(network: Network) {
                Log.w(TAG, "Wi-Fi network lost")
                cm.bindProcessToNetwork(null)
            }
        })
    }

    /**
     * Disconnects from the server and stops audio playback.
     */
    fun disconnect() {
        Log.i(TAG, "Disconnecting...")
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        cm.bindProcessToNetwork(null)
        nativeDisconnect()
    }

    // --- Native Methods ---
    private external fun nativeConnect(
        serverIp: String, tcpPort: Int, udpPort: Int, token: String, deviceName: String
    ): Boolean

    private external fun nativeDisconnect()
}
