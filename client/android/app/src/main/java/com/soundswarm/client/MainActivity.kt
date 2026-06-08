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
    private val client = SoundSwarmClient()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

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
            val ip = obj.getString("ip")
            val tcpPort = obj.getInt("tcp_port")
            val udpPort = obj.getInt("udp_port")
            val token = obj.getString("token")
            val deviceName = android.os.Build.MODEL

            tvStatus.text = "Connecting..."
            Toast.makeText(this, "Connecting to $ip...", Toast.LENGTH_SHORT).show()
            
            // Networking MUST be done on a background thread in Android,
            // otherwise it throws NetworkOnMainThreadException or triggers an ANR crash.
            Thread {
                try {
                    val success = client.connect(ip, tcpPort, udpPort, token, deviceName)
                    runOnUiThread {
                        if (success) {
                            updateUI(true)
                        } else {
                            Toast.makeText(this@MainActivity, "Failed to connect to server", Toast.LENGTH_LONG).show()
                            tvStatus.text = "Not Connected"
                        }
                    }
                } catch (e: Exception) {
                    runOnUiThread {
                        Log.e("MainActivity", "Connection error", e)
                        Toast.makeText(this@MainActivity, "Connection error: ${e.message}", Toast.LENGTH_LONG).show()
                        tvStatus.text = "Not Connected"
                    }
                }
            }.start()

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
