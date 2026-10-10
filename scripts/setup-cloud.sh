#!/usr/bin/env bash
set -euo pipefail

# Cloud-only setup. Workstation instructions are in README.md.
repo_root=/workspace/UbuntuSuperDisplay
tools_root=/workspace/.tools
mkdir -p "$tools_root" /workspace/.cache

go_root="$tools_root/go1.27.2"
if [[ ! -x "$go_root/bin/go" ]]; then
  [[ "$(uname -sm)" == "Linux x86_64" ]] || { echo 'Cloud setup requires Linux x86_64.' >&2; exit 1; }
  go_archive="$(mktemp /tmp/ubuntu-superdisplay-go.XXXXXX.tar.gz)"
  trap 'rm -f "$go_archive"' EXIT
  curl --fail --show-error --silent --location --retry 3 \
    https://go.dev/dl/go1.27.2.linux-amd64.tar.gz -o "$go_archive"
  printf '%s  %s\n' ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5 "$go_archive" | sha256sum --check --status
  mkdir -p "$go_root"
  tar -xzf "$go_archive" -C "$go_root" --strip-components=1
  rm -f "$go_archive"
  trap - EXIT
fi
[[ "$("$go_root/bin/go" version)" == 'go version go1.27.2 linux/amd64' ]] || { echo 'Unexpected Go toolchain.' >&2; exit 1; }

# The cloud base already supplies GStreamer/PipeWire shared libraries and base
# plugins. Add only the missing CLI tools and PipeWire plugin, without root.
# APT verifies signed Debian metadata and the downloaded package checksums.
gst_root="$tools_root/gstreamer"
if [[ ! -x "$gst_root/usr/bin/gst-launch-1.0" || ! -f "$gst_root/usr/lib/x86_64-linux-gnu/gstreamer-1.0/libgstpipewire.so" ]]; then
  apt_root="$tools_root/ubuntu-superdisplay-apt"
  mkdir -p "$apt_root/lists/partial" "$apt_root/archives/partial" "$apt_root/packages" "$apt_root/conf.d" "$apt_root/sources.list.d"
  cat > "$apt_root/apt.conf" <<CONFIG
Dir::Etc::parts "$apt_root/conf.d";
Dir::Etc::main "/dev/null";
CONFIG
  cat > "$apt_root/sources.list" <<'SOURCES'
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://deb.debian.org/debian trixie main
SOURCES
  apt_options=(
    -o "Dir::Etc::sourcelist=$apt_root/sources.list"
    -o "Dir::Etc::sourceparts=$apt_root/sources.list.d"
    -o "Dir::State::lists=$apt_root/lists"
    -o "Dir::Cache::archives=$apt_root/archives"
    -o "Dir::Cache::pkgcache=$apt_root/pkgcache.bin"
    -o "Dir::Cache::srcpkgcache=$apt_root/srcpkgcache.bin"
    -o 'APT::Get::List-Cleanup=false'
    -o 'APT::Update::Error-Mode=any'
    -o 'Acquire::Retries=3'
  )
  APT_CONFIG="$apt_root/apt.conf" apt-get "${apt_options[@]}" update
  (
    cd "$apt_root/packages"
    APT_CONFIG="$apt_root/apt.conf" apt-get "${apt_options[@]}" download gstreamer1.0-tools gstreamer1.0-pipewire
    mkdir -p "$gst_root"
    for package in ./*.deb; do
      dpkg-deb --extract "$package" "$gst_root"
    done
  )
fi

source "$repo_root/scripts/cloud-env.sh"
go version
gst-inspect-1.0 pipewiresrc >/dev/null
gst-inspect-1.0 fakesink >/dev/null
gst-inspect-1.0 videotestsrc >/dev/null
cd "$repo_root/linux"
# Readonly mode downloads locked dependencies without rewriting manifests.
mkdir -p bin
go build -mod=readonly -o bin/server ./cmd/server
go mod verify
echo 'Cloud build prerequisites installed. A GNOME Wayland desktop is required to create a real virtual monitor.'
