#!/usr/bin/env python3
"""Release-only native startup and local FFmpeg conversion acceptance gate."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True)
    parser.add_argument("--stage", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    goos, _ = args.target.split("-")
    stage = args.stage.resolve()
    root = stage / "ConvertGo.app/Contents/MacOS" if goos == "darwin" else stage
    suffix = ".exe" if goos == "windows" else ""
    ffmpeg = root / "resources/ffmpeg" / args.target / ("ffmpeg" + suffix)
    ffprobe = ffmpeg.with_name("ffprobe" + suffix)
    info = json.loads(ffmpeg.with_name("build-info.json").read_text())
    if info["target"] != args.target:
        raise RuntimeError("FFmpeg target metadata mismatch")
    for filename, expected in info["binaries"].items():
        with ffmpeg.with_name(filename).open("rb") as binary:
            actual = hashlib.file_digest(binary, "sha256").hexdigest()
        if actual != expected["sha256"]:
            raise RuntimeError("FFmpeg sidecar hash mismatch: " + filename)
    def capture(command):
        return subprocess.check_output([str(arg) for arg in command], stderr=subprocess.STDOUT, text=True, timeout=90)
    encoders = capture([ffmpeg, "-hide_banner", "-encoders"])
    for encoder in ("libx264", "libx265", "libvpx-vp9", "libopus", "libmp3lame"):
        if encoder not in encoders:
            raise RuntimeError("required encoder missing: " + encoder)
    protocols = capture([ffmpeg, "-hide_banner", "-protocols"]).splitlines()
    if any(line.strip() in ("http", "https", "tcp", "udp", "rtmp", "rtsp", "tls") for line in protocols):
        raise RuntimeError("network protocol present in local-only FFmpeg build")
    with tempfile.TemporaryDirectory(prefix="convertgo-release-smoke-") as temporary:
        output = Path(temporary) / "converted.mp4"
        capture([ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=2",
                 "-f", "lavfi", "-i", "sine=frequency=1000:duration=2", "-vf", "scale=160:90", "-c:v", "libx264", "-c:a", "aac", "-shortest", output])
        probe = json.loads(capture([ffprobe, "-v", "error", "-show_entries", "stream=codec_type,codec_name,width,height:format=duration", "-of", "json", output]))
        if not any(stream.get("codec_name") == "h264" and stream.get("width") == 160 and stream.get("height") == 90 for stream in probe["streams"]):
            raise RuntimeError("H.264 conversion smoke check failed")
        if not any(stream.get("codec_name") == "aac" for stream in probe["streams"]):
            raise RuntimeError("AAC conversion smoke check failed")
        if not 1.8 < float(probe["format"]["duration"]) < 2.3:
            raise RuntimeError("unexpected converted media duration")
        application = root / ("ConvertGo" + suffix)
        env = os.environ.copy()
        env.update({"XDG_CONFIG_HOME": str(Path(temporary) / "config"), "APPDATA": str(Path(temporary) / "config")})
        log = Path(temporary) / "application.log"
        with log.open("wb") as stream:
            process = subprocess.Popen([str(application)], cwd=root, env=env, stdout=stream, stderr=stream)
            try:
                time.sleep(8)
                if process.poll() is not None:
                    raise RuntimeError("application exited during startup: " + log.read_text(errors="replace"))
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps({"target": args.target, "application_started": True, "ffprobe_local_probe": True,
                                      "h264_aac_conversion": True, "sidecar_hashes": True,
                                      "note": "Startup/process and local-media gate only; not full interactive UI or physical GPU validation."}, indent=2) + "\n")


if __name__ == "__main__":
    main()
