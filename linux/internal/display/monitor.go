package display

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// These structs follow Mutter 50.1's GetCurrentState wire signature.
type MonitorSpec struct {
	Connector string
	Vendor    string
	Product   string
	Serial    string
}

type Mode struct {
	ID             string
	Width          int32
	Height         int32
	RefreshRate    float64
	PreferredScale float64
	SupportedScale []float64
	Properties     map[string]dbus.Variant
}

type Monitor struct {
	Spec       MonitorSpec
	Modes      []Mode
	Properties map[string]dbus.Variant
}

type LogicalMonitor struct {
	X          int32
	Y          int32
	Scale      float64
	Transform  uint32
	Primary    bool
	Monitors   []MonitorSpec
	Properties map[string]dbus.Variant
}

type State struct {
	Serial     uint32
	Monitors   []Monitor
	Logical    []LogicalMonitor
	Properties map[string]dbus.Variant
}

func (b *Backend) Monitors(ctx context.Context) (State, error) {
	var state State
	err := b.conn.Object("org.gnome.Mutter.DisplayConfig", "/org/gnome/Mutter/DisplayConfig").
		CallWithContext(ctx, "org.gnome.Mutter.DisplayConfig.GetCurrentState", 0).
		Store(&state.Serial, &state.Monitors, &state.Logical, &state.Properties)
	if err != nil {
		return state, fmt.Errorf("read Mutter monitor state: %w", err)
	}
	return state, nil
}

// Extension requires a new active monitor at the requested size, occupying a
// logical display of its own. A new PipeWire node or a mirrored output isn't enough.
func Extension(before, after State, width, height int) (LogicalMonitor, bool) {
	known := make(map[MonitorSpec]bool)
	for _, monitor := range before.Monitors {
		known[monitor.Spec] = true
	}
	if len(before.Logical) == 0 || len(after.Logical) <= len(before.Logical) {
		return LogicalMonitor{}, false
	}
	for _, monitor := range after.Monitors {
		if known[monitor.Spec] {
			continue
		}
		// Correlate with Mutter's virtual stream backend, not an unrelated
		// physical output hot-plugged during startup.
		if monitor.Spec.Vendor != "MetaVendor" || monitor.Spec.Product != "Virtual remote monitor" {
			continue
		}
		matchingMode := false
		for _, mode := range monitor.Modes {
			current, _ := mode.Properties["is-current"].Value().(bool)
			if current && int(mode.Width) == width && int(mode.Height) == height {
				matchingMode = true
			}
		}
		if !matchingMode {
			continue
		}
		for _, logical := range after.Logical {
			if len(logical.Monitors) == 1 && logical.Monitors[0] == monitor.Spec {
				return logical, true
			}
		}
	}
	return LogicalMonitor{}, false
}
