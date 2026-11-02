import { resolveLocale } from './i18n'
import { isLocalPath } from './queue'
import type { Settings, Theme } from './types'

export const themeTokens = {
  light: { canvas: '#F5F3ED', surface: '#FFFFFF', ink: '#242A26', muted: '#56605A', rule: '#D5D9D2', accent: '#A94F31' },
  dark: { canvas: '#1F2521', surface: '#2A322D', ink: '#EEF1EA', muted: '#B7C0B8', rule: '#46504A', accent: '#E89A74' },
} as const

export function defaults(languages: readonly string[] = []): Settings {
  return {
    locale: resolveLocale(languages), theme: 'system', accent_hex: '', font_family: '', ffmpeg_path: '',
    max_concurrent_jobs: 1, prevent_sleep: false, update_source: 'github',
    update_mirror_url: '', auto_check_updates: false,
  }
}

export function resolvedTheme(theme: Theme, systemDark: boolean): 'light' | 'dark' {
  return theme === 'system' ? (systemDark ? 'dark' : 'light') : theme
}

function luminance(hex: string): number {
  const values = [1, 3, 5].map(offset => {
    const value = parseInt(hex.slice(offset, offset + 2), 16) / 255
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  })
  return values[0] * 0.2126 + values[1] * 0.7152 + values[2] * 0.0722
}

export function contrast(first: string, second: string): number {
  const a = luminance(first)
  const b = luminance(second)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

export function accentForeground(hex: string): string {
  return contrast(hex, '#FFFFFF') >= contrast(hex, '#242A26') ? '#FFFFFF' : '#242A26'
}

export function validAccent(hex: string): boolean {
  return hex === '' || (/^#[\da-f]{6}$/i.test(hex) && contrast(hex, accentForeground(hex)) >= 4.5)
}

export function validMirror(value: string): boolean {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password && !url.search && !url.hash
  } catch {
    return false
  }
}

export function settingsError(settings: Settings): 'invalidAccent' | 'invalidConcurrency' | 'invalidMirror' | 'invalidFFmpegPath' | null {
  if (!validAccent(settings.accent_hex)) return 'invalidAccent'
  if (!Number.isInteger(settings.max_concurrent_jobs) || settings.max_concurrent_jobs < 1) return 'invalidConcurrency'
  if (settings.update_source === 'mirror' && !validMirror(settings.update_mirror_url)) return 'invalidMirror'
  if (settings.ffmpeg_path && !isLocalPath(settings.ffmpeg_path)) return 'invalidFFmpegPath'
  return null
}

export function applyAppearance(settings: Settings, fonts: readonly string[], systemDark: boolean): void {
  const root = document.documentElement
  const theme = resolvedTheme(settings.theme, systemDark)
  root.dataset.theme = theme
  root.lang = settings.locale
  root.style.colorScheme = theme
  const accent = validAccent(settings.accent_hex) && settings.accent_hex ? settings.accent_hex : themeTokens[theme].accent
  root.style.setProperty('--accent', accent)
  root.style.setProperty('--accent-ink', accentForeground(accent))
  const family = fonts.includes(settings.font_family) ? settings.font_family : ''
  root.style.setProperty('--ui-font', family ? `${JSON.stringify(family)}, system-ui, sans-serif` : 'system-ui, sans-serif')
}
