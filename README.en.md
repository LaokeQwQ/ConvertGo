<div align="right"><a href="README.md">简体中文</a> | <strong>English</strong></div>

# ConvertGo

ConvertGo is a local desktop transcoder built with Go, Wails, and FFmpeg. Its file-first interface supports batch input, media stream inspection, presets, crop and video filters, queue management, and build targets for macOS, Windows, and Linux.

> The repository contains cross-platform build recipes. A system/architecture is supported only after a native launch, media probe, and conversion acceptance run on that target. A target without a passing acceptance run or published release is not available for download.

## Languages

The interface supports Simplified Chinese and English. [`README.md`](README.md) is the primary Chinese guide.

## Features

- Add local media through the file picker, drag and drop, or absolute local paths. Inspect container, duration, dimensions, frame rate, and audio, video, and subtitle streams.
- Create an individual job per input, or concatenate multiple inputs with compatible stream layouts into one output.
- Show profiles based on the encoders and containers reported by the bundled FFmpeg. Save local presets and configure stream mapping, subtitles, trim ranges, crop, scale, frame rate, and deinterlacing.
- Monitor job state and live progress, cancel or remove jobs, and limit concurrent FFmpeg processes.
- Optionally prevent sleep using macOS power assertions, Windows execution state, or Linux logind. If a platform cannot inhibit sleep, the app reports why and continues encoding.
- Follow the system theme or select light/dark, choose an installed local font, and set a custom progress color only when text contrast remains readable.
- Media probing and transcoding stay on the computer. The app does not upload media, collect analytics, or download fonts. Runtime network access is limited to user-initiated GitHub update checks, explicitly enabled periodic checks, and an HTTPS GitHub-compatible mirror selected by the user.

Concatenation currently requires compatible stream layouts; the current FFmpeg concat path does not provide subtitle copy or burn-in. Hardware capabilities appear only when the selected FFmpeg build includes a backend and runtime device probing succeeds. The current recipes enable VideoToolbox for macOS and configure NVENC for Windows/Linux amd64. Actual GPU and driver compatibility still needs validation on the target machine. QSV, AMF, and VAAPI are not included in the current recipes.

## Target architectures and status

| System | Target architectures | Current status |
| --- | --- | --- |
| macOS | amd64, arm64 | Build recipes added; native release acceptance remains pending |
| Windows | 386, amd64, arm64 | Build recipes added; native Windows acceptance remains pending |
| Linux | 386, amd64, arm64 | Build recipes added; native Linux acceptance remains pending |

Apple Silicon builds use Go/Wails target `darwin/arm64`; Linux 32-bit builds use the pinned Debian i386 userland. A successful cross-compile alone does not establish desktop runtime support. Installers for targets that pass acceptance will be attached to GitHub Releases with their verified runtime requirements.

## Installation and first use

No production release has been published yet. Each accepted release package will include the matching architecture's `ffmpeg` and `ffprobe`, corresponding source, build configuration, SHA-256, and license material. You may also select a local `ffmpeg` executable in Settings; the same directory must contain `ffprobe`. When the override is empty, the app uses only the bundled target-matching FFmpeg and does not silently fall back to a program found on PATH.

For development or a local source build, install the target platform prerequisites from [`build/ffmpeg/BUILDING.txt`](build/ffmpeg/BUILDING.txt), then run:

```sh
cd frontend
npm ci
npm run check
npm run test
npm run build
cd ..
go test ./...
wails dev
```

A production Wails build uses `wails build`. Linux Wails builds and runtime require GTK 3 and WebKitGTK 4.1; runtime also needs the related system libraries and fonts. See [`build/ffmpeg/BUILDING.txt`](build/ffmpeg/BUILDING.txt) for target-specific system baselines, build instructions, and runtime prerequisites.

After launch, choose Add files or drop media into the window, inspect the detected streams, set a local output path, and add jobs. Outputs do not use the same path as their inputs. If a target already exists, the app asks before replacing it. After a job finishes, its row shows the output path or the reported error.

## Updates and network behavior

Automatic update checks are off by default. Settings lets you check GitHub Releases manually, choose one HTTPS GitHub-compatible mirror, and restore GitHub as the default. An update is offered only after its signature, operating-system/architecture tuple, file size, and SHA-256 verify. Download requires explicit confirmation; the app never silently replaces the running installation.

If a build has no configured GitHub repository, update checks report that they are unconfigured and make no request. Update checks use GitHub release/API/CDN hosts or the single selected mirror. A configured mirror can observe update requests, so use only an HTTPS mirror you trust. Building or developing may contact upstream source hosts and Go/npm dependency services; those are not application runtime requests.

## FFmpeg licensing and source

The bundled FFmpeg enables GPL components, including `libx264` and `libx265`, and does not enable `--enable-nonfree`. FFmpeg runs as a separate process; the app does not link FFmpeg libraries. Every release package must include source for the exact FFmpeg and external libraries used to build its binaries, configure arguments, and third-party license notices. See [`build/ffmpeg/THIRD_PARTY_NOTICES.txt`](build/ffmpeg/THIRD_PARTY_NOTICES.txt), [`build/ffmpeg/sources.lock.json`](build/ffmpeg/sources.lock.json), and FFmpeg's [license guidance](https://ffmpeg.org/legal.html). The ConvertGo application license has not been specified in this repository; do not infer it from FFmpeg's license.

## GitHub development workflow

The public repository uses `dev` as its default development branch. `master` accepts changes only through pull requests from `dev` that pass required checks; direct pushes are blocked. CI and CodeQL workflows use Actions pinned by full SHA, and Dependabot checks Go modules, npm, and Actions dependencies weekly. Greptile is configured for Simplified Chinese reviews focused on correctness, security, concurrency, network boundaries, and licensing.

CodeQL workflow files are present. Security alerts and required check results must be confirmed from actual workflow runs and the GitHub Security page. Greptile requires installing its GitHub App with access limited to the ConvertGo repository before it can review pull requests.
