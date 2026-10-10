package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Width, Height, FPS, Bitrate int
	TestPattern                 bool
}

func (c Config) Validate() error {
	if c.Width < 320 || c.Height < 240 || c.Width > 3840 || c.Height > 2160 || c.Width%2 != 0 || c.Height%2 != 0 {
		return errors.New("stream resolution must be even-sized, between 320x240 and 3840x2160")
	}
	if c.FPS < 1 || c.FPS > 120 || c.Bitrate < 100 || c.Bitrate > 50000 {
		return errors.New("fps must be 1..120 and bitrate must be 100..50000 kbps")
	}
	return nil
}

func Check(ctx context.Context, testPattern bool) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return err
	}
	source := "pipewiresrc"
	if testPattern {
		source = "videotestsrc"
	}
	for _, element := range []string{source, "videoconvert", "x264enc", "h264parse", "multipartmux", "fdsink"} {
		output, err := exec.CommandContext(ctx, "gst-inspect-1.0", element).CombinedOutput()
		if err != nil {
			return fmt.Errorf("GStreamer %s unavailable: %w\n%s", element, err, output)
		}
	}
	return nil
}

func Arguments(node uint32, c Config) []string {
	args := []string{"-q"}
	if c.TestPattern {
		args = append(args, "videotestsrc", "is-live=true", "pattern=ball", "!", fmt.Sprintf("video/x-raw,width=%d,height=%d,framerate=%d/1", c.Width, c.Height, c.FPS))
	} else {
		args = append(args, "pipewiresrc", "path="+strconv.FormatUint(uint64(node), 10), "do-timestamp=true", "!", fmt.Sprintf("video/x-raw,width=%d,height=%d,framerate=0/1,max-framerate=%d/1", c.Width, c.Height, c.FPS))
	}
	return append(args, "!", "videoconvert", "!", "video/x-raw,format=I420", "!",
		"x264enc", "tune=zerolatency", "speed-preset=ultrafast", "bframes=0", "aud=true", "byte-stream=true",
		"bitrate="+strconv.Itoa(c.Bitrate), "key-int-max="+strconv.Itoa(c.FPS), "!",
		"h264parse", "config-interval=-1", "!", "video/x-h264,stream-format=byte-stream,alignment=au,profile=baseline", "!",
		"multipartmux", "boundary=usdframe", "!", "fdsink", "fd=1", "sync=false")
}

type Encoder struct {
	Ready  chan struct{}
	Done   chan struct{}
	frames atomic.Uint64
	cancel context.CancelFunc
	mu     sync.Mutex
	err    error
}

func Start(ctx context.Context, node uint32, c Config, emit func([]byte) error) (*Encoder, error) {
	child, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(child, "gst-launch-1.0", Arguments(node, c)...)
	cmd.WaitDelay = 2 * time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	tail := &boundedLog{}
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pipe.Close()
		return nil, err
	}
	e := &Encoder{Ready: make(chan struct{}), Done: make(chan struct{}), cancel: cancel}
	go func() {
		var once sync.Once
		parseErr := EncodedFrames(pipe, func(frame []byte) error {
			if err := emit(frame); err != nil {
				return err
			}
			e.frames.Add(1)
			once.Do(func() { close(e.Ready) })
			return nil
		})
		cancel()
		waitErr := cmd.Wait()
		if parseErr == nil && waitErr == nil {
			parseErr = errors.New("encoder ended unexpectedly")
		}
		e.mu.Lock()
		e.err = fmt.Errorf("stream encoder stopped: %w\n%s", errors.Join(parseErr, waitErr), tail.data)
		e.mu.Unlock()
		close(e.Done)
	}()
	return e, nil
}

func (e *Encoder) Close()         { e.cancel(); <-e.Done }
func (e *Encoder) Frames() uint64 { return e.frames.Load() }
func (e *Encoder) Err() error     { e.mu.Lock(); defer e.mu.Unlock(); return e.err }

type boundedLog struct{ data []byte }

func (b *boundedLog) Write(p []byte) (int, error) {
	n := len(p)
	if n > 16384 {
		p = p[n-16384:]
	}
	b.data = append(b.data, p...)
	if len(b.data) > 16384 {
		b.data = b.data[len(b.data)-16384:]
	}
	return n, nil
}

var _ io.Writer = (*boundedLog)(nil)
