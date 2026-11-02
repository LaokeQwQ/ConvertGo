import type { MessageKey } from './i18n'
import type { CropRect, JobSpec, MediaInfo, Profile, SourceFile } from './types'

export interface ConversionOptions {
  mode: JobSpec['mode']
  profile: Profile | undefined
  videoStream: number | null
  audioStreams: number[]
  subtitleStream: number | null
  subtitleMode: JobSpec['subtitleMode']
  trimStart: number
  trimEnd: number
  cropEnabled: boolean
  crop: CropRect
  width: number
  height: number
  frameRate: string
  deinterlace: boolean
  hardwareDecode: string
  concatOutput: string
}

export function isLocalPath(path: string): boolean {
  return !path.includes('\0') && !path.startsWith('//') && (path.startsWith('/') || /^[a-z]:[\\/]/i.test(path))
}

export function fileName(path: string): string {
  return path.split(/[\\/]/).pop() || path
}

export function defaultOutput(path: string, container: string): string {
	const extension = ({ matroska: 'mkv', ipod: 'm4a' } as Record<string, string>)[container] || container
	const basename = path.split(/[\\/]/).pop() || path
	const suffix = filepathExtension(basename)
	const stem = suffix ? basename.slice(0, -suffix.length) : basename
	return `${path.slice(0, path.length - basename.length)}${stem}_converted.${extension}`
}

function filepathExtension(name: string): string {
	const dot = name.lastIndexOf('.')
	return dot > 0 ? name.slice(dot) : ''
}

function pathKey(path: string): string {
  return /^[a-z]:/i.test(path) ? path.replace(/\\/g, '/').toLowerCase() : path
}

export function incompatibleInput(sources: SourceFile[]): string | null {
  if (sources.length < 2 || !sources[0].info) return null
  const layout = (info: MediaInfo): string => JSON.stringify(info.streams.map(stream => [
    stream.index, stream.type, stream.codec, stream.width || 0, stream.height || 0,
    stream.frameRate || '', stream.channels || 0, stream.sampleRate || 0,
  ]))
  const first = layout(sources[0].info)
  return sources.slice(1).find(source => source.info && layout(source.info) !== first)?.path ?? null
}

export interface FormError { key: MessageKey; path?: string }

export function validateConversion(sources: SourceFile[], options: ConversionOptions): FormError | null {
  if (!options.profile) return { key: 'invalidProfile' }
  if (!sources.length || sources.some(source => source.state !== 'ready' || !source.info)) return { key: 'invalidPaths' }
  if (options.mode === 'concatenate') {
    if (sources.length < 2) return { key: 'concatMinimum' }
    const path = incompatibleInput(sources)
    if (path) return { key: 'incompatible', path }
  }
  const outputs = options.mode === 'concatenate' ? [options.concatOutput] : sources.map(source => source.output)
  const inputs = new Set(sources.map(source => pathKey(source.path)))
  const outputKeys = outputs.map(pathKey)
  if (outputs.some(path => !isLocalPath(path) || inputs.has(pathKey(path))) || new Set(outputKeys).size !== outputKeys.length) return { key: 'invalidOutput' }
  if (![options.trimStart, options.trimEnd].every(value => Number.isFinite(value) && value >= 0) ||
    (options.trimEnd !== 0 && options.trimEnd <= options.trimStart) ||
    sources.some(source => source.info && source.info.durationSeconds > 0 && (options.trimStart >= source.info.durationSeconds || options.trimEnd > source.info.durationSeconds))) return { key: 'invalidTrim' }
  if (![options.width, options.height].every(value => Number.isInteger(value) && value >= 0) || (options.width === 0) !== (options.height === 0)) return { key: 'invalidDimensions' }
  if (options.cropEnabled) {
    const { x, y, width, height } = options.crop
    if (![x, y, width, height].every(Number.isInteger) || x < 0 || y < 0 || width <= 0 || height <= 0) return { key: 'invalidDimensions' }
    if (sources.some(source => {
      const video = source.info?.streams.find(stream => stream.type === 'video' && (options.videoStream === null || stream.index === options.videoStream))
      return !video || x + width > (video.width || 0) || y + height > (video.height || 0)
    })) return { key: 'invalidDimensions' }
  }
  if (options.frameRate && (!/^\d+(?:\.\d+)?(?:\/\d+(?:\.\d+)?)?$/.test(options.frameRate) || !options.frameRate.split('/').every(value => Number.isFinite(Number(value)) && Number(value) > 0))) return { key: 'invalidFPS' }
  for (const source of sources) {
    const streams = source.info!.streams
    if ((options.videoStream !== null && !streams.some(stream => stream.type === 'video' && stream.index === options.videoStream)) ||
      options.audioStreams.some(index => !streams.some(stream => stream.type === 'audio' && stream.index === index)) ||
      (options.subtitleMode !== 'none' && (options.subtitleStream === null || !streams.some(stream => stream.type === 'subtitle' && stream.index === options.subtitleStream)))) return { key: 'missingStream', path: source.path }
  }
  return null
}

export function makeSpec(sources: SourceFile[], options: ConversionOptions): JobSpec {
  return {
    inputs: sources.map(source => source.path),
    outputPath: options.mode === 'concatenate' ? options.concatOutput : sources[0].output,
    mode: options.mode, overwriteConfirmed: false, profile: options.profile!, videoStream: options.videoStream,
    audioStreams: [...options.audioStreams], subtitleStream: options.subtitleMode === 'none' ? null : options.subtitleStream,
    subtitleMode: options.subtitleMode, trimStart: Math.round(options.trimStart * 1e9), trimEnd: Math.round(options.trimEnd * 1e9),
    crop: options.cropEnabled ? { ...options.crop } : null, width: options.width, height: options.height,
    frameRate: options.frameRate, deinterlace: options.deinterlace, hardwareDecode: options.hardwareDecode,
    encoderOptions: {},
  }
}

export function formatTime(seconds: number): string {
  const safe = Math.max(0, Math.floor(seconds || 0))
  return `${Math.floor(safe / 3600).toString().padStart(2, '0')}:${Math.floor(safe / 60 % 60).toString().padStart(2, '0')}:${(safe % 60).toString().padStart(2, '0')}`
}

export function progressPercent(outTime: number, totalDuration: number): number {
  return totalDuration > 0 ? Math.max(0, Math.min(100, outTime / totalDuration * 100)) : 0
}
