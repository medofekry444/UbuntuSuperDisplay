# Run the tablet streaming prototype

This milestone adds a single-client H.264/TCP sender and an Android fullscreen
MediaCodec receiver. The first version uses x264 software encoding. Hardware
encoding, latency tuning, input forwarding, and automatic reconnection are still
future work. Your real GNOME session has already passed the Phase 1 window-drag
check; actual HONOR Pad playback must now be tested.

## 1. Update and build the laptop server

If you cloned the repository:

```sh
git pull --ff-only
```

If you originally extracted a ZIP, download the latest ZIP from GitHub and
extract it into a fresh folder instead. From the project folder:

```sh
sudo apt update
sudo apt install golang-go make gstreamer1.0-tools gstreamer1.0-pipewire gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly
make build
```

Go must be at least 1.24. Use a terminal in your ordinary GNOME Wayland session.
Stop the old `virtual` command; `stream` creates and holds its own virtual monitor.

## 2. Install the Android app

Install the supplied debug APK on the HONOR Pad. Allow installation from the
app you use to open the APK when Android prompts you. This is a development
build, signed with a debug certificate.

To build your own APK, open `android/` in Android Studio, install Android SDK
platform 35 and build tools 35.0.0, and use its bundled JDK (17 or newer). Or,
with `ANDROID_HOME` configured and a JDK installed:

```sh
cd android
./gradlew :app:assembleDebug :app:testDebugUnitTest
```

The APK is `android/app/build/outputs/apk/debug/app-debug.apk`. The app supports
Android 8.0/API 26 and newer. It needs Internet permission only; it does not need
ADB or screen-capture permission on the tablet.

## 3. Test the connection before using the real desktop

Connect laptop and tablet to the same local network. Find the laptop's local
Wi-Fi IP in Ubuntu network settings (for example `192.168.1.20`). Start:

```sh
linux/bin/server stream -test-pattern -listen=192.168.1.20:8765 -width=1280 -height=800 -fps=30 -bitrate=4000
```

Replace the example IP with your laptop's actual address. In the tablet app,
enter that same laptop IP and port `8765`, then tap **Connect**. A moving ball
should appear. Tap the picture to show connection controls; tap **Disconnect**
to end the session. Each server run accepts one client and exits on disconnect;
restart the command before connecting again.

The default listener is `127.0.0.1:8765`, which is local to the laptop. Wi-Fi
requires an explicit LAN address. If Ubuntu's firewall blocks the connection,
allow TCP port 8765 from the tablet's local IP. The prototype is unencrypted and
unauthenticated; use it on a trusted local network.

## 4. Stream the extended desktop

Stop the test-pattern server, then start:

```sh
linux/bin/server stream -listen=192.168.1.20:8765 -width=1920 -height=1200 -fps=30 -bitrate=8000 -fixed-mode
```

Connect from the tablet. The command creates the monitor after the client
connects, so it is normal for the second display to be absent while waiting.
Once it reports `STREAMING`, move a window past the laptop's right edge. You
should see it on the tablet. Open GNOME Displays to arrange the monitor if needed.
Disconnecting the tablet or pressing Ctrl+C removes the virtual monitor.

Software encoding starts at 30 FPS. Try `-fps=60` only after playback works and
check CPU use and smoothness; this setting is a request, not a measured rate.
For a slow connection or decoder, try `-width=1280 -height=800 -bitrate=4000`.
The app's touchscreen currently toggles controls; it does not send input to Linux.

## Optional USB transport

The same TCP connection can use ADB reverse. With USB debugging enabled and the
tablet authorized on your laptop:

```sh
adb reverse tcp:8765 tcp:8765
linux/bin/server stream -fixed-mode
```

Enter `127.0.0.1` and `8765` in the tablet app. This transport path still needs
verification on your hardware. Remove the mapping with
`adb reverse --remove tcp:8765` afterward.

## What has been tested

The cloud checks cover framed H.264 over a real TCP connection, native x264
output decoded by FFmpeg, fragmentation and bounded frame sizes, immediate
delivery of a single frame, and Android protocol unit tests and APK compilation.
The cloud has no GNOME desktop or Android device. Actual display playback,
device-specific MediaCodec behavior, Wi-Fi/USB performance, and latency remain
hardware acceptance checks. A server `STREAMING` message establishes that
access units were sent; the app's first rendered output establishes playback.

## Wire format

All integers are unsigned, big-endian 32-bit values. The connection starts with
eight ASCII bytes `USDS0001`, then width, height, and maximum FPS (20 bytes total).
Each packet is a 4-byte length followed by one Annex B H.264 access unit, up to
8 MiB. The first unit includes SPS/PPS and an IDR; parameter sets are repeated
at later keyframes. There are no B-frames. The app uses arrival-based timestamps
and a bounded queue that applies backpressure rather than dropping dependent
frames. There is no audio, input channel, or authenticated handshake yet.

Internally GStreamer `multipartmux` preserves access-unit lengths between the
encoder and Go. These headers are stripped before sending; Android receives
only the documented binary protocol. This allows a static desktop's last frame
to be sent immediately, without waiting for the next AUD delimiter or update.
