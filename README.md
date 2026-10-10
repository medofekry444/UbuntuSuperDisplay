# UbuntuSuperDisplay

A Linux alternative to SuperDisplay, starting with a Go virtual-display backend
for GNOME Wayland. The intended desktop is Ubuntu 26.04 / GNOME 50.1, and the
eventual tablet client is the HONOR Pad X8a.

**Current milestone: Phase 2 streaming prototype.** The Go backend creates a Mutter screencast session,
requests a virtual stream, starts a GStreamer PipeWire consumer, and waits for
both captured video buffers and an additional active logical monitor. It keeps
the display alive until Ctrl+C and cleans up its own session and subprocess.

The Phase 1 virtual monitor and window-drag check passed on the user's real
GNOME session. The new `stream` command sends software-encoded H.264 over TCP,
and the Kotlin Android app decodes it fullscreen with MediaCodec. Cloud builds,
protocol tests, and native encoding/decoding checks pass. Playback on the HONOR
Pad still requires hardware validation. Hardware encoding, input forwarding,
and latency optimization are later phases; 60 FPS is a target, not a measured result.

**To display the desktop on your tablet, follow [the streaming guide](docs/streaming.md).**
The `virtual` command below remains a diagnostic that discards captured frames.

## Run on your Ubuntu desktop

Use Go 1.24 or later. Install the capture tools:

```sh
sudo apt-get update
sudo apt-get install gstreamer1.0-tools gstreamer1.0-pipewire gstreamer1.0-plugins-base dbus-daemon
```

From this checkout, build the backend:

```sh
make build
```

Run from a terminal **inside your logged-in GNOME Wayland session**, as your
ordinary user. D-Bus and PipeWire must belong to that same session. Running the
backend with sudo, from a headless cloud machine, or on a newly created test bus
cannot create a monitor on your laptop's desktop.

```sh
linux/bin/server doctor
linux/bin/server monitors
linux/bin/server virtual -width=1920 -height=1200 -fps=60
```

The command prints the session path, stream path, PipeWire node ID, first-buffer
confirmation, and monitor coordinates. `READY` requires a new output at the
requested physical resolution in its own active logical monitor. A stream
created without captured frames, an inactive output, and a mirrored output
cannot pass this check. Then drag a window into the reported monitor area;
that manual step is the Phase 1 acceptance criterion. Ctrl+C removes the display.

To make a bounded attempt that automatically cleans up after readiness:

```sh
linux/bin/server virtual -duration=30s -startup-timeout=20s
```

Mutter 50.1 also advertises explicit monitor modes. If a virtual stream stays
absent from GNOME Displays, try:

```sh
linux/bin/server virtual -fixed-mode
```

This optional flag sends a fixed preferred 1920×1200, 60 Hz mode. It is not a
compatibility fallback for older Mutter versions. `-platform=false` omits the
request to treat the output as a platform monitor if that behavior is rejected
by your desktop. Neither option has been validated against physical hardware.

Check [the desktop acceptance procedure](docs/phase-1-validation.md) for the
expected behavior and troubleshooting. Run `linux/bin/server virtual -h` for all
flags. Resolution changes currently require stopping and starting the command.

## Cloud development

The cloud environment is Debian 13 and has no GNOME desktop session. It supports
editing, building, and automated tests. Setup installs checksum-verified Go
1.27.2 and signed Debian GStreamer packages under `/workspace/.tools`; it uses
the existing base image's shared libraries and does not require root. Go caches
and GStreamer's registry are stored under `/workspace/.cache`.

```sh
cd /workspace/UbuntuSuperDisplay
bash scripts/setup-cloud.sh
source scripts/cloud-env.sh
make build
make test
make check
```

`make test` runs on an isolated D-Bus session with packages executed serially,
so test doubles cannot compete for Mutter's well-known bus names. It tests
session lifecycle, wire types, immediate signals, missing nodes, cleanup,
consumer failures, and rejection of mirrored displays or missing frames. A
native GStreamer pipeline also verifies that real buffers establish readiness.
The tests use simulated Mutter responses, not a GNOME compositor. If D-Bus or
GStreamer are missing in another environment, relevant tests explicitly skip;
such a run does not establish readiness.

Use the existing isolated checkout for cloud tasks. A Git worktree is unnecessary.
No background desktop services need to run in the cloud. `server doctor`
deliberately exits with an actionable error when the desktop session is missing.

For Android builds in the cloud:

```sh
bash scripts/setup-android-cloud.sh
python3 scripts/gradle-cloud.py :app:assembleDebug :app:testDebugUnitTest
```

The setup installs a verified JDK, Android SDK 35, and Gradle 8.11.1. The helper
uses the environment's existing proxy and workspace caches; desktop developers
can use Android Studio or `android/gradlew` directly. Generated APKs and local
SDK paths are excluded from Git.

## Implementation

```text
linux/
  cmd/server/              CLI, diagnostics, readiness, and shutdown
  internal/display/        Mutter D-Bus sessions and monitor state
  internal/capture/        GStreamer PipeWire consumer and buffer readiness
  internal/stream/         H.264 encoding, access-unit framing, and TCP protocol
android/app/               Kotlin fullscreen MediaCodec client
docs/phase-1-validation.md  Desktop acceptance procedure
docs/streaming.md           Tablet installation and streaming procedure
scripts/                   Reusable cloud dependency setup and activation
```

The consumer requests variable-framerate video with a maximum frame rate:

```text
pipewiresrc path=NODE do-timestamp=true
  ! video/x-raw,width=1920,height=1200,framerate=0/1,max-framerate=60/1
  ! fakesink sync=false silent=false
```

Mutter advertises `framerate=0/1` and uses `max-framerate` when creating the virtual
monitor mode. A fixed `framerate=60/1` can fail negotiation. The verbose sink
messages are counted and discarded; buffers are not accumulated in Go memory.
This is a diagnostic capture pipeline, not the future low-latency encoder.

Mutter's ScreenCast interface is private and version-dependent. The implementation
uses the actual [GNOME 50.1 D-Bus signatures](https://github.com/GNOME/mutter/blob/50.1/data/dbus-interfaces/org.gnome.Mutter.ScreenCast.xml),
[display state interface](https://github.com/GNOME/mutter/blob/50.1/data/dbus-interfaces/org.gnome.Mutter.DisplayConfig.xml),
and [virtual stream implementation](https://github.com/GNOME/mutter/blob/50.1/src/backends/meta-screen-cast-virtual-stream-src.c).
The ScreenCast API version returned by `doctor` is not the GNOME release number.

## Next milestone

Validate playback on the HONOR Pad with the test pattern, then the extended
desktop. Hardware encoding, robust reconnect behavior, and touch/input support
follow with measured latency and frame-rate checks. The same TCP connection
can use ADB reverse for USB; that path also needs a real-device check.
