package org.medofekry.ubuntusuperdisplay

import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Build
import android.view.Surface
import java.io.BufferedInputStream
import java.io.DataInputStream
import java.net.InetSocketAddress
import java.net.Socket
import java.nio.ByteBuffer
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

class StreamSession(
    private val host: String,
    private val port: Int,
    private val surface: Surface,
    private val onHeader: (StreamProtocol.Header) -> Unit,
    private val onPlaying: () -> Unit,
    private val onFinished: (String) -> Unit,
) {
    private val active = AtomicBoolean(true)
    private val socket = Socket()
    private val frames = ArrayBlockingQueue<ByteArray>(3)

    fun stop() {
        active.set(false)
        runCatching { socket.close() }
    }

    fun start() {
        Thread({ receive() }, "usd-network").start()
    }

    private fun receive() {
        var decoder: Thread? = null
        var message = "Disconnected"
        try {
            socket.tcpNoDelay = true
            socket.connect(InetSocketAddress(host, port), 5000)
            val input = DataInputStream(BufferedInputStream(socket.getInputStream(), 64 * 1024))
            val header = StreamProtocol.readHeader(input)
            onHeader(header)
            decoder = Thread({ decode(header) }, "usd-decoder").also { it.start() }
            while (active.get()) {
                val frame = StreamProtocol.readFrame(input)
                // Backpressure preserves predictive frames; do not silently drop P-frames.
                while (active.get() && !frames.offer(frame, 100, TimeUnit.MILLISECONDS)) { }
            }
        } catch (e: Exception) {
            if (active.get()) message = "Connection ended: ${e.message ?: e.javaClass.simpleName}"
        } finally {
            stop()
            decoder?.join()
            onFinished(message)
        }
    }

    private fun decode(header: StreamProtocol.Header) {
        var codec: MediaCodec? = null
        try {
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(15)
            var initial: ByteArray? = null
            while (active.get() && initial == null && System.nanoTime() < deadline) {
                initial = frames.poll(100, TimeUnit.MILLISECONDS)
            }
            if (!active.get()) return
            val first = initial ?: error("No video received")
            val (sps, pps) = StreamProtocol.parameterSets(first) ?: error("Missing H.264 SPS/PPS")
            val format = MediaFormat.createVideoFormat("video/avc", header.width, header.height)
            format.setByteBuffer("csd-0", ByteBuffer.wrap(sps))
            format.setByteBuffer("csd-1", ByteBuffer.wrap(pps))
            format.setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, StreamProtocol.MAX_FRAME)
            if (Build.VERSION.SDK_INT >= 30) format.setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
            codec = MediaCodec.createDecoderByType("video/avc")
            codec.configure(format, surface, null, 0)
            codec.start()
            var pending: ByteArray? = first
            val info = MediaCodec.BufferInfo()
            var playing = false
            while (active.get()) {
                if (pending == null) pending = frames.poll(10, TimeUnit.MILLISECONDS)
                if (pending != null) {
                    val index = codec.dequeueInputBuffer(5000)
                    if (index >= 0) {
                        val buffer = codec.getInputBuffer(index) ?: error("Decoder has no input buffer")
                        buffer.clear()
                        require(buffer.remaining() >= pending.size) { "Decoder input buffer too small; reduce resolution/bitrate" }
                        buffer.put(pending)
                        codec.queueInputBuffer(index, 0, pending.size, System.nanoTime() / 1000, 0)
                        pending = null
                    }
                }
                while (active.get()) {
                    val index = codec.dequeueOutputBuffer(info, 0)
                    if (index >= 0) {
                        codec.releaseOutputBuffer(index, true)
                        if (!playing) { playing = true; onPlaying() }
                    } else if (index == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                        continue
                    } else break
                }
            }
        } catch (e: Exception) {
            if (active.get()) onFinished("Playback failed: ${e.message ?: e.javaClass.simpleName}")
            stop()
        } finally {
            codec?.let { runCatching { it.stop() }; it.release() }
        }
    }
}
