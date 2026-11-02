export type Locale = 'zh-CN' | 'en-US'
export type Theme = 'system' | 'light' | 'dark'

export interface Settings {
  locale: Locale
  theme: Theme
  accent_hex: string
  font_family: string
  ffmpeg_path: string
  max_concurrent_jobs: number
  prevent_sleep: boolean
  update_source: 'github' | 'mirror'
  update_mirror_url: string
  auto_check_updates: boolean
}

export interface Profile {
  id: string
  name: string
  container: string
  video_encoder: string
  audio_encoder: string
  video_options: Record<string, string>
  audio_options: Record<string, string>
}

export interface MediaStream {
  index: number
  type: string
  codec: string
  language?: string
  width?: number
  height?: number
  frameRate?: string
  channels?: number
  sampleRate?: number
}

export interface MediaInfo {
  path: string
  name: string
  container: string
  durationSeconds: number
  sizeBytes: number
  streams: MediaStream[]
}

export interface CropRect {
  x: number
  y: number
  width: number
  height: number
}

export interface JobSpec {
  inputs: string[]
  outputPath: string
  mode: 'individual' | 'concatenate'
  overwriteConfirmed: boolean
  profile: Profile
  videoStream: number | null
  audioStreams: number[]
  subtitleStream: number | null
  subtitleMode: 'none' | 'copy' | 'burn'
  trimStart: number
  trimEnd: number
  crop: CropRect | null
  width: number
  height: number
  frameRate: string
  deinterlace: boolean
  hardwareDecode: string
  encoderOptions: Record<string, string>
}

export type JobState = 'queued' | 'probing' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Progress {
  frame: number
  outTime: number
  totalDuration: number
  speed: number
}

export interface Job {
  id: string
  spec: JobSpec
  state: JobState
  progress: Progress
  error: string
}

export interface Release {
  version: string
  assets: Array<{ os: string; arch: string; filename: string; url: string; sha256: string; size: number }>
  signature: string
}

export interface SleepInhibitionStatus {
  active: boolean
  error: string
}

export interface SourceFile {
  path: string
  state: 'probing' | 'ready' | 'error'
  info?: MediaInfo
  error?: string
  output: string
}
