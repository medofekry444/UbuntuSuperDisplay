# Source this file before running Go or GStreamer in the cloud workspace.
export PATH="/workspace/.tools/go1.27.2/bin:/workspace/.tools/gstreamer/usr/bin:$PATH"
export GOTOOLCHAIN=local
export GOPATH=/workspace/.cache/go-path
export GOMODCACHE=/workspace/.cache/go-mod
export GOCACHE=/workspace/.cache/go-build
export XDG_CACHE_HOME=/workspace/.cache
export GST_PLUGIN_PATH="/workspace/.tools/gstreamer/usr/lib/x86_64-linux-gnu/gstreamer-1.0${GST_PLUGIN_PATH:+:$GST_PLUGIN_PATH}"
export GST_REGISTRY=/workspace/.cache/gstreamer-registry.bin
