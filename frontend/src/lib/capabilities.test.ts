import { describe, expect, it } from 'vitest'
import { concatSubtitleModesAllowed, hardwareBackends, subtitleSupported, supportedProfile } from './capabilities'
import { capabilities, media, profile, source } from '../test/fixtures'

describe('runtime feature gating', () => {
  it('hides profiles when a required encoder is absent', () => {
    expect(supportedProfile(profile, capabilities)).toBe(true)
    expect(supportedProfile({ ...profile, video_encoder: 'h264_nvenc' }, capabilities)).toBe(false)
    expect(supportedProfile({ ...profile, audio_encoder: '' }, capabilities)).toBe(true)
    expect(supportedProfile({ ...profile, container: 'webm' }, capabilities)).toBe(false)
  })
  it('only enables subtitle modes supported by every selected input', () => {
    expect(subtitleSupported([source], 2, capabilities.subtitleCopy.mp4)).toBe(true)
    expect(subtitleSupported([source], 2, [])).toBe(false)
    expect(subtitleSupported([source], null, capabilities.subtitleBurn)).toBe(false)
    const incompatible = { ...source, info: { ...media, streams: [{ index: 2, type: 'subtitle', codec: 'dvd_subtitle' }] } }
    expect(subtitleSupported([source, incompatible], 2, capabilities.subtitleBurn)).toBe(false)
  })

  it('disables both subtitle modes when concatenation cannot carry subtitles', () => {
    expect(concatSubtitleModesAllowed('individual', capabilities)).toBe(true)
    expect(concatSubtitleModesAllowed('concatenate', capabilities)).toBe(false)
    expect(concatSubtitleModesAllowed('concatenate', { ...capabilities, concatSubtitles: true })).toBe(true)
  })
  it('only offers probed decoder backends that cover the input codecs', () => {
    const detected = { ...capabilities, hardwareDecoders: [
      { name: 'h264', codec: 'h264', hardware: true, backend: 'videotoolbox' },
      { name: 'hevc', codec: 'hevc', hardware: true, backend: 'other' },
    ] }
    expect(hardwareBackends([source], null, detected)).toEqual(['videotoolbox'])
    expect(hardwareBackends([source], null, capabilities)).toEqual([])
  })
})
