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
class SoundSwarmClient(context: Context) {

    private val appContext = context.applicationContext
    private var currentNetworkCallback: ConnectivityManager.NetworkCallback? = null

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
     */
    @Synchronized
    fun connect(
        serverIp: String,
        tcpPort: Int,
        udpPort: Int,
        token: String,
        deviceName: String,
        onResult: (Boolean, String) -> Unit
    ) {
        Log.i(TAG, "Requesting Wi-Fi network binding before connecting to $serverIp...")

        val cm = appContext.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

        // Unregister any previous callback to prevent memory leaks and zombie network bindings
        currentNetworkCallback?.let {
            try {
                cm.unregisterNetworkCallback(it)
            } catch (e: Exception) {
                Log.w(TAG, "Failed to unregister old callback", e)
            }
        }

        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .removeCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()

        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                Log.i(TAG, "Wi-Fi network available — binding process")

                val bound = cm.bindProcessToNetwork(network)
                if (!bound) {
                    Log.e(TAG, "bindProcessToNetwork failed")
                    onResult(false, "Failed to bind to Wi-Fi network")
                    return
                }

                Log.i(TAG, "Process bound to Wi-Fi — booting native engine")
                val success = nativeConnect(serverIp, tcpPort, udpPort, token, deviceName)
                Log.i(TAG, "nativeConnect result: $success")
                onResult(success, if (success) "" else "Native engine failed to start")
            }

            override fun onUnavailable() {
                Log.e(TAG, "No Wi-Fi network available")
                onResult(false, "Wi-Fi unavailable. Is the phone connected to the hotspot?")
                synchronized(this@SoundSwarmClient) {
                    currentNetworkCallback = null
                }
            }

            override fun onLost(network: Network) {
                Log.w(TAG, "Wi-Fi network lost")
                cm.bindProcessToNetwork(null)
                synchronized(this@SoundSwarmClient) {
                    currentNetworkCallback = null
                }
            }
        }

        currentNetworkCallback = callback

        try {
            cm.requestNetwork(request, callback)
        } catch (e: SecurityException) {
            Log.e(TAG, "SecurityException requesting network. Ensure CHANGE_NETWORK_STATE is granted.", e)
            currentNetworkCallback = null
            onResult(false, "Network permission denied")
        } catch (e: Exception) {
            Log.e(TAG, "Failed to request network", e)
            currentNetworkCallback = null
            onResult(false, "Failed to request Wi-Fi network")
        }
    }

    /**
     * Disconnects from the server and stops audio playback.
     */
    @Synchronized
    fun disconnect() {
        Log.i(TAG, "Disconnecting...")
        val cm = appContext.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        
        currentNetworkCallback?.let {
            try {
                cm.unregisterNetworkCallback(it)
            } catch (e: Exception) {
                Log.w(TAG, "Error unregistering callback during disconnect", e)
            }
            currentNetworkCallback = null
        }
        
        cm.bindProcessToNetwork(null)
        nativeDisconnect()
    }

    // --- Native Methods ---
    private external fun nativeConnect(
        serverIp: String, tcpPort: Int, udpPort: Int, token: String, deviceName: String
    ): Boolean

    private external fun nativeDisconnect()
}
