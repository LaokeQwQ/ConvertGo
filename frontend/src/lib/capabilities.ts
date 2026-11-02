import type { Profile, SourceFile } from './types'

export interface CodecCapability {
  name: string
  codec: string
  hardware: boolean
  backend: string
}

export interface Capabilities {
  containers: string[]
  videoEncoders: CodecCapability[]
  audioEncoders: CodecCapability[]
  hardwareDecoders: CodecCapability[]
  subtitleCopy: Record<string, string[]>
  subtitleBurn: string[]
  concatSubtitles: boolean
}

export function supportedProfile(profile: Profile, capabilities: Capabilities): boolean {
  return capabilities.containers.includes(profile.container) &&
    (!profile.video_encoder || capabilities.videoEncoders.some(encoder => encoder.name === profile.video_encoder)) &&
    (!profile.audio_encoder || capabilities.audioEncoders.some(encoder => encoder.name === profile.audio_encoder)) &&
    !!(profile.video_encoder || profile.audio_encoder)
}

export function subtitleSupported(sources: SourceFile[], index: number | null, supportedCodecs: readonly string[]): boolean {
  return index !== null && sources.length > 0 && sources.every(source => source.info?.streams.some(stream => stream.index === index && stream.type === 'subtitle' && supportedCodecs.includes(stream.codec)))
}

export function concatSubtitleModesAllowed(mode: 'individual' | 'concatenate', capabilities: Capabilities | null): boolean {
  return mode !== 'concatenate' || capabilities?.concatSubtitles === true
}

export function hardwareBackends(sources: SourceFile[], index: number | null, capabilities: Capabilities): string[] {
  const backends = [...new Set(capabilities.hardwareDecoders.map(decoder => decoder.backend).filter(Boolean))]
  return backends.filter(backend => sources.every(source => {
    const stream = source.info?.streams.find(item => item.type === 'video' && (index === null || index === item.index))
    return !stream || capabilities.hardwareDecoders.some(decoder => decoder.backend === backend && decoder.codec === stream.codec)
  }))
}
