#!/usr/bin/env python3
"""Build and package the application with its matching FFmpeg source bundle."""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile

from build import Builder, HERE, ROOT, TARGETS


RUNTIME = """ConvertGo runtime requirements

Windows: Windows 10 or newer, with Microsoft Edge WebView2 Runtime installed.
The application never downloads WebView2 automatically. Install the runtime
from Microsoft's official distribution before starting ConvertGo.
macOS: macOS 12 or newer. Packages are not Developer ID signed or notarized;
Gatekeeper may require an explicit user override. Ad-hoc signatures only
preserve executable integrity and are not an Apple identity assertion.
Linux amd64/arm64: Ubuntu 24.04-compatible userspace (glibc 2.39 or newer).
Linux 386: Debian 12-compatible userspace (glibc 2.36 or newer).
All Linux targets need a graphical session, GTK 3, WebKitGTK 4.1 (libsoup 3),
libstdc++6, Fontconfig and system fonts. Debian/Ubuntu example:
  sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0 libstdc++6 fontconfig fonts-dejavu-core
On distributions using the t64 transition, the GTK package may be libgtk-3-0t64.
Use the distribution's package manager; no dependencies are downloaded at app startup.
FFmpeg has network protocols disabled. GPU encoders still require a compatible
local GPU and vendor driver; a compiled backend is not proof of GPU availability.
"""


def run(command, env=None):
    subprocess.run([str(arg) for arg in command], cwd=ROOT, env=env, check=True)


def validate_version(version):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", version):
        raise ValueError("release version must be a v-prefixed semantic version")


def build_app(target, version, repository):
    validate_version(version)
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("invalid release repository")
    goos, goarch = target.split("-")
    env = os.environ.copy()
    env.update({"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "1"})
    if goos == "windows":
        builder = Builder(target, ROOT / "resources/ffmpeg" / target, 1)
        env.update({"CC": builder.cc, "CXX": builder.cxx, "AR": builder.ar, "PATH": builder.env["PATH"]})
    if goos == "darwin":
        env["MACOSX_DEPLOYMENT_TARGET"] = "12.0"
    filename = "ConvertGo.exe" if goos == "windows" else "ConvertGo"
    target_flag = "linux" if target == "linux-386" else goos + "/" + goarch
    command = ["wails", "build", "-clean", "-s", "-skipbindings", "-nosyncgomod", "-m", "-trimpath", "-platform", target_flag,
               "-o", filename, "-ldflags", "-s -w -X main.version=" + version + " -X main.githubRepository=" + repository]
    if goos == "linux":
        command += ["-tags", "webkit2_41"]
    if goos == "windows":
        command += ["-webview2", "error"]
    run(command, env)
    app = ROOT / "build/bin" / ("ConvertGo.app" if goos == "darwin" else filename)
    if not app.exists():
        raise RuntimeError("Wails did not produce the requested target application")


def stage(target, version):
    goos, _ = target.split("-")
    output = HERE / "_stage" / target
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)
    bundle = ROOT / "resources/ffmpeg" / target
    metadata = json.loads((bundle / "build-info.json").read_text())
    if metadata["target"] != target:
        raise RuntimeError("FFmpeg bundle target does not match the application")
    if goos == "darwin":
        app = output / "ConvertGo.app"
        shutil.copytree(ROOT / "build/bin/ConvertGo.app", app, symlinks=True)
        destination = app / "Contents/MacOS/resources/ffmpeg" / target
    else:
        filename = "ConvertGo.exe" if goos == "windows" else "ConvertGo"
        shutil.copy2(ROOT / "build/bin" / filename, output / filename)
        destination = output / "resources/ffmpeg" / target
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(bundle, destination)
    # Artifact transport may discard POSIX mode bits; restore the actual executables.
    for name in ("ffmpeg", "ffprobe"):
        binary = destination / (name + (".exe" if goos == "windows" else ""))
        binary.chmod(0o755)
    if goos != "windows":
        (output / "ConvertGo.app/Contents/MacOS/ConvertGo" if goos == "darwin" else output / "ConvertGo").chmod(0o755)
    (output / "RUNTIME-REQUIREMENTS.txt").write_text(RUNTIME)
    (output / "RELEASE-VERSION.txt").write_text(version + "\n" + target + "\n")
    shutil.copy2(HERE / "THIRD_PARTY_NOTICES.txt", output / "FFMPEG-NOTICES.txt")
    return output


def package(target, version):
    validate_version(version)
    output = stage(target, version)
    goos, goarch = target.split("-")
    assets = ROOT / "dist"
    assets.mkdir(exist_ok=True)
    prefix = "ConvertGo-" + version + "-" + target
    if goos == "darwin":
        app = output / "ConvertGo.app"
        sidecars = app / "Contents/MacOS/resources/ffmpeg" / target
        for name in ("ffmpeg", "ffprobe"):
            run(["codesign", "--force", "--sign", "-", sidecars / name])
        # Signing mutates executable bytes; regenerate their shipped hashes afterwards.
        refresh_bundle_hashes(sidecars)
        run(["codesign", "--force", "--sign", "-", app])
        (output / "Applications").symlink_to("/Applications")
        filename = prefix + ".dmg"
        run(["hdiutil", "create", "-volname", "ConvertGo", "-srcfolder", output, "-ov", "-format", "UDZO", assets / filename])
    elif goos == "windows":
        filename = prefix + "-setup.exe"
        script = output.parent / (prefix + ".nsi")
        # Paths are generated locally, not interpolated from repository or tag input.
        script.write_text('''Unicode true
!include "MUI2.nsh"
Name "ConvertGo"
OutFile "''' + str(assets / filename) + '''"
InstallDir "$LOCALAPPDATA\\Programs\\ConvertGo"
RequestExecutionLevel user
SetCompressor /SOLID lzma
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
Section "ConvertGo"
SetOutPath "$INSTDIR"
File /r "''' + str(output) + '''/*"
WriteUninstaller "$INSTDIR\\Uninstall.exe"
CreateDirectory "$SMPROGRAMS\\ConvertGo"
CreateShortcut "$SMPROGRAMS\\ConvertGo\\ConvertGo.lnk" "$INSTDIR\\ConvertGo.exe"
SectionEnd
Section "Uninstall"
Delete "$SMPROGRAMS\\ConvertGo\\ConvertGo.lnk"
RMDir "$SMPROGRAMS\\ConvertGo"
Delete "$INSTDIR\\ConvertGo.exe"
Delete "$INSTDIR\\RUNTIME-REQUIREMENTS.txt"
Delete "$INSTDIR\\RELEASE-VERSION.txt"
Delete "$INSTDIR\\FFMPEG-NOTICES.txt"
RMDir /r "$INSTDIR\\resources\\ffmpeg\\''' + target + '''"
RMDir "$INSTDIR\\resources\\ffmpeg"
RMDir "$INSTDIR\\resources"
Delete "$INSTDIR\\Uninstall.exe"
RMDir "$INSTDIR"
SectionEnd
''')
        run(["makensis", script])
    else:
        filename = prefix + ".tar.gz"
        with tarfile.open(assets / filename, "w:gz") as archive:
            archive.add(output, arcname=prefix)
    from build import digest
    asset = assets / filename
    (assets / (target + ".asset.json")).write_text(json.dumps({"os": goos, "arch": goarch, "filename": filename, "sha256": digest(asset), "size": asset.stat().st_size}, separators=(",", ":")) + "\n")


def refresh_bundle_hashes(bundle):
    from build import digest
    path = bundle / "build-info.json"
    info = json.loads(path.read_text())
    for filename in info["binaries"]:
        binary = bundle / filename
        info["binaries"][filename] = {"sha256": digest(binary), "size": binary.stat().st_size}
    info["postprocessing"] = "macOS ad-hoc codesign before hashing; app bundle signed after sidecars"
    path.write_text(json.dumps(info, ensure_ascii=False, indent=2) + "\n")
    (bundle / "SHA256SUMS").write_text("".join(value["sha256"] + "  " + filename + "\n" for filename, value in info["binaries"].items()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("app", "package", "stage"))
    parser.add_argument("--target", choices=TARGETS, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY", ""))
    args = parser.parse_args()
    if args.action == "app":
        build_app(args.target, args.version, args.repository)
    elif args.action == "stage":
        validate_version(args.version)
        stage(args.target, args.version)
    else:
        package(args.target, args.version)


if __name__ == "__main__":
    main()
