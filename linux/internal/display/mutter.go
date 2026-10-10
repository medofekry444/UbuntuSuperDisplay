package display

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	BusName      = "org.gnome.Mutter.ScreenCast"
	RootPath     = dbus.ObjectPath("/org/gnome/Mutter/ScreenCast")
	SessionIface = "org.gnome.Mutter.ScreenCast.Session"
	StreamIface  = "org.gnome.Mutter.ScreenCast.Stream"
)

type Backend struct{ conn *dbus.Conn }

func Connect() (*Backend, error) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return nil, errors.New("DBUS_SESSION_BUS_ADDRESS is unset; run from a terminal inside your GNOME Wayland desktop, as the logged-in user")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to desktop session bus: %w", err)
	}
	return &Backend{conn: conn}, nil
}

func (b *Backend) Close() error { return b.conn.Close() }

func (b *Backend) Version(ctx context.Context) (int32, error) {
	var value dbus.Variant
	err := b.conn.Object(BusName, RootPath).
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, BusName, "Version").Store(&value)
	if err != nil {
		return 0, fmt.Errorf("Mutter ScreenCast unavailable (requires GNOME Wayland): %w", err)
	}
	version, ok := value.Value().(int32)
	if !ok {
		return 0, fmt.Errorf("unexpected ScreenCast Version type %T", value.Value())
	}
	return version, nil
}

type Options struct {
	Width, Height, FPS int
	Platform           bool
	FixedMode          bool
}

func (o Options) Validate() error {
	if o.Width < 320 || o.Width > 8192 || o.Height < 240 || o.Height > 8192 {
		return errors.New("resolution must be between 320x240 and 8192x8192")
	}
	if o.FPS < 1 || o.FPS > 120 {
		return errors.New("fps must be between 1 and 120")
	}
	return nil
}

func (o Options) properties() map[string]dbus.Variant {
	props := map[string]dbus.Variant{
		"cursor-mode": dbus.MakeVariant(uint32(1)), // Embedded for future video streaming.
		"is-platform": dbus.MakeVariant(o.Platform),
	}
	if o.FixedMode {
		props["modes"] = dbus.MakeVariant([]map[string]dbus.Variant{{
			"size":         dbus.MakeVariant(struct{ Width, Height uint32 }{uint32(o.Width), uint32(o.Height)}),
			"refresh-rate": dbus.MakeVariant(float64(o.FPS)),
			"is-preferred": dbus.MakeVariant(true),
		}})
	}
	return props
}

type Session struct {
	backend *Backend
	Path    dbus.ObjectPath
	Stream  dbus.ObjectPath
	signals chan *dbus.Signal
	matches [][]dbus.MatchOption
	once    sync.Once
	stopErr error
}

func (b *Backend) Create(ctx context.Context, options Options) (_ *Session, err error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	s := &Session{backend: b, signals: make(chan *dbus.Signal, 16)}
	if err := b.conn.Object(BusName, RootPath).
		CallWithContext(ctx, BusName+".CreateSession", 0, map[string]dbus.Variant{}).Store(&s.Path); err != nil {
		return nil, fmt.Errorf("create screencast session: %w", err)
	}
	defer func() {
		if err != nil {
			if stopErr := s.Stop(); stopErr != nil {
				err = errors.Join(err, fmt.Errorf("clean up failed session: %w", stopErr))
			}
		}
	}()
	// Subscribe before RecordVirtual/Start so an immediate signal is not lost.
	b.conn.Signal(s.signals)
	closedMatch := []dbus.MatchOption{dbus.WithMatchSender(BusName), dbus.WithMatchObjectPath(s.Path), dbus.WithMatchInterface(SessionIface), dbus.WithMatchMember("Closed")}
	if err = s.addMatch(ctx, closedMatch); err != nil {
		return nil, err
	}
	if err = b.conn.Object(BusName, s.Path).
		CallWithContext(ctx, SessionIface+".RecordVirtual", 0, options.properties()).Store(&s.Stream); err != nil {
		return nil, fmt.Errorf("RecordVirtual failed; check Mutter support and permissions: %w", err)
	}
	streamMatch := []dbus.MatchOption{dbus.WithMatchSender(BusName), dbus.WithMatchObjectPath(s.Stream), dbus.WithMatchInterface(StreamIface), dbus.WithMatchMember("PipeWireStreamAdded")}
	if err = s.addMatch(ctx, streamMatch); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Session) addMatch(ctx context.Context, match []dbus.MatchOption) error {
	if err := s.backend.conn.AddMatchSignalContext(ctx, match...); err != nil {
		return fmt.Errorf("subscribe to Mutter signals: %w", err)
	}
	s.matches = append(s.matches, match)
	return nil
}

func (s *Session) Start(ctx context.Context) (uint32, error) {
	if err := s.backend.conn.Object(BusName, s.Path).CallWithContext(ctx, SessionIface+".Start", 0).Err; err != nil {
		return 0, fmt.Errorf("start screencast session: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("wait for PipeWire node: %w", ctx.Err())
		case <-s.backend.conn.Context().Done():
			return 0, errors.New("desktop session bus disconnected")
		case signal := <-s.signals:
			if signal.Name == SessionIface+".Closed" && signal.Path == s.Path {
				return 0, errors.New("Mutter closed the session before creating a PipeWire node")
			}
			if signal.Name != StreamIface+".PipeWireStreamAdded" || signal.Path != s.Stream {
				continue
			}
			if len(signal.Body) != 1 {
				return 0, errors.New("invalid PipeWireStreamAdded signal body")
			}
			id, ok := signal.Body[0].(uint32)
			if !ok || id == 0 {
				return 0, errors.New("invalid PipeWire node ID")
			}
			return id, nil
		}
	}
}

func (s *Session) Wait(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.backend.conn.Context().Done():
			return errors.New("desktop session bus disconnected")
		case signal := <-s.signals:
			if signal.Name == SessionIface+".Closed" && signal.Path == s.Path {
				return errors.New("Mutter closed the virtual display session")
			}
		}
	}
}

// Use a fresh bounded context: callers normally stop after their run context is canceled.
func (s *Session) Stop() error {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.stopErr = s.backend.conn.Object(BusName, s.Path).CallWithContext(ctx, SessionIface+".Stop", 0).Err
		for _, match := range s.matches {
			if err := s.backend.conn.RemoveMatchSignalContext(ctx, match...); err != nil && s.stopErr == nil {
				s.stopErr = err
			}
		}
		s.backend.conn.RemoveSignal(s.signals)
	})
	return s.stopErr
}
