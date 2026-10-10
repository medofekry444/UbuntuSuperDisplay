package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/medofekry444/UbuntuSuperDisplay/linux/internal/display"
)

const (
	fixtureSession = dbus.ObjectPath("/org/gnome/Mutter/ScreenCast/Session/cli")
	fixtureStream  = dbus.ObjectPath("/org/gnome/Mutter/ScreenCast/Stream/cli")
)

type desktopFixture struct {
	conn    *dbus.Conn
	mu      sync.Mutex
	active  bool
	mirror  bool
	stopped int
}

func (f *desktopFixture) CreateSession(_ map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return fixtureSession, nil
}

func (f *desktopFixture) RecordVirtual(_ map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return fixtureStream, nil
}

func (f *desktopFixture) Get(_ string, _ string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant(int32(4)), nil
}

func (f *desktopFixture) Start() *dbus.Error {
	f.mu.Lock()
	f.active = true
	f.mu.Unlock()
	_ = f.conn.Emit(fixtureStream, display.StreamIface+".PipeWireStreamAdded", uint32(42))
	return nil
}

func (f *desktopFixture) Stop() *dbus.Error {
	f.mu.Lock()
	f.active = false
	f.stopped++
	f.mu.Unlock()
	return nil
}

func (f *desktopFixture) GetCurrentState() (uint32, []display.Monitor, []display.LogicalMonitor, map[string]dbus.Variant, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	laptop := display.Monitor{Spec: display.MonitorSpec{Connector: "eDP-1"}, Modes: []display.Mode{{Width: 1920, Height: 1080, SupportedScale: []float64{1}, Properties: map[string]dbus.Variant{"is-current": dbus.MakeVariant(true)}}}}
	monitors := []display.Monitor{laptop}
	logical := []display.LogicalMonitor{{Scale: 1, Primary: true, Monitors: []display.MonitorSpec{laptop.Spec}}}
	if f.active {
		virtual := display.Monitor{Spec: display.MonitorSpec{Connector: "Virtual-1", Vendor: "MetaVendor", Product: "Virtual remote monitor"}, Modes: []display.Mode{{Width: 1920, Height: 1200, SupportedScale: []float64{1}, Properties: map[string]dbus.Variant{"is-current": dbus.MakeVariant(true)}}}}
		monitors = append(monitors, virtual)
		if f.mirror {
			logical[0].Monitors = append(logical[0].Monitors, virtual.Spec)
		} else {
			logical = append(logical, display.LogicalMonitor{X: 1920, Scale: 1, Monitors: []display.MonitorSpec{virtual.Spec}})
		}
	}
	return 1, monitors, logical, map[string]dbus.Variant{}, nil
}

func cliFixture(t *testing.T, mirror bool, consumerScript string) *desktopFixture {
	t.Helper()
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("requires dbus-run-session -- go test -p 1 ./...")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	fixture := &desktopFixture{conn: conn, mirror: mirror}
	for _, name := range []string{display.BusName, "org.gnome.Mutter.DisplayConfig"} {
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("claim %s: %v, %v; use isolated bus and -p 1", name, reply, err)
		}
	}
	for _, target := range []struct {
		path  dbus.ObjectPath
		iface string
	}{
		{display.RootPath, display.BusName},
		{display.RootPath, "org.freedesktop.DBus.Properties"},
		{fixtureSession, display.SessionIface},
		{"/org/gnome/Mutter/DisplayConfig", "org.gnome.Mutter.DisplayConfig"},
	} {
		if err := conn.Export(fixture, target.path, target.iface); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	for name, body := range map[string]string{"gst-inspect-1.0": "exit 0\n", "gst-launch-1.0": consumerScript} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return fixture
}

func TestVirtualLifecycle(t *testing.T) {
	fixture := cliFixture(t, false, "printf '/GstPipeline:p/GstFakeSink:f: last-message = chain buffer\\n'\nexec /bin/sleep 60\n")
	var out, errOut bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := run(ctx, []string{"virtual", "-duration=10ms", "-startup-timeout=2s"}, &out, &errOut)
	if err != nil {
		t.Fatalf("virtual lifecycle: %v\n%s\n%s", err, &out, &errOut)
	}
	if !strings.Contains(out.String(), "READY: new logical monitor at (1920,0)") || !strings.Contains(out.String(), "consumed 1 buffers") {
		t.Fatalf("readiness or shutdown missing: %s", &out)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.active || fixture.stopped != 1 {
		t.Fatalf("virtual monitor not removed: active=%t stop count=%d", fixture.active, fixture.stopped)
	}
}

func TestVirtualRejectsMirrorDespiteFrames(t *testing.T) {
	fixture := cliFixture(t, true, "printf '/GstPipeline:p/GstFakeSink:f: last-message = chain buffer\\n'\nexec /bin/sleep 60\n")
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"virtual", "-startup-timeout=400ms"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "not ready") || !strings.Contains(err.Error(), "frames=true") {
		t.Fatalf("mirroring was accepted: %v\n%s", err, &out)
	}
	if strings.Contains(out.String(), "READY:") {
		t.Fatal("mirrored desktop reported ready")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.stopped != 1 {
		t.Fatal("timeout leaked a display session")
	}
}

func TestVirtualRejectsDisplayWithoutFrames(t *testing.T) {
	cliFixture(t, false, "echo 'Pipeline is PLAYING'\nexec /bin/sleep 60\n")
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"virtual", "-startup-timeout=400ms"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "frames=false") || strings.Contains(out.String(), "READY:") {
		t.Fatalf("nonfunctional capture reported ready: %v\n%s", err, &out)
	}
}

func TestVirtualConsumerFailureCleansUp(t *testing.T) {
	fixture := cliFixture(t, false, "echo 'permission denied' >&2\nexit 9\n")
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"virtual"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("consumer failure missing: %v", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.stopped != 1 {
		t.Fatal("consumer failure leaked a display session")
	}
}

func TestInvalidFlagsFailBeforeDesktopAccess(t *testing.T) {
	for _, args := range [][]string{
		{"virtual", "-width=0"}, {"virtual", "-fps=500"}, {"virtual", "-startup-timeout=0"},
		{"virtual", "-duration=-1s"}, {"virtual", "extra"}, {"unknown"}, {"doctor", "extra"},
	} {
		var out, errOut bytes.Buffer
		if err := run(context.Background(), args, &out, &errOut); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}
