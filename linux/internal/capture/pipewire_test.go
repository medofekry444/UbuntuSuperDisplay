package capture

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestFrameReadinessRequiresBuffer(t *testing.T) {
	p := &Process{Ready: make(chan struct{})}
	w := &frameWriter{process: p}
	_, _ = w.Write([]byte("Pipeline is PLAYING\n/GstPipeline:p/GstFakeSink:f: last-message = event foo\n"))
	select {
	case <-p.Ready:
		t.Fatal("state transitions and events must not establish capture readiness")
	default:
	}
	_, _ = w.Write([]byte("/GstPipeline:p/GstFakeSink:f: last-message = ch"))
	_, _ = w.Write([]byte("ain buffer\n/GstPipeline:p/GstFakeSink:f: last-message = chain buffer\n"))
	select {
	case <-p.Ready:
	default:
		t.Fatal("actual captured buffers did not establish readiness")
	}
	if p.Frames() != 2 {
		t.Fatalf("frame count = %d; want 2", p.Frames())
	}
}

func TestConsumerFailurePreservesDiagnostics(t *testing.T) {
	p, err := startCommand(context.Background(), "sh", []string{"-c", "echo 'PipeWire permission denied' >&2; exit 9"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	select {
	case <-p.Done:
		if !strings.Contains(p.Err().Error(), "exit status 9") || !strings.Contains(p.Err().Error(), "permission denied") {
			t.Fatalf("lost consumer diagnostics: %v", p.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("consumer exit was not reported")
	}
}

func TestConsumerCancellationReapsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, err := startCommand(ctx, "sleep", []string{"60"})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case <-p.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not exit after cancellation")
	}
	p.Close()
}

func TestNativeGStreamerBuffers(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("native GStreamer tools unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := startCommand(ctx, "gst-launch-1.0", []string{"-v", "videotestsrc", "num-buffers=3", "!", "fakesink", "sync=false", "silent=false"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	select {
	case <-p.Done:
		if p.exitErr != nil {
			t.Fatalf("native pipeline failed: %v", p.Err())
		}
		if p.Frames() != 3 {
			t.Fatalf("expected 3 real GStreamer buffers, received %d; %v", p.Frames(), p.Err())
		}
	case <-ctx.Done():
		t.Fatal("native GStreamer pipeline did not finish")
	}
}
