import { describe, expect, it } from 'vitest'
import { parseEncoderOptions } from './encoder-options'
import { defaultOutput, incompatibleInput, isLocalPath, makeSpec, progressPercent, validateConversion } from './queue'
import type { ConversionOptions } from './queue'
import { media, profile, source } from '../test/fixtures'

function options(overrides: Partial<ConversionOptions> = {}): ConversionOptions {
  return {
    mode: 'individual', profile, videoStream: null, audioStreams: [], subtitleStream: null,
    subtitleMode: 'none', trimStart: 0, trimEnd: 0, cropEnabled: false,
    crop: { x: 0, y: 0, width: 0, height: 0 }, width: 0, height: 0, frameRate: '',
    deinterlace: false, hardwareDecode: 'auto', concatOutput: '/media/joined.mp4', ...overrides,
  }
}

describe('local conversion request validation', () => {
  it('accepts local absolute paths but rejects URLs, relative paths and network shares', () => {
    expect(isLocalPath('/media/video.mp4')).toBe(true)
    expect(isLocalPath('C:\\Media\\video.mp4')).toBe(true)
    for (const path of ['video.mp4', 'https://host/video.mp4', '//host/media.mp4', '\\\\host\\media.mp4', '/media/a\0.mp4']) expect(isLocalPath(path)).toBe(false)
    expect(defaultOutput('/media/input.mp4', 'matroska')).toBe('/media/input_converted.mkv')
  })
  it('keeps defaults and converts seconds to Go duration nanoseconds', () => {
    const spec = makeSpec([source], options({ trimStart: 1.25, trimEnd: 10 }))
    expect(spec.trimStart).toBe(1250000000)
    expect(spec.trimEnd).toBe(10000000000)
    expect(spec.subtitleStream).toBeNull()
    expect(spec.videoStream).toBeNull()
    expect(spec.audioStreams).toEqual([])
    expect(spec.crop).toBeNull()
    expect(spec.hardwareDecode).toBe('auto')
    expect(spec.overwriteConfirmed).toBe(false)
  })
  it('rejects source overwrites and duplicate batch destinations', () => {
    expect(validateConversion([{ ...source, output: source.path }], options())?.key).toBe('invalidOutput')
    expect(validateConversion([source, { ...source, path: '/media/other.mp4' }], options())?.key).toBe('invalidOutput')
  })
  it('checks trim, crop bounds, dimensions, frame rate and selected streams', () => {
    expect(validateConversion([source], options({ trimStart: 120 }))?.key).toBe('invalidTrim')
    expect(validateConversion([source], options({ trimEnd: 121 }))?.key).toBe('invalidTrim')
    expect(validateConversion([source], options({ cropEnabled: true, crop: { x: 1800, y: 0, width: 200, height: 200 } }))?.key).toBe('invalidDimensions')
    expect(validateConversion([source], options({ width: 320, height: 0 }))?.key).toBe('invalidDimensions')
    expect(validateConversion([source], options({ frameRate: '24/0' }))?.key).toBe('invalidFPS')
    expect(validateConversion([source], options({ audioStreams: [99] }))?.key).toBe('missingStream')
    expect(validateConversion([source], options({ subtitleMode: 'burn', subtitleStream: null }))?.key).toBe('missingStream')
    expect(validateConversion([source], options({ subtitleMode: 'copy', subtitleStream: 2, frameRate: '24000/1001', width: 320, height: 180 }))).toBeNull()
  })
  it('checks all concatenate inputs before building a request', () => {
    const second = { ...source, path: '/media/second.mp4', output: '/media/second_converted.mp4' }
    expect(incompatibleInput([source, second])).toBeNull()
    const conflict = { ...second, info: { ...media, streams: [{ ...media.streams[0], width: 1280 }, ...media.streams.slice(1)] } }
    expect(validateConversion([source, conflict], options({ mode: 'concatenate' }))).toEqual({ key: 'incompatible', path: second.path })
    expect(validateConversion([source], options({ mode: 'concatenate' }))?.key).toBe('concatMinimum')
  })
  it('computes real progress from duration values and clamps the track', () => {
    expect(progressPercent(30e9, 120e9)).toBe(25)
    expect(progressPercent(0, 0)).toBe(0)
    expect(progressPercent(200, 100)).toBe(100)
  })
  it('parses editable profile options without accepting malformed or duplicate keys', () => {
    expect(parseEncoderOptions('crf=22\npreset=slow')).toEqual({ crf: '22', preset: 'slow' })
    expect(parseEncoderOptions('crf=22\ncrf=23')).toBeNull()
    expect(parseEncoderOptions('not-an-option')).toBeNull()
  })
})
