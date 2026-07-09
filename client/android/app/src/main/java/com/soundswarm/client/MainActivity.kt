package com.soundswarm.client

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
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

        // Request POST_NOTIFICATIONS on Android 13+
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            if (checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
                requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 101)
            }
        }

        // Request Ignore Battery Optimizations
        val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.M) {
            if (!pm.isIgnoringBatteryOptimizations(packageName)) {
                val intent = Intent()
                intent.action = Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS
                intent.data = Uri.parse("package:$packageName")
                startActivity(intent)
            }
        }

        client = SoundSwarmClient(this)

        tvStatus = findViewById(R.id.tvStatus)
        btnScan = findViewById(R.id.btnScan)
        btnDisconnect = findViewById(R.id.btnDisconnect)

        btnScan.setOnClickListener {
            val integrator = IntentIntegrator(this)
            integrator.setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
            integrator.setPrompt("Scan SoundSwarm QR Code")
            integrator.setOrientationLocked(false)
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

        } catch (e: org.json.JSONException) {
            Log.e("MainActivity", "Invalid QR code format", e)
            Toast.makeText(this, "Invalid QR code", Toast.LENGTH_LONG).show()
        } catch (e: Exception) {
            Log.e("MainActivity", "Connection exception", e)
            Toast.makeText(this, "Error: ${e.message}", Toast.LENGTH_LONG).show()
            tvStatus.text = "Not Connected"
        }
    }

    private fun updateUI(connected: Boolean) {
        if (connected) {
            tvStatus.text = "Connected & Synchronized"
            tvStatus.setTextColor(android.graphics.Color.GREEN)
            btnScan.visibility = View.GONE
            btnDisconnect.visibility = View.VISIBLE
            
            // Start KeepAliveService to allow background execution
            val serviceIntent = Intent(this, KeepAliveService::class.java)
            try {
                if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.O) {
                    startForegroundService(serviceIntent)
                } else {
                    startService(serviceIntent)
                }
            } catch (e: Exception) {
                Log.e("MainActivity", "Failed to start foreground service: ${e.message}")
            }
        } else {
            tvStatus.text = "Not Connected"
            tvStatus.setTextColor(android.graphics.Color.WHITE)
            btnScan.visibility = View.VISIBLE
            btnDisconnect.visibility = View.GONE
            
            // Stop KeepAliveService
            val serviceIntent = Intent(this, KeepAliveService::class.java)
            stopService(serviceIntent)
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        client.disconnect()
    }
}
