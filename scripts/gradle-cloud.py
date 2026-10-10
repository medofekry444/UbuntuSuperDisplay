#!/usr/bin/env python3
"""Run the Android build using retained cloud tools and the provided proxy."""
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit

repo = Path(__file__).resolve().parent.parent
env = os.environ.copy()
env.update(
    JAVA_HOME="/workspace/.tools/jdk/usr/lib/jvm/java-21-openjdk-amd64",
    ANDROID_HOME="/workspace/.tools/android-sdk",
    ANDROID_USER_HOME="/workspace/.cache/android",
    GRADLE_USER_HOME="/workspace/.cache/gradle",
)
Path("/workspace/.cache/java-home").mkdir(parents=True, exist_ok=True)
wrapper = repo / "android/gradlew"
executable = str(wrapper) if wrapper.exists() else "/workspace/.tools/gradle-8.11.1/bin/gradle"
args = [executable, "-Duser.home=/workspace/.cache/java-home"]
proxy = os.environ.get("HTTPS_PROXY") or os.environ.get("HTTP_PROXY")
if proxy:
    parsed = urlsplit(proxy)
    if parsed.hostname:
        for protocol in ("http", "https"):
            args.extend([f"-D{protocol}.proxyHost={parsed.hostname}", f"-D{protocol}.proxyPort={parsed.port or 80}"])
args += ["--no-daemon"] + (sys.argv[1:] or [":app:assembleDebug", ":app:testDebugUnitTest"])
raise SystemExit(subprocess.run(args, cwd=repo / "android", env=env).returncode)
