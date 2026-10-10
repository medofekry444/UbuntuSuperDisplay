package display

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func testMonitor(connector string, width, height int32) Monitor {
	spec := MonitorSpec{Connector: connector}
	if connector == "Virtual-1" {
		spec.Vendor, spec.Product = "MetaVendor", "Virtual remote monitor"
	}
	return Monitor{Spec: spec, Modes: []Mode{{Width: width, Height: height,
		Properties: map[string]dbus.Variant{"is-current": dbus.MakeVariant(true)}}}}
}

func TestExtension(t *testing.T) {
	laptop := testMonitor("eDP-1", 1920, 1080)
	virtual := testMonitor("Virtual-1", 1920, 1200)
	laptopLogical := LogicalMonitor{Scale: 1, Primary: true, Monitors: []MonitorSpec{laptop.Spec}}
	virtualLogical := LogicalMonitor{X: 1920, Scale: 1, Monitors: []MonitorSpec{virtual.Spec}}
	before := State{Monitors: []Monitor{laptop}, Logical: []LogicalMonitor{laptopLogical}}
	for _, test := range []struct {
		name  string
		after State
		want  bool
	}{
		{"extended", State{Monitors: []Monitor{laptop, virtual}, Logical: []LogicalMonitor{laptopLogical, virtualLogical}}, true},
		{"stream-only", before, false},
		{"inactive-output", State{Monitors: []Monitor{laptop, virtual}, Logical: []LogicalMonitor{laptopLogical}}, false},
		{"mirrored", State{Monitors: []Monitor{laptop, virtual}, Logical: []LogicalMonitor{{Scale: 1, Monitors: []MonitorSpec{laptop.Spec, virtual.Spec}}}}, false},
		{"wrong-resolution", State{Monitors: []Monitor{laptop, testMonitor("Virtual-1", 1280, 800)}, Logical: []LogicalMonitor{laptopLogical, virtualLogical}}, false},
		{"unrelated-physical-output", State{Monitors: []Monitor{laptop, testMonitor("HDMI-1", 1920, 1200)}, Logical: []LogicalMonitor{laptopLogical, {X: 1920, Scale: 1, Monitors: []MonitorSpec{{Connector: "HDMI-1"}}}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			logical, ok := Extension(before, test.after, 1920, 1200)
			if ok != test.want {
				t.Fatalf("Extension() = %t; want %t", ok, test.want)
			}
			if ok && logical.X != 1920 {
				t.Fatalf("wrong monitor selected: %+v", logical)
			}
		})
	}
}
