import { defaults } from '../lib/appearance'
import type { Job, JobState, MediaInfo, Profile, SourceFile } from '../lib/types'
import type { Capabilities } from '../lib/capabilities'

export const settings = defaults(['en-US'])
export const profile: Profile = {
  id: 'mp4-h264', name: 'MP4 H.264 / AAC', container: 'mp4', video_encoder: 'libx264',
  audio_encoder: 'aac', video_options: { crf: '23' }, audio_options: { 'b:a': '192k' },
}
export const media: MediaInfo = {
  path: '/media/source.mp4', name: 'source.mp4', container: 'mov,mp4', durationSeconds: 120, sizeBytes: 1048576,
  streams: [
    { index: 0, type: 'video', codec: 'h264', width: 1920, height: 1080, frameRate: '24/1' },
    { index: 1, type: 'audio', codec: 'aac', channels: 2, sampleRate: 48000 },
    { index: 2, type: 'subtitle', codec: 'mov_text', language: 'eng' },
  ],
}
export const source: SourceFile = { path: media.path, state: 'ready', info: media, output: '/media/source_converted.mp4' }

export const capabilities: Capabilities = {
  containers: ['mp4', 'matroska'],
  videoEncoders: [{ name: 'libx264', codec: 'h264', hardware: false, backend: '' }],
  audioEncoders: [{ name: 'aac', codec: 'aac', hardware: false, backend: '' }],
  hardwareDecoders: [], subtitleCopy: { mp4: ['mov_text'], matroska: ['mov_text', 'subrip'] }, subtitleBurn: ['mov_text', 'subrip'], concatSubtitles: false,
}

export function job(state: JobState = 'queued'): Job {
  return {
    id: 'job-1', state, error: state === 'failed' ? 'Encoder is unavailable' : '',
    spec: {
      inputs: [media.path], outputPath: source.output, mode: 'individual', overwriteConfirmed: false, profile,
      videoStream: null, audioStreams: [], subtitleStream: null, subtitleMode: 'none',
      trimStart: 0, trimEnd: 0, crop: null, width: 0, height: 0, frameRate: '',
      deinterlace: false, hardwareDecode: 'auto', encoderOptions: {},
    },
    progress: { frame: 100, outTime: 30e9, totalDuration: 120e9, speed: 2 },
  }
}
