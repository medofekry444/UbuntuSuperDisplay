package org.medofekry.ubuntusuperdisplay

import java.io.DataInputStream
import java.nio.charset.StandardCharsets

object StreamProtocol {
    const val MAX_FRAME = 8 * 1024 * 1024
    data class Header(val width: Int, val height: Int, val fps: Int)

    fun readHeader(input: DataInputStream): Header {
        val magic = ByteArray(8)
        input.readFully(magic)
        require(String(magic, StandardCharsets.US_ASCII) == "USDS0001") { "Unsupported server protocol" }
        val header = Header(input.readInt(), input.readInt(), input.readInt())
        require(header.width in 320..3840 && header.height in 240..2160 && header.width % 2 == 0 && header.height % 2 == 0 && header.fps in 1..120) { "Unsupported video dimensions or FPS" }
        return header
    }

    fun readFrame(input: DataInputStream): ByteArray {
        val size = input.readInt()
        require(size in 1..MAX_FRAME) { "Invalid video frame length" }
        return ByteArray(size).also { input.readFully(it) }
    }

    // Return Annex B parameter sets, including start codes, for MediaCodec CSD.
    fun parameterSets(frame: ByteArray): Pair<ByteArray, ByteArray>? {
        val starts = mutableListOf<Pair<Int, Int>>()
        var i = 0
        while (i + 3 < frame.size) {
            val prefix = when {
                frame[i] == 0.toByte() && frame[i + 1] == 0.toByte() && frame[i + 2] == 1.toByte() -> 3
                i + 4 < frame.size && frame[i] == 0.toByte() && frame[i + 1] == 0.toByte() && frame[i + 2] == 0.toByte() && frame[i + 3] == 1.toByte() -> 4
                else -> 0
            }
            if (prefix != 0) { starts.add(i to prefix); i += prefix } else i++
        }
        var sps: ByteArray? = null
        var pps: ByteArray? = null
        starts.forEachIndexed { index, (start, prefix) ->
            val end = if (index + 1 < starts.size) starts[index + 1].first else frame.size
            when (frame[start + prefix].toInt() and 31) {
                7 -> sps = frame.copyOfRange(start, end)
                8 -> pps = frame.copyOfRange(start, end)
            }
        }
        return if (sps != null && pps != null) sps!! to pps!! else null
    }
}
