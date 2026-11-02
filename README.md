<div align="right"><strong>简体中文</strong> | <a href="README.en.md">English</a></div>

# ConvertGo

ConvertGo 是一款使用 Go、Wails 和 FFmpeg 的本地桌面转码工具。界面以文件队列为中心，支持批量添加、媒体流检查、预设、裁剪与滤镜、队列管理，以及 macOS、Windows、Linux 的目标构建。

> 当前代码包含跨平台构建配方。各系统和架构只有经过对应原生启动、媒体探测和转码验收后才算受支持。尚未通过验收或发布的目标不代表已可下载使用。

## 语言

界面支持简体中文与英语。`README.md` 为中文主文档，英文版见 [`README.en.md`](README.en.md)。

## 功能

- 从文件选择器、拖放或本地绝对路径添加媒体；读取容器、时长、尺寸、帧率和音视频字幕流信息。
- 为每个输入建立独立队列任务，也可将流布局一致的多个输入拼接为一个输出。
- 按随附 FFmpeg 实际报告的编码器与封装格式显示预设；可保存本地预设并配置流映射、字幕、时间区间、裁剪、缩放、帧率及去隔行。
- 支持任务状态、实时进度、取消和移除。可设置同时运行的 FFmpeg 进程上限。
- 可选阻止系统休眠。实现使用 macOS 电源断言、Windows 执行状态和 Linux logind。平台不提供该能力时，应用显示原因并继续编码。
- 支持跟随系统、浅色、深色主题，本地字体选择和满足文字对比度要求的自定义进度颜色。
- 媒体探测与转码在本机完成。不会上传媒体、记录分析数据或下载字体。运行时网络仅用于用户发起的 GitHub 更新检查、明确启用的定期检查以及用户选择的 HTTPS GitHub 兼容镜像。

拼接目前要求各输入具有兼容的流布局；当前 FFmpeg 拼接路径不提供字幕复制或烧录。硬件能力只有在所用 FFmpeg 构建包含对应后端且运行时设备探测成功后才显示。构建配方仅为 macOS 启用 VideoToolbox，为 Windows/Linux amd64 配置 NVENC；实际 GPU 和驱动兼容性仍须在目标机器上验证。当前配方没有 QSV、AMF 或 VAAPI。

## 架构目标与状态

| 系统 | 目标架构 | 当前状态 |
| --- | --- | --- |
| macOS | amd64、arm64 | 构建配方已加入，原生发布验收尚未完成 |
| Windows | 386、amd64、arm64 | 构建配方已加入，目标 Windows 原生验收尚未完成 |
| Linux | 386、amd64、arm64 | 构建配方已加入，目标 Linux 原生验收尚未完成 |

Apple Silicon 原生构建通过 Go/Wails 目标 `darwin/arm64`；Linux 32 位在锁定的 Debian i386 用户空间构建。请不要把交叉编译成功等同于桌面运行支持。通过验收的安装包与实际运行要求会随正式 GitHub Release 发布。

## 安装和首次使用

正式版目前尚未发布。通过验收的每个 Release 安装包将分别携带匹配架构的 `ffmpeg`、`ffprobe`、对应源码、构建配置、SHA-256 和许可证材料。也可以在设置中选择本机已有的 `ffmpeg` 可执行文件；同目录需有 `ffprobe`。设置留空时，应用只使用安装包内对应架构的 FFmpeg，不会自动退回 PATH 中的程序。

开发或运行源码构建时，请先按 [`build/ffmpeg/BUILDING.txt`](build/ffmpeg/BUILDING.txt) 准备相应平台依赖，再执行：

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

生产 Wails 构建使用 `wails build`。Linux Wails 构建和运行需要 GTK 3 与 WebKitGTK 4.1；运行还需要相应系统库和字体。具体系统基线、交叉构建方式和运行依赖见 [`build/ffmpeg/BUILDING.txt`](build/ffmpeg/BUILDING.txt)。

启动后，使用“添加文件”或将媒体拖入应用，检查识别到的流，设置本地输出路径并添加任务。输出文件不会与输入文件同名。若目标路径已存在，应用会先要求明确确认。队列结束后，可在任务行查看输出路径及错误信息。

## 更新与网络行为

更新默认关闭自动检查。设置中可手动检查 GitHub Releases，或选择一个 HTTPS GitHub 兼容镜像并随时恢复默认来源。只有签名有效、系统与架构匹配、文件大小及 SHA-256 校验通过的更新才会显示为可下载。下载需要再次明确确认；应用不会静默替换当前安装。

如果本次构建没有配置 GitHub 仓库名，更新功能会显示“未配置”，不会发出请求。检查更新时仅访问 GitHub 发布/API/CDN 地址，或设置中选择的镜像源。自定义镜像可获知更新请求，故只应使用你信任的 HTTPS 镜像。构建与开发阶段可能需要访问上游源代码和 Go/npm 依赖；这不属于应用运行时请求。

## FFmpeg 许可与源代码

随包 FFmpeg 使用启用 GPL 的 `libx264` 与 `libx265`，未启用 `--enable-nonfree`。FFmpeg 是单独进程，应用不链接 FFmpeg 库。每份发布包需包含与二进制完全对应的 FFmpeg 和外部库源码、编译参数及第三方许可证材料，详见 [`build/ffmpeg/THIRD_PARTY_NOTICES.txt`](build/ffmpeg/THIRD_PARTY_NOTICES.txt)、[`build/ffmpeg/sources.lock.json`](build/ffmpeg/sources.lock.json) 和 FFmpeg 的[许可说明](https://ffmpeg.org/legal.html)。ConvertGo 应用自身的许可证尚未在仓库中指定，不能据 FFmpeg 的许可推断应用许可证。

## GitHub 开发流程

公开仓库使用 `dev` 作为默认开发分支。`master` 只接收从 `dev` 创建并通过必需检查的拉取请求，不直接推送；保护规则要求 CI、CodeQL 和来源分支检查。CI 与 CodeQL 工作流使用固定 SHA 的 GitHub Actions，Dependabot 每周检查 Go modules、npm 与 Actions 依赖。Greptile 配置聚焦简体中文、正确性、安全、并发、网络边界与许可证审查。

CodeQL 工作流配置已加入仓库；其 GitHub 安全告警和状态检查是否生效，以仓库实际 Actions 运行与 Security 页面结果为准。Greptile 需要在 GitHub 将应用安装权限限定为 ConvertGo 仓库后才能审查拉取请求。
