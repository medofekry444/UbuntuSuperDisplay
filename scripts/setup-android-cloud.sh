#!/usr/bin/env bash
set -euo pipefail
repo_root=/workspace/UbuntuSuperDisplay
bash "$repo_root/scripts/setup-cloud.sh"
tools_root=/workspace/.tools

if [[ ! -x "$tools_root/jdk/usr/lib/jvm/java-21-openjdk-amd64/bin/javac" ]]; then
  apt_root="$tools_root/ubuntu-superdisplay-apt"
  mkdir -p "$apt_root/jdk-packages"
  (
    cd "$apt_root/jdk-packages"
    APT_CONFIG="$apt_root/apt.conf" apt-get \
      -o "Dir::Etc::sourcelist=$apt_root/sources.list" \
      -o "Dir::Etc::sourceparts=$apt_root/sources.list.d" \
      -o "Dir::State::lists=$apt_root/lists" \
      -o "Dir::Cache::pkgcache=$apt_root/pkgcache.bin" \
      -o "Dir::Cache::srcpkgcache=$apt_root/srcpkgcache.bin" \
      -o 'APT::Update::Error-Mode=any' update
    APT_CONFIG="$apt_root/apt.conf" apt-get \
      -o "Dir::Etc::sourcelist=$apt_root/sources.list" \
      -o "Dir::Etc::sourceparts=$apt_root/sources.list.d" \
      -o "Dir::State::lists=$apt_root/lists" \
      -o "Dir::Cache::pkgcache=$apt_root/pkgcache.bin" \
      -o "Dir::Cache::srcpkgcache=$apt_root/srcpkgcache.bin" \
      download openjdk-21-jdk-headless openjdk-21-jre-headless
    mkdir -p "$tools_root/jdk"
    for package in ./*.deb; do dpkg-deb --extract "$package" "$tools_root/jdk"; done
  )
fi

if [[ ! -x "$tools_root/gradle-8.11.1/bin/gradle" ]]; then
  archive="$(mktemp /tmp/usd-gradle.XXXXXX.zip)"
  curl --fail --silent --show-error --location --retry 3 https://services.gradle.org/distributions/gradle-8.11.1-bin.zip -o "$archive"
  printf '%s  %s\n' f397b287023acdba1e9f6fc5ea72d22dd63669d59ed4a289a29b1a76eee151c6 "$archive" | sha256sum --check --status
  unzip -q "$archive" -d "$tools_root"
  rm -f "$archive"
fi

sdk_root="$tools_root/android-sdk"
if [[ ! -x "$sdk_root/cmdline-tools/19.0/bin/sdkmanager" ]]; then
  archive="$(mktemp /tmp/usd-android.XXXXXX.zip)"
  curl --fail --silent --show-error --location --retry 3 https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip -o "$archive"
  # SHA256 of the archive first verified against Google's repository metadata.
  printf '%s  %s\n' 7ec965280a073311c339e571cd5de778b9975026cfcbe79f2b1cdcb1e15317ee "$archive" | sha256sum --check --status
  mkdir -p "$sdk_root/cmdline-tools"
  unzip -q "$archive" -d "$sdk_root/cmdline-tools"
  mv "$sdk_root/cmdline-tools/cmdline-tools" "$sdk_root/cmdline-tools/19.0"
  rm -f "$archive"
fi

export JAVA_HOME="$tools_root/jdk/usr/lib/jvm/java-21-openjdk-amd64"
export ANDROID_HOME="$sdk_root"
export ANDROID_USER_HOME=/workspace/.cache/android
mkdir -p "$ANDROID_USER_HOME"
if [[ ! -f "$sdk_root/platforms/android-35/android.jar" || ! -x "$sdk_root/build-tools/35.0.0/aapt2" || ! -x "$sdk_root/platform-tools/adb" ]]; then
  python3 - <<'PY'
import subprocess
log='/workspace/.cache/android-sdk-install.log'
with open(log,'w') as output:
    result=subprocess.run(['/workspace/.tools/android-sdk/cmdline-tools/19.0/bin/sdkmanager',
        '--sdk_root=/workspace/.tools/android-sdk','platforms;android-35','build-tools;35.0.0','platform-tools'],
        input='y\n'*100,text=True,stdout=output,stderr=subprocess.STDOUT)
if result.returncode:
    print(open(log).read()[-5000:])
    raise SystemExit(result.returncode)
PY
fi
python3 "$repo_root/scripts/gradle-cloud.py" :app:assembleDebug :app:testDebugUnitTest
mkdir -p /workspace/artifacts
cp "$repo_root/android/app/build/outputs/apk/debug/app-debug.apk" /workspace/artifacts/UbuntuSuperDisplay-debug.apk
