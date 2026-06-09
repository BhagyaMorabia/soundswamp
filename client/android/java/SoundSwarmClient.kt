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
 * The TCP connection succeeds by luck (local subnet IP routes via Wi-Fi even without
 * binding), but the UDP stream gets sent out through the cell tower, never reaching
 * the laptop. (F6 fix)
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
     * This method MUST be called from a Context-aware location (Activity or Service)
     * so that ConnectivityManager is available. It will:
     *   1. Request the current Wi-Fi network from the OS.
     *   2. Bind the entire process to that network so ALL native sockets use it.
     *   3. Boot the native engine.
     *   4. Invoke the result callback on the calling thread after connection.
     *
     * @param serverIp      The IP address of the laptop server
     * @param tcpPort       The TCP control port
     * @param udpPort       The UDP stream port
     * @param token         The cryptographic session token
     * @param deviceName    The human-readable name of this phone
     * @param onResult      Callback invoked with (success: Boolean, errorMsg: String)
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

        val connectivityManager =
            context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

        // Request specifically the Wi-Fi transport. This matches the hotspot interface
        // the phone is connected to. Without TRANSPORT_WIFI, Android may select the
        // cellular network as the "best" network when Wi-Fi has no internet access.
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .build()

        connectivityManager.requestNetwork(
            request,
            object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) {
                    Log.i(TAG, "Wi-Fi network available — binding process to network")

                    // F6 fix: Force ALL native C++ POSIX sockets in this process to
                    // route via this specific Wi-Fi network object. This call survives
                    // until unregisterNetworkCallback() is called or the process dies.
                    val bound = connectivityManager.bindProcessToNetwork(network)
                    if (!bound) {
                        Log.e(TAG, "bindProcessToNetwork failed — UDP may route via cell")
                        onResult(false, "Failed to bind to Wi-Fi network")
                        connectivityManager.unregisterNetworkCallback(this)
                        return
                    }

                    Log.i(TAG, "Process bound to Wi-Fi network — booting native engine")
                    val success = nativeConnect(serverIp, tcpPort, udpPort, token, deviceName)
                    Log.i(TAG, "nativeConnect result: $success")

                    onResult(success, if (success) "" else "Native engine failed to start")

                    // Do NOT unregister the callback here — we need the binding to
                    // persist for the duration of the audio session. Call disconnect()
                    // when done, which will trigger onLost() and release the binding.
                }

                override fun onUnavailable() {
                    Log.e(TAG, "No Wi-Fi network available — not connected to hotspot?")
                    onResult(false, "Wi-Fi network unavailable. Is the phone connected to the hotspot?")
                }

                override fun onLost(network: Network) {
                    Log.w(TAG, "Wi-Fi network lost — audio stream will stop")
                    // Clear the process binding so future sockets don't get stuck
                    // on a dead network handle.
                    connectivityManager.bindProcessToNetwork(null)
                }
            }
        )
    }

    /**
     * Disconnects from the server and stops audio playback.
     */
    fun disconnect() {
        Log.i(TAG, "Disconnecting...")
        // Clear the process-level network binding.
        val connectivityManager =
            context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        connectivityManager.bindProcessToNetwork(null)
        nativeDisconnect()
    }

    // --- Native Methods ---
    private external fun nativeConnect(
        serverIp: String, tcpPort: Int, udpPort: Int, token: String, deviceName: String
    ): Boolean

    private external fun nativeDisconnect()
}
