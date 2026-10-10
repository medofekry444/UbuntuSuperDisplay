package display

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	testSessionPath = dbus.ObjectPath("/org/gnome/Mutter/ScreenCast/Session/test")
	testStreamPath  = dbus.ObjectPath("/org/gnome/Mutter/ScreenCast/Stream/test")
)

type mockMutter struct {
	conn         *dbus.Conn
	mu           sync.Mutex
	props        map[string]dbus.Variant
	stopped      int
	failRecord   bool
	noNode       bool
	closeOnStart bool
}

func (m *mockMutter) CreateSession(_ map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return testSessionPath, nil
}

func (m *mockMutter) RecordVirtual(props map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.props = props
	if m.failRecord {
		return "", dbus.NewError("org.freedesktop.DBus.Error.UnknownMethod", []interface{}{"RecordVirtual unavailable"})
	}
	return testStreamPath, nil
}

func (m *mockMutter) Start() *dbus.Error {
	m.mu.Lock()
	closeOnStart, noNode := m.closeOnStart, m.noNode
	m.mu.Unlock()
	if closeOnStart {
		_ = m.conn.Emit(testSessionPath, SessionIface+".Closed")
	} else if !noNode {
		// Emitting synchronously, before the method reply, exercises the signal race.
		_ = m.conn.Emit(testStreamPath, StreamIface+".PipeWireStreamAdded", uint32(42))
	}
	return nil
}

func (m *mockMutter) Stop() *dbus.Error {
	m.mu.Lock()
	m.stopped++
	m.mu.Unlock()
	return nil
}

func (m *mockMutter) Get(_ string, _ string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant(int32(4)), nil
}

func (m *mockMutter) GetCurrentState() (uint32, []Monitor, []LogicalMonitor, map[string]dbus.Variant, *dbus.Error) {
	monitor := testMonitor("eDP-1", 1920, 1080)
	monitor.Modes[0].SupportedScale = []float64{1, 2}
	return 7, []Monitor{monitor}, []LogicalMonitor{{Scale: 1, Primary: true, Monitors: []MonitorSpec{monitor.Spec}}}, map[string]dbus.Variant{"layout-mode": dbus.MakeVariant(uint32(1))}, nil
}

func setupMock(t *testing.T) (*Backend, *mockMutter) {
	t.Helper()
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("D-Bus integration requires dbus-run-session -- go test ./...")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, name := range []string{BusName, "org.gnome.Mutter.DisplayConfig"} {
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("claim test bus name %s: %v, %v; run on an isolated test bus", name, reply, err)
		}
	}
	mock := &mockMutter{conn: conn}
	for _, target := range []struct {
		path  dbus.ObjectPath
		iface string
	}{
		{RootPath, BusName},
		{RootPath, "org.freedesktop.DBus.Properties"},
		{testSessionPath, SessionIface},
		{"/org/gnome/Mutter/DisplayConfig", "org.gnome.Mutter.DisplayConfig"},
	} {
		if err := conn.Export(mock, target.path, target.iface); err != nil {
			t.Fatal(err)
		}
	}
	backend, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend, mock
}

func TestSessionLifecycleAndWireTypes(t *testing.T) {
	backend, mock := setupMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	version, err := backend.Version(ctx)
	if err != nil || version != 4 {
		t.Fatalf("Version(): %d, %v", version, err)
	}
	state, err := backend.Monitors(ctx)
	if err != nil || len(state.Logical) != 1 || state.Monitors[0].Modes[0].Height != 1080 {
		t.Fatalf("GetCurrentState wire decoding: %+v, %v", state, err)
	}
	session, err := backend.Create(ctx, Options{Width: 1920, Height: 1200, FPS: 60, Platform: true, FixedMode: true})
	if err != nil {
		t.Fatal(err)
	}
	node, err := session.Start(ctx)
	if err != nil || node != 42 {
		t.Fatalf("Start(): %d, %v", node, err)
	}
	mock.mu.Lock()
	if mock.props["cursor-mode"].Signature().String() != "u" || mock.props["is-platform"].Value() != true || mock.props["modes"].Signature().String() != "aa{sv}" {
		t.Errorf("incorrect RecordVirtual wire types: %+v", mock.props)
	}
	mock.mu.Unlock()
	if err := mock.conn.Emit(testSessionPath, SessionIface+".Closed"); err != nil {
		t.Fatal(err)
	}
	if err := session.Wait(ctx); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed session not detected: %v", err)
	}
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.stopped != 1 {
		t.Fatalf("Stop called %d times; want exactly once", mock.stopped)
	}
}

func TestRecordVirtualFailureCleansUp(t *testing.T) {
	backend, mock := setupMock(t)
	mock.mu.Lock()
	mock.failRecord = true
	mock.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := backend.Create(ctx, Options{Width: 1920, Height: 1200, FPS: 60}); err == nil {
		t.Fatal("expected RecordVirtual error")
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.stopped != 1 {
		t.Fatal("session leaked after RecordVirtual failure")
	}
}

func TestMissingNodeTimesOutAndCanceledContextStillCleansUp(t *testing.T) {
	backend, mock := setupMock(t)
	mock.mu.Lock()
	mock.noNode = true
	mock.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	session, err := backend.Create(ctx, Options{Width: 1920, Height: 1200, FPS: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Start(ctx); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected node timeout: %v", err)
	}
	if err := session.Stop(); err != nil {
		t.Fatalf("cleanup must work after startup context expires: %v", err)
	}
}

func TestClosedDuringStart(t *testing.T) {
	backend, mock := setupMock(t)
	mock.mu.Lock()
	mock.closeOnStart = true
	mock.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := backend.Create(ctx, Options{Width: 1920, Height: 1200, FPS: 60})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop()
	if _, err := session.Start(ctx); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected early-close error: %v", err)
	}
}

func TestConnectWithoutDesktop(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if _, err := Connect(); err == nil || !strings.Contains(err.Error(), "GNOME Wayland") {
		t.Fatalf("expected actionable no-desktop error: %v", err)
	}
}
