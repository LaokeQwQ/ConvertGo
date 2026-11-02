import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import Settings from './Settings.svelte'
import { settings } from '../test/fixtures'

const backend = vi.hoisted(() => ({ saveSettings: vi.fn(), selectFFmpegPath: vi.fn(), checkForUpdate: vi.fn(), downloadUpdate: vi.fn() }))
vi.mock('../lib/api', () => ({ api: backend, errorText: (cause: unknown) => cause instanceof Error ? cause.message : String(cause) }))

beforeEach(() => {
  vi.resetAllMocks()
  backend.saveSettings.mockResolvedValue(undefined)
  backend.selectFFmpegPath.mockResolvedValue('/opt/ffmpeg/bin/ffmpeg')
  backend.checkForUpdate.mockResolvedValue({ version: '1.2.0', assets: [], signature: 'test-signature' })
  backend.downloadUpdate.mockResolvedValue('/downloads/ConvertGo.dmg')
})

function props(overrides: Record<string, unknown> = {}) {
  return {
    locale: 'en-US' as const, settings: { ...settings }, fonts: ['Local Font'], currentVersion: '1.0.0',
    onRetry: vi.fn(), onSaved: vi.fn(), onPreview: vi.fn(), ...overrides,
  }
}

describe('settings controls and error handling', () => {
  it('shows loading and blocking local-file recovery states', async () => {
    const callbacks = props({ loading: true })
    const view = render(Settings, callbacks)
    expect(screen.getByText('Loading local settings…')).toBeTruthy()
    await view.rerender({ ...callbacks, loading: false, settings: null, loadError: 'settings /config/ConvertGo/settings.json is malformed' })
    expect(screen.getByRole('alert').textContent).toContain('/config/ConvertGo/settings.json')
    expect(screen.queryByRole('button', { name: 'Save settings' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Reload settings' }))
    expect(callbacks.onRetry).toHaveBeenCalledOnce()
    expect(backend.saveSettings).not.toHaveBeenCalled()
  })
  it('previews theme and language choices, and saves explicit local settings', async () => {
    const callbacks = props()
    render(Settings, callbacks)
    await fireEvent.change(screen.getByLabelText('Theme'), { target: { value: 'dark' } })
    await waitFor(() => expect(callbacks.onPreview).toHaveBeenLastCalledWith(expect.objectContaining({ theme: 'dark' })))
    await fireEvent.change(screen.getByLabelText('Interface language'), { target: { value: 'zh-CN' } })
    await fireEvent.change(screen.getByLabelText('Local interface font'), { target: { value: 'Local Font' } })
    await fireEvent.click(screen.getByLabelText('Prevent sleep while encoding'))
    await fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(backend.saveSettings).toHaveBeenCalledOnce())
    expect(backend.saveSettings.mock.calls[0][0]).toMatchObject({ theme: 'dark', locale: 'zh-CN', font_family: 'Local Font', prevent_sleep: true, auto_check_updates: false })
    expect(callbacks.onSaved).toHaveBeenCalledOnce()
  })
  it('validates custom accents before writing settings', async () => {
    render(Settings, props())
    await fireEvent.input(screen.getByLabelText('Custom progress color'), { target: { value: '#fff' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(backend.saveSettings).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Use theme default' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(backend.saveSettings).toHaveBeenCalledOnce())
  })
  it('uses the native executable picker and saves the override instead of silently falling back', async () => {
    render(Settings, props())
    await fireEvent.click(screen.getByRole('button', { name: 'Browse' }))
    await waitFor(() => expect((screen.getByLabelText('FFmpeg executable override') as HTMLInputElement).value).toBe('/opt/ffmpeg/bin/ffmpeg'))
    await fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(backend.saveSettings).toHaveBeenCalledWith(expect.objectContaining({ ffmpeg_path: '/opt/ffmpeg/bin/ffmpeg' })))
  })
  it('resets a mirror to GitHub and leaves automatic checks opt-in', async () => {
    render(Settings, props({ settings: { ...settings, update_source: 'mirror', update_mirror_url: 'https://mirror.test' } }))
    expect((screen.getByLabelText('Automatically check for updates (opt in)') as HTMLInputElement).checked).toBe(false)
    await fireEvent.click(screen.getByRole('button', { name: 'Restore default update source' }))
    expect(screen.queryByLabelText('Mirror HTTPS base URL')).toBeNull()
    expect((screen.getByRole('button', { name: 'Check for update' }) as HTMLButtonElement).disabled).toBe(true)
    await fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(backend.saveSettings).toHaveBeenCalledWith(expect.objectContaining({ update_source: 'github', update_mirror_url: '', auto_check_updates: false })))
    expect(backend.checkForUpdate).not.toHaveBeenCalled()
  })
  it('requires explicit confirmation before a verified update download', async () => {
    render(Settings, props())
    await fireEvent.click(screen.getByRole('button', { name: 'Check for update' }))
    expect(await screen.findByText('Latest release: 1.2.0')).toBeTruthy()
    expect(screen.getByText('Current version: 1.0.0')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Download verified update' }))
    expect(backend.downloadUpdate).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm download' }))
    await waitFor(() => expect(backend.downloadUpdate).toHaveBeenCalledWith('1.2.0'))
    expect(await screen.findByText(/Verified update downloaded: \/downloads\/ConvertGo.dmg/)).toBeTruthy()
  })
  it('does not offer equal release versions as updates and exposes backend failures', async () => {
    backend.checkForUpdate.mockResolvedValueOnce({ version: '1.0.0', assets: [], signature: 'test-signature' })
    render(Settings, props())
    await fireEvent.click(screen.getByRole('button', { name: 'Check for update' }))
    expect(await screen.findByText(/No newer release is available/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Download verified update' })).toBeNull()
    backend.saveSettings.mockRejectedValueOnce(new Error('Settings file is read-only'))
    await fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Settings file is read-only')
  })
})
