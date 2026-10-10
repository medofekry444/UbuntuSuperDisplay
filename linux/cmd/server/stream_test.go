package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/medofekry444/UbuntuSuperDisplay/linux/internal/stream"
)

type readinessWriter struct {
	ready chan struct{}
	once  sync.Once
}

func (w *readinessWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "STREAMING:") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func TestStreamDesktopLifecycle(t *testing.T) {
	fixture := cliFixture(t, false, "printf '%b' '--usdframe\\r\\nContent-Type: video/x-h264\\r\\nContent-Length: 5\\r\\n\\r\\n\\000\\000\\001\\145\\001'\nexec /bin/sleep 60\n")
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	w := &readinessWriter{ready: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		finished <- serveStream(ctx, server, stream.Config{Width: 1920, Height: 1200, FPS: 30, Bitrate: 8000}, false, 2*time.Second, w, io.Discard)
	}()
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var length [4]byte
	if _, err := io.ReadFull(client, length[:]); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(length[:]) != 5 {
		t.Fatal("bad frame length")
	}
	frame := make([]byte, 5)
	if _, err := io.ReadFull(client, frame); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.ready:
	case <-ctx.Done():
		t.Fatal("desktop stream didn't become ready")
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.active || fixture.stopped != 1 {
		t.Fatal("stream leaked virtual monitor")
	}
}
