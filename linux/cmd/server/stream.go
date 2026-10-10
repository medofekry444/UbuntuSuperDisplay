package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/medofekry444/UbuntuSuperDisplay/linux/internal/display"
	"github.com/medofekry444/UbuntuSuperDisplay/linux/internal/stream"
)

func streamCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("stream", flag.ContinueOnError)
	flags.SetOutput(errOut)
	c := stream.Config{}
	flags.IntVar(&c.Width, "width", 1920, "video width in pixels")
	flags.IntVar(&c.Height, "height", 1200, "video height in pixels")
	flags.IntVar(&c.FPS, "fps", 30, "maximum FPS; software encoding first")
	flags.IntVar(&c.Bitrate, "bitrate", 8000, "H.264 bitrate in kbps")
	flags.BoolVar(&c.TestPattern, "test-pattern", false, "stream a moving ball without a GNOME session")
	listen := flags.String("listen", "127.0.0.1:8765", "single-client TCP address; set laptop LAN IP for Wi-Fi")
	fixed := flags.Bool("fixed-mode", false, "advertise an explicit Mutter monitor mode")
	timeout := flags.Duration("startup-timeout", 30*time.Second, "startup deadline after the client connects")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("stream takes no positional arguments")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if *timeout <= 0 {
		return errors.New("startup-timeout must be positive")
	}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := stream.Check(check, c.TestPattern)
	cancel()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	watch, stop := context.WithCancel(ctx)
	defer stop()
	go func() { <-watch.Done(); _ = listener.Close() }()
	fmt.Fprintf(out, "Listening on %s; waiting for one Android client.\n", listener.Addr())
	conn, err := listener.Accept()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer conn.Close()
	go func() { <-watch.Done(); _ = conn.Close() }()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	fmt.Fprintf(out, "Client connected: %s\n", conn.RemoteAddr())
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := stream.WriteHeader(conn, stream.Header{Width: uint32(c.Width), Height: uint32(c.Height), FPS: uint32(c.FPS)}); err != nil {
		return err
	}
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	go func() {
		// The client sends no application bytes. Reading detects an idle client's
		// disconnect immediately, even when the desktop isn't producing frames.
		var one [1]byte
		_, _ = conn.Read(one[:])
		stopStream()
	}()
	return serveStream(streamCtx, conn, c, *fixed, *timeout, out, errOut)
}

func serveStream(ctx context.Context, conn net.Conn, c stream.Config, fixed bool, timeout time.Duration, out, errOut io.Writer) (result error) {
	startup, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var backend *display.Backend
	var session *display.Session
	var before display.State
	var node uint32
	var closed <-chan error
	watch, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	if !c.TestPattern {
		var err error
		backend, err = display.Connect()
		if err != nil {
			return err
		}
		defer backend.Close()
		before, err = backend.Monitors(startup)
		if err != nil {
			return err
		}
		if len(before.Logical) == 0 {
			return errors.New("an existing GNOME desktop monitor is required")
		}
		session, err = backend.Create(startup, display.Options{Width: c.Width, Height: c.Height, FPS: c.FPS, Platform: true, FixedMode: fixed})
		if err != nil {
			return err
		}
		defer func() {
			if err := session.Stop(); err != nil {
				result = errors.Join(result, fmt.Errorf("stop virtual monitor: %w", err))
			}
		}()
		node, err = session.Start(startup)
		if err != nil {
			return err
		}
		ch := make(chan error, 1)
		closed = ch
		go func() { ch <- session.Wait(watch) }()
	}
	encoder, err := stream.Start(ctx, node, c, func(frame []byte) error {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return stream.WriteFrame(conn, frame)
	})
	if err != nil {
		return err
	}
	defer encoder.Close()
	frames := encoder.Ready
	haveFrames := false
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case <-startup.Done():
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("stream startup timed out (encoded frames=%t): %w", haveFrames, startup.Err())
		case <-encoder.Done:
			if ctx.Err() != nil {
				return nil
			}
			return encoder.Err()
		case err := <-closed:
			return err
		case <-frames:
			haveFrames = true
			frames = nil
			fmt.Fprintln(out, "First H.264 access unit sent.")
			if c.TestPattern {
				ready = true
			}
		case <-ticker.C:
			if backend == nil || !haveFrames {
				continue
			}
			after, err := backend.Monitors(startup)
			if err != nil {
				return err
			}
			if logical, ok := display.Extension(before, after, c.Width, c.Height); ok {
				fmt.Fprintf(out, "Extended monitor at (%d,%d).\n", logical.X, logical.Y)
				ready = true
			}
		}
	}
	cancel()
	fmt.Fprintln(out, "STREAMING: H.264 is being sent. Confirm playback on the tablet; Ctrl+C disconnects.")
	select {
	case <-ctx.Done():
		fmt.Fprintf(out, "Stopping; sent %d access units.\n", encoder.Frames())
		return nil
	case <-encoder.Done:
		if ctx.Err() != nil {
			return nil
		}
		return encoder.Err()
	case err := <-closed:
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
}
