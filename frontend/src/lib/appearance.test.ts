import { describe, expect, it } from 'vitest'
import { accentForeground, applyAppearance, contrast, defaults, resolvedTheme, settingsError, themeTokens, validAccent } from './appearance'
import { catalogs, resolveLocale, translator } from './i18n'
import { compareVersions } from './versions'

describe('locale selection and catalogs', () => {
  it('uses a supported OS locale and otherwise defaults to Simplified Chinese', () => {
    expect(resolveLocale(['en-US'])).toBe('en-US')
    expect(resolveLocale(['zh-CN'])).toBe('zh-CN')
    expect(resolveLocale(['fr-FR', 'en-US'])).toBe('en-US')
    expect(resolveLocale(['en-GB'])).toBe('zh-CN')
    expect(resolveLocale([])).toBe('zh-CN')
  })
  it('ships matching catalogs and interpolates actual values', () => {
    expect(Object.keys(catalogs['en-US']).sort()).toEqual(Object.keys(catalogs['zh-CN']).sort())
    expect(translator('en-US')('fileCount', { count: 2 })).toBe('2 input(s)')
    expect(translator('zh-CN')('queue')).toBe('队列')
    for (const catalog of Object.values(catalogs)) for (const message of Object.values(catalog)) expect(message.trim().length).toBeGreaterThan(0)
  })
})

describe('appearance preferences', () => {
  it('resolves system and explicit themes', () => {
    expect(resolvedTheme('system', true)).toBe('dark')
    expect(resolvedTheme('system', false)).toBe('light')
    expect(resolvedTheme('light', true)).toBe('light')
    expect(resolvedTheme('dark', false)).toBe('dark')
  })
  it('preserves exact approved palette values and readable accent foregrounds', () => {
    expect(themeTokens.light.canvas).toBe('#F5F3ED')
    expect(themeTokens.dark.canvas).toBe('#1F2521')
    for (const palette of Object.values(themeTokens)) {
      expect(contrast(palette.ink, palette.canvas)).toBeGreaterThanOrEqual(4.5)
      expect(contrast(palette.muted, palette.surface)).toBeGreaterThanOrEqual(4.5)
      expect(contrast(palette.accent, accentForeground(palette.accent))).toBeGreaterThanOrEqual(4.5)
    }
  })
  it('applies a local font only when available and falls back without downloading', () => {
    const settings = { ...defaults(['en-US']), theme: 'dark' as const, font_family: 'Local Font', accent_hex: '#123456' }
    applyAppearance(settings, ['Local Font'], false)
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.lang).toBe('en-US')
    expect(document.documentElement.style.getPropertyValue('--ui-font')).toContain('Local Font')
    expect(document.documentElement.style.getPropertyValue('--accent')).toBe('#123456')
    applyAppearance(settings, [], false)
    expect(document.documentElement.style.getPropertyValue('--ui-font')).toBe('system-ui, sans-serif')
  })
  it('rejects invalid accents, concurrency, insecure mirrors and non-local engine overrides', () => {
    expect(validAccent('red')).toBe(false)
    expect(validAccent('#fff')).toBe(false)
    expect(validAccent('')).toBe(true)
    expect(settingsError({ ...defaults(), max_concurrent_jobs: 1.5 })).toBe('invalidConcurrency')
    expect(settingsError({ ...defaults(), update_source: 'mirror', update_mirror_url: 'http://mirror.test' })).toBe('invalidMirror')
    expect(settingsError({ ...defaults(), update_source: 'mirror', update_mirror_url: 'https://user:pass@mirror.test' })).toBe('invalidMirror')
    expect(settingsError({ ...defaults(), ffmpeg_path: 'https://example.test/ffmpeg' })).toBe('invalidFFmpegPath')
  })
})

describe('current and latest release comparison', () => {
  it('does not offer equal or older versions as updates', () => {
    expect(compareVersions('v1.2.0', '1.2.0')).toBe(0)
    expect(compareVersions('1.1.9', '1.2.0')).toBe(-1)
    expect(compareVersions('1.10.0', '1.2.0')).toBe(1)
    expect(compareVersions('1.2.0', '1.2.0-rc.1')).toBe(1)
    expect(compareVersions('1.2.0-rc.2', '1.2.0-rc.10')).toBe(-1)
    expect(compareVersions('1.0.0', 'dev')).toBeNull()
  })
})
