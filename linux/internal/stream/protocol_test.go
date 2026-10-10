package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type fragmented struct{ reader io.Reader }

func (f fragmented) Read(p []byte) (int, error) { return f.reader.Read(p[:1]) }

func part(payload []byte) []byte {
	return append([]byte(fmt.Sprintf("--usdframe\r\nContent-Type: video/x-h264\r\nContent-Length: %d\r\n\r\n", len(payload))), payload...)
}

func TestMultipartFragmentation(t *testing.T) {
	first := []byte{0, 0, 0, 1, 9, 16, 0, 0, 1, 7, 1, 0, 0, 1, 8, 2, 0, 0, 1, 5, 3}
	second := []byte{0, 0, 1, 9, 16, 0, 0, 1, 1, 4}
	input := append(part(first), []byte("\r\n")...)
	input = append(input, part(second)...)
	var got [][]byte
	err := EncodedFrames(fragmented{bytes.NewReader(input)}, func(frame []byte) error { got = append(got, frame); return nil })
	if err != nil || len(got) != 2 || !bytes.Equal(got[0], first) || !bytes.Equal(got[1], second) {
		t.Fatalf("frames corrupted: %v %x", err, got)
	}
}

func TestSingleFrameEmitsBeforeNextFrameOrEOF(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	sent := make(chan struct{})
	finished := make(chan error, 1)
	stop := errors.New("test stop")
	go func() { finished <- EncodedFrames(r, func([]byte) error { close(sent); return stop }) }()
	go func() { _, _ = w.Write(part([]byte{0, 0, 1, 5, 1})) }()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("frame requires another screen update")
	}
	if err := <-finished; !errors.Is(err, stop) {
		t.Fatal(err)
	}
}

func TestMalformedEncoderFrames(t *testing.T) {
	for _, input := range []string{
		"--wrong\r\n",
		"--usdframe\r\nContent-Type: video/x-h264\r\nContent-Length: 8388609\r\n\r\n",
		"--usdframe\r\nContent-Type: video/x-h264\r\nContent-Length: -1\r\n\r\n",
		"--usdframe\r\nContent-Type: video/x-h264\r\nContent-Length: 5\r\n\r\nabc",
		"--usdframe\r\nContent-Type: video/x-h264\r\nContent-Length: 1\r\nContent-Length: 1\r\n\r\na",
		strings.Repeat("x", 5000) + "\n",
	} {
		if err := EncodedFrames(strings.NewReader(input), func([]byte) error { return nil }); err == nil {
			t.Errorf("malformed input accepted: %.80q", input)
		}
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return w.Buffer.Write(p)
}

func TestWireProtocolAndShortWrites(t *testing.T) {
	w := &shortWriter{}
	if err := WriteHeader(w, Header{1920, 1200, 30}); err != nil {
		t.Fatal(err)
	}
	frame := []byte{0, 0, 1, 5, 7}
	if err := WriteFrame(w, frame); err != nil {
		t.Fatal(err)
	}
	data := w.Bytes()
	if string(data[:8]) != Magic || binary.BigEndian.Uint32(data[8:12]) != 1920 || binary.BigEndian.Uint32(data[12:16]) != 1200 || binary.BigEndian.Uint32(data[16:20]) != 30 || binary.BigEndian.Uint32(data[20:24]) != uint32(len(frame)) || !bytes.Equal(data[24:], frame) {
		t.Fatalf("bad wire bytes: %x", data)
	}
	if err := WriteFrame(io.Discard, nil); err == nil {
		t.Fatal("empty frame allowed")
	}
	if err := WriteFrame(io.Discard, make([]byte, MaxFrame+1)); err == nil {
		t.Fatal("oversize frame allowed")
	}
}

func TestNativeEncoder(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("GStreamer unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Check(ctx, true); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	count := 0
	stop := errors.New("collected 5 frames")
	e, err := Start(ctx, 0, Config{Width: 320, Height: 240, FPS: 30, Bitrate: 500, TestPattern: true}, func(frame []byte) error {
		output.Write(frame)
		count++
		if count == 5 {
			return stop
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	select {
	case <-e.Done:
	case <-ctx.Done():
		t.Fatal("native encoder stalled")
	}
	if count != 5 || !errors.Is(e.Err(), stop) {
		t.Fatalf("native encoding failed: frames=%d error=%v", count, e.Err())
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("encoded buffers received; ffmpeg decode check unavailable")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "h264", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(output.Bytes())
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real encoded frames don't decode: %v\n%s", err, result)
	}
}
