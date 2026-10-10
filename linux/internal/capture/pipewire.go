package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func Check(ctx context.Context) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return errors.New("gst-launch-1.0 missing; install gstreamer1.0-tools and gstreamer1.0-pipewire")
	}
	for _, plugin := range []string{"pipewiresrc", "fakesink"} {
		output, err := exec.CommandContext(ctx, "gst-inspect-1.0", plugin).CombinedOutput()
		if err != nil {
			return fmt.Errorf("GStreamer element %s unavailable: %w\n%s", plugin, err, output)
		}
	}
	return nil
}

func Arguments(node uint32, width, height, fps int) []string {
	return []string{"-v", "pipewiresrc", "path=" + strconv.FormatUint(uint64(node), 10), "do-timestamp=true", "!",
		fmt.Sprintf("video/x-raw,width=%d,height=%d,framerate=0/1,max-framerate=%d/1", width, height, fps), "!",
		"fakesink", "sync=false", "silent=false"}
}

type Process struct {
	Ready   chan struct{}
	Done    chan struct{}
	frames  atomic.Uint64
	cancel  context.CancelFunc
	mu      sync.Mutex
	err     error
	exitErr error
}

func Start(ctx context.Context, node uint32, width, height, fps int) (*Process, error) {
	return startCommand(ctx, "gst-launch-1.0", Arguments(node, width, height, fps))
}

func startCommand(ctx context.Context, executable string, args []string) (*Process, error) {
	childCtx, cancel := context.WithCancel(ctx)
	p := &Process{Ready: make(chan struct{}), Done: make(chan struct{}), cancel: cancel}
	cmd := exec.CommandContext(childCtx, executable, args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = &frameWriter{process: p}
	stderr := &tailWriter{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start PipeWire consumer: %w", err)
	}
	go func() {
		exitErr := cmd.Wait()
		err := exitErr
		if err == nil {
			err = errors.New("PipeWire consumer exited unexpectedly")
		}
		p.mu.Lock()
		p.exitErr = exitErr
		p.err = fmt.Errorf("PipeWire consumer stopped: %w\n%s", err, stderr.data)
		p.mu.Unlock()
		close(p.Done)
	}()
	return p, nil
}

func (p *Process) Frames() uint64 { return p.frames.Load() }

func (p *Process) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *Process) Close() {
	p.cancel()
	<-p.Done
}

// gst-launch -v publishes fakesink's last-message property for every buffer.
// Count actual buffers, discarding the verbose stream rather than retaining it.
type frameWriter struct {
	process *Process
	pending []byte
	once    sync.Once
}

func (w *frameWriter) Write(data []byte) (int, error) {
	n := len(data)
	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			w.pending = append(w.pending, data...)
			if len(w.pending) > 65536 {
				w.pending = nil
			}
			break
		}
		w.pending = append(w.pending, data[:idx]...)
		line := string(w.pending)
		if strings.Contains(line, "GstFakeSink:") && strings.Contains(line, "last-message = chain") {
			w.process.frames.Add(1)
			w.once.Do(func() { close(w.process.Ready) })
		}
		w.pending = nil
		data = data[idx+1:]
	}
	return n, nil
}

type tailWriter struct{ data []byte }

func (w *tailWriter) Write(p []byte) (int, error) {
	const max = 16 * 1024
	n := len(p)
	if n >= max {
		w.data = append(w.data[:0], p[n-max:]...)
	} else {
		w.data = append(w.data, p...)
		if len(w.data) > max {
			w.data = w.data[len(w.data)-max:]
		}
	}
	return n, nil
}

var _ io.Writer = (*frameWriter)(nil)
