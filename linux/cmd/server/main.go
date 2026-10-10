package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/medofekry444/UbuntuSuperDisplay/linux/internal/capture"
	"github.com/medofekry444/UbuntuSuperDisplay/linux/internal/display"
)

const usage = `UbuntuSuperDisplay — GNOME Wayland virtual display prototype

Usage: server <doctor|monitors|virtual> [flags]
  doctor    Check desktop D-Bus and GStreamer prerequisites
  monitors  Print Mutter's active monitor layout
  virtual   Hold a virtual monitor open and consume its PipeWire frames

Run inside your GNOME desktop session. Use "server virtual -h" for options.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, usage)
		return nil
	}
	switch args[0] {
	case "doctor", "monitors":
		if len(args) != 1 {
			return fmt.Errorf("%s takes no arguments", args[0])
		}
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if args[0] == "doctor" {
			return doctor(checkCtx, out)
		}
		backend, err := display.Connect()
		if err != nil {
			return err
		}
		defer backend.Close()
		state, err := backend.Monitors(checkCtx)
		if err != nil {
			return err
		}
		printState(out, state)
		return nil
	case "virtual":
		flags := flag.NewFlagSet("virtual", flag.ContinueOnError)
		flags.SetOutput(errOut)
		options := display.Options{}
		flags.IntVar(&options.Width, "width", 1920, "width in physical pixels")
		flags.IntVar(&options.Height, "height", 1200, "height in physical pixels")
		flags.IntVar(&options.FPS, "fps", 60, "requested refresh rate; achieved rate is hardware dependent")
		flags.BoolVar(&options.Platform, "platform", true, "ask Mutter to treat this as a platform monitor")
		flags.BoolVar(&options.FixedMode, "fixed-mode", false, "advertise a fixed monitor mode (requires Mutter modes support)")
		timeout := flags.Duration("startup-timeout", 20*time.Second, "maximum time to receive frames and find an extended monitor")
		duration := flags.Duration("duration", 0, "optional time to keep the display after readiness; zero waits for Ctrl+C")
		if err := flags.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() > 0 {
			return fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
		if err := options.Validate(); err != nil {
			return err
		}
		if *timeout <= 0 || *duration < 0 {
			return errors.New("startup-timeout must be positive and duration must be nonnegative")
		}
		return virtual(ctx, options, *timeout, *duration, out, errOut)
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

func doctor(ctx context.Context, out io.Writer) error {
	var failures []error
	if err := capture.Check(ctx); err != nil {
		fmt.Fprintln(out, "FAIL GStreamer:", err)
		failures = append(failures, err)
	} else {
		fmt.Fprintln(out, "PASS GStreamer: pipewiresrc and fakesink available")
	}
	backend, err := display.Connect()
	if err != nil {
		fmt.Fprintln(out, "FAIL desktop session:", err)
		return errors.Join(append(failures, err)...)
	}
	defer backend.Close()
	if version, err := backend.Version(ctx); err != nil {
		fmt.Fprintln(out, "FAIL Mutter:", err)
		failures = append(failures, err)
	} else {
		fmt.Fprintf(out, "PASS Mutter ScreenCast API version: %d (not the GNOME release number)\n", version)
	}
	if state, err := backend.Monitors(ctx); err != nil {
		fmt.Fprintln(out, "FAIL display configuration:", err)
		failures = append(failures, err)
	} else {
		printState(out, state)
	}
	fmt.Fprintln(out, "RecordVirtual and capture readiness are tested by the virtual command.")
	return errors.Join(failures...)
}

func printState(out io.Writer, state display.State) {
	fmt.Fprintf(out, "Monitor state serial %d: %d outputs, %d active logical monitors\n", state.Serial, len(state.Monitors), len(state.Logical))
	for _, logical := range state.Logical {
		for _, monitor := range logical.Monitors {
			fmt.Fprintf(out, "  %s (%s): position=(%d,%d) scale=%.2f primary=%t\n", monitor.Connector, monitor.Product, logical.X, logical.Y, logical.Scale, logical.Primary)
		}
	}
}

func virtual(ctx context.Context, options display.Options, timeout, duration time.Duration, out, errOut io.Writer) (result error) {
	startup, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := capture.Check(startup); err != nil {
		return err
	}
	backend, err := display.Connect()
	if err != nil {
		return err
	}
	defer backend.Close()
	if _, err := backend.Version(startup); err != nil {
		return err
	}
	before, err := backend.Monitors(startup)
	if err != nil {
		return err
	}
	if len(before.Logical) == 0 {
		return errors.New("no active desktop monitor; this prototype requires an existing GNOME Wayland desktop")
	}
	printState(out, before)
	session, err := backend.Create(startup, options)
	if err != nil {
		return err
	}
	defer func() {
		if err := session.Stop(); err != nil {
			fmt.Fprintln(errOut, "Session cleanup:", err)
			result = errors.Join(result, fmt.Errorf("stop virtual display: %w", err))
		}
	}()
	fmt.Fprintf(out, "Session: %s\nVirtual stream: %s\n", session.Path, session.Stream)
	node, err := session.Start(startup)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "PipeWire node: %d; negotiating %dx%d at %d FPS\n", node, options.Width, options.Height, options.FPS)
	consumer, err := capture.Start(ctx, node, options.Width, options.Height, options.FPS)
	if err != nil {
		return err
	}
	defer consumer.Close()
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	closed := make(chan error, 1)
	go func() { closed <- session.Wait(watchCtx) }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	frames := consumer.Ready
	haveFrames := false
	var lastState display.State
	for {
		select {
		case <-startup.Done():
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("virtual monitor not ready: frames=%t; active logical monitors=%d (before=%d); check PipeWire permissions and GNOME Displays in extended mode: %w", haveFrames, len(lastState.Logical), len(before.Logical), startup.Err())
		case <-consumer.Done:
			if ctx.Err() != nil {
				return nil
			}
			return consumer.Err()
		case err := <-closed:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case <-frames:
			haveFrames, frames = true, nil
			fmt.Fprintln(out, "First PipeWire video buffer received.")
		case <-ticker.C:
			lastState, err = backend.Monitors(startup)
			if err != nil {
				return err
			}
			logical, extended := display.Extension(before, lastState, options.Width, options.Height)
			if !haveFrames || !extended {
				continue
			}
			fmt.Fprintf(out, "READY: new logical monitor at (%d,%d), scale %.2f; capture is receiving frames.\n", logical.X, logical.Y, logical.Scale)
			printState(out, lastState)
			fmt.Fprintln(out, "Now drag a window into that area to confirm the desktop is usable. Ctrl+C removes the virtual display.")
			cancel() // Startup deadline no longer controls the running session.
			return hold(ctx, duration, consumer, closed, out)
		}
	}
}

func hold(ctx context.Context, duration time.Duration, consumer *capture.Process, closed <-chan error, out io.Writer) error {
	var finished <-chan time.Time
	if duration > 0 {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		finished = timer.C
	}
	select {
	case <-ctx.Done():
	case <-finished:
	case <-consumer.Done:
		if ctx.Err() == nil {
			return consumer.Err()
		}
	case err := <-closed:
		if ctx.Err() == nil {
			return err
		}
	}
	fmt.Fprintf(out, "Stopping virtual display; consumed %d buffers (not an FPS or latency benchmark).\n", consumer.Frames())
	return nil
}
