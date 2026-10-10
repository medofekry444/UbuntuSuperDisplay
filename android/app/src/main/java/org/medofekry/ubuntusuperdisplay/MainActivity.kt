package org.medofekry.ubuntusuperdisplay

import android.app.Activity
import android.os.Bundle
import android.view.Gravity
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.View
import android.view.WindowManager
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import android.text.InputType

class MainActivity : Activity(), SurfaceHolder.Callback {
    private lateinit var video: SurfaceView
    private lateinit var root: FrameLayout
    private lateinit var controls: LinearLayout
    private lateinit var status: TextView
    private lateinit var host: EditText
    private lateinit var port: EditText
    private lateinit var connect: Button
    private var session: StreamSession? = null
    private var surfaceReady = false

    @Suppress("DEPRECATION")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_FULLSCREEN or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
        root = FrameLayout(this)
        root.setBackgroundColor(0xff000000.toInt())
        video = SurfaceView(this)
        video.holder.addCallback(this)
        root.addView(video, FrameLayout.LayoutParams(-1, -1, Gravity.CENTER))
        controls = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(24, 12, 24, 12)
            setBackgroundColor(0xdd222222.toInt())
        }
        status = TextView(this).apply { text = "Connect to your laptop's local IP address"; setTextColor(-1) }
        val preferences = getPreferences(MODE_PRIVATE)
        host = EditText(this).apply { hint = "Laptop IP address"; setSingleLine(); setText(preferences.getString("host", "")); inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI }
        port = EditText(this).apply { hint = "Port"; setSingleLine(); setText(preferences.getString("port", "8765")); inputType = InputType.TYPE_CLASS_NUMBER }
        connect = Button(this).apply { text = "Connect"; setOnClickListener { connect() } }
        val disconnect = Button(this).apply { text = "Disconnect"; setOnClickListener { disconnect() } }
        listOf(status, host, port, connect, disconnect).forEach { controls.addView(it) }
        root.addView(controls, FrameLayout.LayoutParams(-1, -2, Gravity.TOP))
        video.setOnClickListener { controls.visibility = if (controls.visibility == View.VISIBLE) View.GONE else View.VISIBLE }
        setContentView(root)
    }

    private fun connect() {
        val address = host.text.toString().trim()
        val number = port.text.toString().toIntOrNull()
        if (address.isEmpty() || number == null || number !in 1..65535) { status.text = "Enter a laptop address and valid port"; return }
        if (!surfaceReady) { status.text = "Display surface is not ready"; return }
        if (session != null) return
        getPreferences(MODE_PRIVATE).edit().putString("host", address).putString("port", number.toString()).apply()
        connect.isEnabled = false
        status.text = "Connecting…"
        lateinit var current: StreamSession
        current = StreamSession(address, number, video.holder.surface,
            onHeader = { header -> runOnUiThread {
                if (session === current) {
                    status.text = "Connected: ${header.width}×${header.height}. Waiting for video…"
                    val scale = minOf(root.width.toFloat() / header.width, root.height.toFloat() / header.height)
                    video.layoutParams = FrameLayout.LayoutParams((header.width * scale).toInt(), (header.height * scale).toInt(), Gravity.CENTER)
                }
            } },
            onPlaying = { runOnUiThread { if (session === current) { status.text = "Playing. Tap the screen for controls."; controls.visibility = View.GONE } } },
            onFinished = { message -> runOnUiThread { if (session === current) { session = null; connect.isEnabled = true; controls.visibility = View.VISIBLE; status.text = message } } },
        )
        session = current
        current.start()
    }

    private fun disconnect() {
        session?.stop()
        session = null
        connect.isEnabled = true
        controls.visibility = View.VISIBLE
        status.text = "Disconnected"
    }

    override fun surfaceCreated(holder: SurfaceHolder) { surfaceReady = true }
    override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) { }
    override fun surfaceDestroyed(holder: SurfaceHolder) { surfaceReady = false; disconnect() }
    override fun onStop() { disconnect(); super.onStop() }
}
