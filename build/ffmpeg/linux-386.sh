#!/bin/sh
set -eu

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl git build-essential \
  python3 python3-venv cmake ninja-build meson pkg-config nasm yasm autoconf \
  automake libtool libgtk-3-dev libwebkit2gtk-4.1-dev xvfb xauth dbus-x11 \
  fontconfig fonts-dejavu-core

# Use the same exact Go release as go.mod/setup-go, verified against go.dev metadata.
python3 - <<'PY'
import hashlib
import json
import os
import tarfile
import urllib.request
from pathlib import Path
version = os.environ['GO_VERSION']
releases = json.load(urllib.request.urlopen('https://go.dev/dl/?mode=json&include=all', timeout=90))
release = next(item for item in releases if item['version'] == version)
entry = next(item for item in release['files'] if item['os'] == 'linux' and item['arch'] == '386' and item['kind'] == 'archive')
archive = Path('/tmp') / entry['filename']
with urllib.request.urlopen('https://go.dev/dl/' + entry['filename'], timeout=120) as source, archive.open('wb') as output:
    while chunk := source.read(1024 * 1024):
        output.write(chunk)
with archive.open('rb') as source:
    if hashlib.file_digest(source, 'sha256').hexdigest() != entry['sha256']:
        raise RuntimeError('Go toolchain checksum mismatch')
with tarfile.open(archive) as source:
    source.extractall('/usr/local', filter='data')
PY
export PATH="/usr/local/go/bin:/root/go/bin:$PATH"
export GOARCH=386 GOOS=linux CGO_ENABLED=1
go install github.com/wailsapp/wails/v2/cmd/wails@"$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2)"
python3 build/ffmpeg/build.py --target linux-386
python3 build/ffmpeg/release.py app --target linux-386 --version "$VERSION" --repository "$GITHUB_REPOSITORY"
python3 build/ffmpeg/release.py stage --target linux-386 --version "$VERSION"
WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 dbus-run-session -- xvfb-run -a \
  python3 build/ffmpeg/smoke.py --target linux-386 --stage build/ffmpeg/_stage/linux-386 \
  --report dist/linux-386.smoke.json
python3 build/ffmpeg/release.py package --target linux-386 --version "$VERSION"
