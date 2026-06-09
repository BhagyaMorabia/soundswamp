package com.soundswarm.client

import android.content.Intent
import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import com.google.zxing.integration.android.IntentIntegrator
import org.json.JSONObject

class MainActivity : AppCompatActivity() {

    private lateinit var tvStatus: TextView
    private lateinit var btnScan: Button
    private lateinit var btnDisconnect: Button

    // SoundSwarmClient now requires Context so it can access ConnectivityManager
    // for the Wi-Fi network binding (F6 fix).
    private lateinit var client: SoundSwarmClient

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        client = SoundSwarmClient(this)

        tvStatus = findViewById(R.id.tvStatus)
        btnScan = findViewById(R.id.btnScan)
        btnDisconnect = findViewById(R.id.btnDisconnect)

        btnScan.setOnClickListener {
            val integrator = IntentIntegrator(this)
            integrator.setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
            integrator.setPrompt("Scan SoundSwarm QR Code")
            integrator.setCameraId(0)
            integrator.setBeepEnabled(false)
            integrator.setBarcodeImageEnabled(false)
            integrator.initiateScan()
        }

        btnDisconnect.setOnClickListener {
            client.disconnect()
            updateUI(false)
        }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        val result = IntentIntegrator.parseActivityResult(requestCode, resultCode, data)
        if (result != null) {
            if (result.contents == null) {
                Toast.makeText(this, "Cancelled", Toast.LENGTH_LONG).show()
            } else {
                handleQRCode(result.contents)
            }
        } else {
            super.onActivityResult(requestCode, resultCode, data)
        }
    }

    private fun handleQRCode(jsonStr: String) {
        try {
            val obj = JSONObject(jsonStr)
            val ip       = obj.getString("ip")
            val tcpPort  = obj.getInt("tcp_port")
            val udpPort  = obj.getInt("udp_port")
            val token    = obj.getString("token")
            val deviceName = android.os.Build.MODEL

            tvStatus.text = "Binding Wi-Fi network..."
            Toast.makeText(this, "Connecting to $ip...", Toast.LENGTH_SHORT).show()

            // SoundSwarmClient.connect() now:
            //   1. Requests the Wi-Fi network from ConnectivityManager
            //   2. Calls bindProcessToNetwork() so C++ sockets use hotspot Wi-Fi
            //   3. Boots the native engine
            //   4. Calls onResult on the calling thread (NOT the main thread)
            //
            // Result is dispatched back to the UI thread via runOnUiThread.
            client.connect(ip, tcpPort, udpPort, token, deviceName) { success, errorMsg ->
                runOnUiThread {
                    if (success) {
                        updateUI(true)
                    } else {
                        val msg = if (errorMsg.isNotEmpty()) errorMsg else "Failed to connect"
                        Toast.makeText(this@MainActivity, msg, Toast.LENGTH_LONG).show()
                        tvStatus.text = "Not Connected"
                    }
                }
            }

        } catch (e: Exception) {
            Log.e("MainActivity", "Invalid QR code format", e)
            Toast.makeText(this, "Invalid QR code", Toast.LENGTH_LONG).show()
        }
    }

    private fun updateUI(connected: Boolean) {
        if (connected) {
            tvStatus.text = "Connected & Synchronized"
            tvStatus.setTextColor(android.graphics.Color.GREEN)
            btnScan.visibility = View.GONE
            btnDisconnect.visibility = View.VISIBLE
        } else {
            tvStatus.text = "Not Connected"
            tvStatus.setTextColor(android.graphics.Color.WHITE)
            btnScan.visibility = View.VISIBLE
            btnDisconnect.visibility = View.GONE
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        client.disconnect()
    }
}
