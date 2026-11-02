#!/usr/bin/env python3
"""Build one source-pinned FFmpeg sidecar bundle, without invoking application code."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import subprocess
import tarfile
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
SOURCES = json.loads((HERE / "sources.lock.json").read_text())
TARGETS = json.loads((HERE / "targets.json").read_text())
LLVM = {
    "url": "https://github.com/mstorsjo/llvm-mingw/releases/download/20250709/llvm-mingw-20250709-ucrt-ubuntu-22.04-x86_64.tar.xz",
    "sha256": "60cafae6474c7411174cff1d4ba21a8e46cadbaeb05a1bace306add301628337",
}


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def download(info, directory):
    directory.mkdir(parents=True, exist_ok=True)
    destination = directory / info["url"].rsplit("/", 1)[1]
    if not destination.exists() or digest(destination) != info["sha256"]:
        destination.unlink(missing_ok=True)
        temporary = destination.with_suffix(destination.suffix + ".partial")
        try:
            request = urllib.request.Request(info["url"], headers={"User-Agent": "ConvertGo-source-build"})
            with urllib.request.urlopen(request, timeout=120) as response, temporary.open("wb") as output:
                shutil.copyfileobj(response, output)
            if digest(temporary) != info["sha256"]:
                raise RuntimeError("source SHA-256 mismatch: " + info["url"])
            temporary.replace(destination)
        finally:
            temporary.unlink(missing_ok=True)
    return destination


def extract(archive, directory):
    if directory.exists():
        shutil.rmtree(directory)
    directory.mkdir(parents=True)
    with tarfile.open(archive) as source:
        source.extractall(directory, filter="data")
    entries = list(directory.iterdir())
    if len(entries) != 1 or not entries[0].is_dir():
        raise RuntimeError("source archive must contain one root directory: " + str(archive))
    return entries[0]


def toolchain(cache):
    archive = download(LLVM, cache)
    directory = cache / "llvm-mingw"
    if not directory.exists():
        extracted = extract(archive, cache / "llvm-unpack")
        shutil.move(str(extracted), directory)
    return directory


class Builder:
    def __init__(self, target, output, jobs):
        self.target = target
        self.os, self.goarch = target.split("-")
        self.config = TARGETS[target]
        self.jobs = str(jobs)
        self.work = HERE / "_work" / target
        self.prefix = self.work / "prefix"
        self.output = output.resolve()
        self.env = os.environ.copy()
        self.commands = []
        self.archives = {}
        self.license_sources = {}
        self.cross = self.os == "windows"
        self.cc = self.env.get("CC", "cc")
        self.cxx = self.env.get("CXX", "c++")
        self.ar = self.env.get("AR", "ar")
        self.ranlib = self.env.get("RANLIB", "ranlib")
        self.strip = self.env.get("STRIP", "strip")
        self.llvm = None
        if self.cross:
            if platform.system() != "Linux" or platform.machine() != "x86_64":
                raise RuntimeError("Windows builds require the pinned Linux x86_64 LLVM-MinGW toolchain")
            self.llvm = toolchain(HERE / "_cache")
            binary = self.llvm / "bin"
            triple = self.config["host"]
            self.cc = str(binary / (triple + "-clang"))
            self.cxx = str(binary / (triple + "-clang++"))
            self.ar = str(binary / "llvm-ar")
            self.ranlib = str(binary / "llvm-ranlib")
            self.strip = str(binary / "llvm-strip")
            self.env["PATH"] = str(binary) + os.pathsep + self.env["PATH"]
        else:
            expected = {"darwin": "Darwin", "linux": "Linux"}[self.os]
            machine = platform.machine().lower()
            accepted = {"386": ("i386", "i486", "i586", "i686"), "amd64": ("x86_64", "amd64"), "arm64": ("aarch64", "arm64")}
            if platform.system() != expected or machine not in accepted[self.goarch]:
                # Linux i386 userlands still report the x86_64 kernel architecture.
                if not (self.os == "linux" and self.goarch == "386" and platform.architecture()[0] == "32bit"):
                    raise RuntimeError("use a native target userland for " + target)
        self.env.update({"CC": self.cc, "CXX": self.cxx, "AR": self.ar, "RANLIB": self.ranlib,
                         "CFLAGS": "-O2 -fPIC", "CXXFLAGS": "-O2 -fPIC",
                         "PKG_CONFIG_PATH": "", "PKG_CONFIG_LIBDIR": str(self.prefix / "lib/pkgconfig") + os.pathsep + str(self.prefix / "share/pkgconfig")})
        if self.os == "darwin":
            self.env["MACOSX_DEPLOYMENT_TARGET"] = "12.0"
        self.work.mkdir(parents=True, exist_ok=True)
        self.prefix.mkdir(parents=True, exist_ok=True)
        self.meson_file = self.work / "meson.ini"
        self.meson_file.write_text("[binaries]\n" + "\n".join(
            key + " = " + repr(value) for key, value in {"c": self.cc, "cpp": self.cxx, "ar": self.ar, "strip": self.strip, "pkg-config": shutil.which("pkg-config")}.items()) +
            "\n[properties]\nneeds_exe_wrapper = " + ("true" if self.cross else "false") +
            "\n[host_machine]\nsystem = '" + self.os.replace("darwin", "darwin") + "'\ncpu_family = '" + self.config["cpu_family"] + "'\ncpu = '" + self.config["arch"] + "'\nendian = 'little'\n")

    def run(self, command, cwd, env=None):
        command = [str(arg) for arg in command]
        print("+ " + shlex.join(command), flush=True)
        self.commands.append({"cwd": str(cwd), "argv": command})
        subprocess.run(command, cwd=cwd, env=env or self.env, check=True)

    def source(self, name):
        archive = download(SOURCES[name], HERE / "_cache")
        self.archives[name] = archive
        source = extract(archive, self.work / (name + "-source"))
        self.license_sources[name] = source
        return source

    def cmake(self, name, options, subdir=""):
        source = self.source(name) / subdir
        build = self.work / (name + "-build")
        command = ["cmake", "-S", source, "-B", build, "-G", "Ninja", "-DCMAKE_BUILD_TYPE=Release",
                   "-DCMAKE_INSTALL_PREFIX=" + str(self.prefix), "-DCMAKE_INSTALL_LIBDIR=lib", "-DBUILD_SHARED_LIBS=OFF",
                   "-DCMAKE_POSITION_INDEPENDENT_CODE=ON", "-DCMAKE_C_COMPILER=" + self.cc, "-DCMAKE_CXX_COMPILER=" + self.cxx,
                   "-DCMAKE_AR=" + self.ar, "-DCMAKE_RANLIB=" + self.ranlib, "-DCMAKE_PREFIX_PATH=" + str(self.prefix),
                   "-DCMAKE_POLICY_VERSION_MINIMUM=3.5"]
        if self.cross:
            command += ["-DCMAKE_SYSTEM_NAME=Windows", "-DCMAKE_SYSTEM_PROCESSOR=" + self.config["arch"], "-DCMAKE_RC_COMPILER=" + str(self.llvm / "bin" / (self.config["host"] + "-windres"))]
        self.run(command + options, self.work)
        self.run(["cmake", "--build", build, "--parallel", self.jobs], self.work)
        self.run(["cmake", "--install", build], self.work)
        if name == "zlib":
            if self.cross:
                shutil.copy2(self.prefix / "lib/libzlibstatic.a", self.prefix / "lib/libz.a")
            for path in self.prefix.rglob("*"):
                if (path.is_file() or path.is_symlink()) and (".so" in path.name or path.name.endswith((".dylib", ".dll", ".dll.a"))):
                    path.unlink()

    def autotools(self, name, options):
        source = self.source(name)
        command = [source / "configure", "--prefix=" + str(self.prefix), "--libdir=" + str(self.prefix / "lib"), "--disable-shared", "--enable-static"]
        if self.cross:
            command += ["--host=" + self.config["host"]]
        self.run(command + options, source)
        self.run(["make", "-j" + self.jobs], source)
        if name == "fontconfig":
            destination = self.work / "fontconfig-install"
            self.run(["make", "install", "DESTDIR=" + str(destination)], source)
            installed_prefix = destination / self.prefix.relative_to(self.prefix.anchor)
            shutil.copytree(installed_prefix, self.prefix, dirs_exist_ok=True)
        else:
            self.run(["make", "install"], source)

    def meson(self, name, options):
        source = self.source(name)
        build = self.work / (name + "-build")
        command = ["meson", "setup", build, source, "--prefix=" + str(self.prefix), "--libdir=lib", "--default-library=static", "--buildtype=release"]
        command += ["--cross-file" if self.cross else "--native-file", self.meson_file]
        self.run(command + options, self.work)
        self.run(["meson", "compile", "-C", build, "-j", self.jobs], self.work)
        self.run(["meson", "install", "-C", build], self.work)

    def build(self):
        if self.cross:
            for name in ("llvm-mingw-recipe", "mingw-w64", "libcxx", "libcxxabi", "libunwind", "compiler-rt", "llvm-cmake"):
                self.source(name)
        self.cmake("zlib", ["-DZLIB_BUILD_EXAMPLES=OFF"])
        self.cmake("expat", ["-DEXPAT_BUILD_TOOLS=OFF", "-DEXPAT_BUILD_EXAMPLES=OFF", "-DEXPAT_BUILD_TESTS=OFF", "-DEXPAT_SHARED_LIBS=OFF"])
        self.cmake("freetype", ["-DFT_DISABLE_HARFBUZZ=ON", "-DFT_DISABLE_BZIP2=ON", "-DFT_DISABLE_PNG=ON", "-DFT_DISABLE_BROTLI=ON"])
        self.cmake("harfbuzz", ["-DHB_BUILD_UTILS=OFF", "-DHB_BUILD_SUBSET=OFF", "-DHB_HAVE_FREETYPE=OFF", "-DHB_HAVE_GLIB=OFF", "-DHB_HAVE_ICU=OFF"])
        self.meson("fribidi", ["-Ddocs=false", "-Dbin=false", "-Dtests=false"])
        if self.os == "linux":
            self.autotools("fontconfig", ["--disable-docs", "--disable-nls", "--disable-libxml2", "--enable-iconv=no", "--with-expat=" + str(self.prefix), "--with-default-fonts=/usr/share/fonts", "--with-add-fonts=/usr/local/share/fonts", "--with-cache-dir=/var/cache/fontconfig", "--sysconfdir=/etc", "--localstatedir=/var", "--with-baseconfigdir=/etc/fonts"])
        self.autotools("libass", ["--disable-libunibreak", "--disable-test", "--disable-compare", "--disable-asm"] + ([] if self.os == "linux" else ["--disable-fontconfig"]))
        source = self.source("x264")
        flags = ["--prefix=" + str(self.prefix), "--enable-static", "--enable-pic", "--disable-cli", "--disable-opencl", "--disable-asm"]
        if self.cross:
            flags += ["--host=" + self.config["host"], "--cross-prefix=" + str(self.llvm / "bin" / (self.config["host"] + "-"))]
        self.run([source / "configure"] + flags, source)
        self.run(["make", "-j" + self.jobs], source)
        self.run(["make", "install-lib-static", "install-lib-dev"], source)
        self.cmake("x265", ["-DENABLE_SHARED=OFF", "-DENABLE_CLI=OFF", "-DENABLE_ASSEMBLY=OFF", "-DENABLE_LIBNUMA=OFF", "-DENABLE_SVT_HEVC=OFF"], "source")
        source = self.source("vpx")
        self.run([source / "configure", "--prefix=" + str(self.prefix), "--target=generic-gnu", "--enable-pic", "--disable-shared", "--enable-static", "--disable-examples", "--disable-tools", "--disable-docs", "--disable-unit-tests", "--disable-vp8", "--enable-vp9"], source)
        self.run(["make", "-j" + self.jobs], source)
        self.run(["make", "install"], source)
        self.autotools("opus", ["--disable-extra-programs", "--disable-doc", "--disable-intrinsics"])
        self.autotools("lame", ["--disable-frontend", "--disable-decoder", "--disable-gtktest"])
        if "nvenc" in self.config["hardware"]:
            source = self.source("nvcodec")
            self.run(["make", "install", "PREFIX=" + str(self.prefix)], source)
        source = self.source("ffmpeg")
        flags = ["--prefix=" + str(self.prefix), "--arch=" + self.config["arch"], "--target-os=" + self.os.replace("darwin", "darwin").replace("windows", "mingw32"),
                 "--cc=" + self.cc, "--cxx=" + self.cxx, "--ld=" + self.cxx, "--ar=" + self.ar, "--ranlib=" + self.ranlib, "--strip=" + self.strip,
                 "--enable-gpl", "--enable-static", "--disable-shared", "--disable-autodetect", "--disable-debug", "--disable-doc", "--disable-ffplay",
                 "--disable-network", "--disable-devices", "--disable-x86asm", "--pkg-config=pkg-config", "--pkg-config-flags=--static",
                 "--extra-cflags=-I" + str(self.prefix / "include"), "--extra-ldflags=-L" + str(self.prefix / "lib") + (" -static" if self.cross else ""),
                 "--extra-libs=" + ("-lc++ -lwinpthread -lm" if self.cross else "-lc++ -lm" if self.os == "darwin" else "-lstdc++ -lpthread -lm"),
                 "--enable-zlib", "--enable-libx264", "--enable-libx265", "--enable-libvpx", "--enable-libopus", "--enable-libmp3lame", "--enable-libass",
                 "--enable-libfreetype", "--enable-libfribidi", "--enable-libharfbuzz"]
        if self.cross:
            flags += ["--enable-cross-compile", "--cross-prefix=" + str(self.llvm / "bin" / (self.config["host"] + "-"))]
        if self.os == "linux":
            flags += ["--enable-libfontconfig"]
        if "videotoolbox" in self.config["hardware"]:
            flags += ["--enable-videotoolbox", "--enable-audiotoolbox"]
        if "nvenc" in self.config["hardware"]:
            flags += ["--enable-ffnvcodec", "--enable-nvenc", "--disable-nvdec", "--disable-cuvid", "--disable-cuda-llvm"]
        if any("nonfree" in flag for flag in flags):
            raise RuntimeError("nonfree builds cannot be distributed")
        self.ffmpeg_flags = flags
        self.run([source / "configure"] + flags, source)
        self.run(["make", "-j" + self.jobs, "ffmpeg" + self.extension, "ffprobe" + self.extension], source)
        self.package(source)

    @property
    def extension(self):
        return ".exe" if self.os == "windows" else ""

    def package(self, source):
        if self.output.exists():
            raise RuntimeError("refusing to overwrite an existing bundle: " + str(self.output))
        self.output.mkdir(parents=True)
        binaries = {}
        for name in ("ffmpeg", "ffprobe"):
            filename = name + self.extension
            shutil.copy2(source / filename, self.output / filename)
            self.run([self.strip, self.output / filename], self.work)
            binaries[filename] = {"sha256": digest(self.output / filename), "size": (self.output / filename).stat().st_size}
        sources = self.output / "sources"
        notices = self.output / "licenses"
        sources.mkdir()
        notices.mkdir()
        for name, archive in self.archives.items():
            shutil.copy2(archive, sources / archive.name)
            directory = notices / name
            directory.mkdir()
            root = self.license_sources[name]
            for path in root.rglob("*"):
                if path.is_file() and (path.name.upper().startswith(("COPYING", "LICENSE", "NOTICE")) or path.name in ("FTL.TXT", "GPLv2.TXT", "AUTHORS")):
                    relative = path.relative_to(root)
                    destination = directory / relative
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(path, destination)
        if self.llvm:
            for path in self.llvm.rglob("*"):
                if path.is_file() and path.name.upper().startswith(("COPYING", "LICENSE", "NOTICE")):
                    destination = notices / "llvm-mingw" / path.relative_to(self.llvm)
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(path, destination)
        recipe = self.output / "build-recipe"
        recipe.mkdir()
        for filename in ("build.py", "sources.lock.json", "targets.json", "BUILDING.txt", "THIRD_PARTY_NOTICES.txt"):
            shutil.copy2(HERE / filename, recipe / filename)
        for name in ("BUILDING.txt", "THIRD_PARTY_NOTICES.txt"):
            shutil.copy2(HERE / name, self.output / name)
        metadata = {"target": self.target, "ffmpeg_version": SOURCES["ffmpeg"]["version"], "configure_flags": self.ffmpeg_flags,
                    "sources": {name: SOURCES[name] for name in self.archives}, "binaries": binaries,
                    "toolchain": LLVM if self.llvm else {"cc": self.cc, "cxx": self.cxx},
                    "toolchain_versions": {}, "hardware_compiled": self.config["hardware"], "commands": self.commands,
                    "build_environment": {key: self.env[key] for key in ("CFLAGS", "CXXFLAGS", "PKG_CONFIG_LIBDIR", "MACOSX_DEPLOYMENT_TARGET") if key in self.env}}
        for name, command in {"cc": [self.cc, "--version"], "cmake": ["cmake", "--version"], "meson": ["meson", "--version"], "make": ["make", "--version"]}.items():
            metadata["toolchain_versions"][name] = subprocess.check_output(command, env=self.env, text=True).splitlines()[0]
        (self.output / "build-info.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
        (self.output / "SHA256SUMS").write_text("".join(info["sha256"] + "  " + filename + "\n" for filename, info in binaries.items()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True, choices=TARGETS)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--jobs", type=int, default=min(os.cpu_count() or 1, 8))
    args = parser.parse_args()
    if args.jobs < 1:
        parser.error("--jobs must be positive")
    output = args.output or ROOT / "resources/ffmpeg" / args.target
    Builder(args.target, output, args.jobs).build()


if __name__ == "__main__":
    main()
