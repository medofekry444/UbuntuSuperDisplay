# Validate the virtual desktop on Ubuntu

Run these steps from your GNOME Wayland desktop. The cloud's mock tests cannot
establish that your GPU/compositor exposes a usable desktop surface.

1. Run `linux/bin/server doctor` from the checkout. GStreamer and Mutter checks
   must pass. The monitor list should include your laptop display.
2. Run `linux/bin/server monitors` and keep the initial layout for comparison.
3. Run `linux/bin/server virtual -width=1920 -height=1200 -fps=60`. Leave the
   command running. The process owns the lifetime of the virtual monitor.
4. Confirm the first-buffer message and `READY`, with a new logical monitor
   and its coordinates. Open GNOME Settings → Displays. If necessary, select
   extended/joined displays and arrange the virtual monitor next to your laptop.
   The prototype does not write or persist a monitor layout.
5. Drag an ordinary application window from the laptop into that monitor's
   desktop area. Its disappearance into the additional area, with the pointer
   crossing that boundary, confirms the expected desktop extension. A new
   PipeWire node by itself is insufficient. There is no tablet renderer yet.
6. Bring the window back and press Ctrl+C. Run `linux/bin/server monitors` again.
   The virtual monitor should be gone and your physical displays should remain.

Record the GNOME version, GPU, command used, initial/final monitor lists, startup
output, and whether moving the window succeeded. Avoid pasting environment
variables or credentials. No API keys are required.

## Diagnose a failed attempt

| Observed result | Next action |
|---|---|
| Session bus unset | Run as the logged-in desktop user, inside a GNOME terminal. |
| Mutter service unavailable | Confirm a GNOME Wayland session; Xorg and other compositors are outside this prototype's scope. |
| `RecordVirtual` unknown/rejected | Check the GNOME/Mutter release and its private interface. Capture the error for backend compatibility work. |
| `pipewiresrc` missing | Install `gstreamer1.0-pipewire` and verify `gst-inspect-1.0 pipewiresrc`. |
| Node created, no frames | Check GStreamer diagnostics, `systemctl --user status pipewire`, and that the process uses the desktop user's runtime/session. |
| Frames, no additional logical monitor | Check Displays is in extended mode; try `-fixed-mode` on Mutter 50.1 and inspect the resulting state. |
| Platform flag rejected | Retry with `-platform=false`; the command still requires captured frames and an additional logical monitor. |
| New monitor has a different mode | Request a supported size, or retry with `-fixed-mode`; readiness deliberately rejects a mismatched active mode. |
| Cleanup error | Keep the diagnostic output and check the monitor list. Mutter should tear down streams when the private bus connection closes. |

The buffer total is a diagnostic count. It is not an achieved frame rate,
end-to-end latency measurement, or evidence of an Android connection.
