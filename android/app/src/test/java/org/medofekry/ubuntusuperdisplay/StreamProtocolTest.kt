package org.medofekry.ubuntusuperdisplay

import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.EOFException

class StreamProtocolTest {
    private fun header(magic: String = "USDS0001", width: Int = 1920): ByteArray {
        val out = ByteArrayOutputStream()
        DataOutputStream(out).use { it.writeBytes(magic); it.writeInt(width); it.writeInt(1200); it.writeInt(30) }
        return out.toByteArray()
    }
    @Test fun readsGoWireHeaderAndFrame() {
        val out = ByteArrayOutputStream()
        out.write(header())
        DataOutputStream(out).apply { writeInt(4); write(byteArrayOf(0, 0, 1, 5)) }
        val input = DataInputStream(ByteArrayInputStream(out.toByteArray()))
        assertEquals(StreamProtocol.Header(1920, 1200, 30), StreamProtocol.readHeader(input))
        assertArrayEquals(byteArrayOf(0, 0, 1, 5), StreamProtocol.readFrame(input))
    }
    @Test(expected = IllegalArgumentException::class) fun rejectsProtocolMismatch() { StreamProtocol.readHeader(DataInputStream(ByteArrayInputStream(header("BAD00001")))) }
    @Test(expected = IllegalArgumentException::class) fun rejectsDimensions() { StreamProtocol.readHeader(DataInputStream(ByteArrayInputStream(header(width = 99999)))) }
    @Test(expected = IllegalArgumentException::class) fun rejectsOversizeFrame() { StreamProtocol.readFrame(DataInputStream(ByteArrayInputStream(byteArrayOf(0, -128, 0, 1)))) }
    @Test(expected = EOFException::class) fun rejectsTruncatedFrame() { StreamProtocol.readFrame(DataInputStream(ByteArrayInputStream(byteArrayOf(0, 0, 0, 4, 1)))) }
    @Test fun extractsBothParameterSetsWithMixedPrefixes() {
        val frame = byteArrayOf(0,0,0,1,9,16, 0,0,0,1,0x67,1,2, 0,0,1,0x68,3, 0,0,1,0x65,4)
        val sets = StreamProtocol.parameterSets(frame)!!
        assertArrayEquals(byteArrayOf(0,0,0,1,0x67,1,2), sets.first)
        assertArrayEquals(byteArrayOf(0,0,1,0x68,3), sets.second)
        assertNull(StreamProtocol.parameterSets(byteArrayOf(0,0,1,0x65,4)))
    }
}
