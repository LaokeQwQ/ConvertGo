import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import App from './App.svelte'
import { capabilities, job, profile, settings } from './test/fixtures'

const backend = vi.hoisted(() => ({
  getSettings: vi.fn(), listFonts: vi.fn(), getCurrentVersion: vi.fn(), getSleepInhibitionStatus: vi.fn(), checkForUpdate: vi.fn(),
  saveSettings: vi.fn(), getJobs: vi.fn(), listProfiles: vi.fn(),
  getCapabilities: vi.fn(),
}))
vi.mock('./lib/api', () => ({
  api: backend, errorText: (cause: unknown) => cause instanceof Error ? cause.message : String(cause),
  subscribeJobs: () => vi.fn(), subscribeFiles: () => vi.fn(),
}))

beforeEach(() => {
  vi.resetAllMocks()
  backend.getSettings.mockResolvedValue({ ...settings })
	backend.getSleepInhibitionStatus.mockResolvedValue({ active: false, error: '' })
  backend.listFonts.mockResolvedValue([])
  backend.getCurrentVersion.mockResolvedValue('1.0.0')
  backend.getJobs.mockResolvedValue([])
  backend.listProfiles.mockResolvedValue([profile])
  backend.getCapabilities.mockResolvedValue(capabilities)
  backend.saveSettings.mockResolvedValue(undefined)
  backend.checkForUpdate.mockResolvedValue({ version: '1.1.0', assets: [], signature: 'test-signature' })
})

describe('application preference lifecycle', () => {
  it('does not check updates at startup without a persisted opt-in', async () => {
    render(App)
    await screen.findByText('No files in the queue')
    expect(backend.checkForUpdate).not.toHaveBeenCalled()
    expect(document.documentElement.lang).toBe('en-US')
  })
  it('checks only when opted in and never downloads automatically', async () => {
    backend.getSettings.mockResolvedValue({ ...settings, auto_check_updates: true })
    render(App)
    await waitFor(() => expect(backend.checkForUpdate).toHaveBeenCalledOnce())
    await fireEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(await screen.findByText('Latest release: 1.1.0')).toBeTruthy()
    expect(screen.queryByText('Downloading and verifying…')).toBeNull()
  })
  it('previews light and dark themes, then restores unsaved appearance on leaving Settings', async () => {
    render(App)
    await screen.findByText('No files in the queue')
    await fireEvent.click(screen.getByRole('button', { name: 'Settings' }))
    await fireEvent.change(screen.getByLabelText('Theme'), { target: { value: 'dark' } })
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'))
    await fireEvent.click(screen.getByRole('button', { name: 'Queue' }))
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('light'))
  })
  it('blocks queue writes and shows the actual malformed settings file path', async () => {
    backend.getSettings.mockRejectedValue(new Error('settings /config/ConvertGo/settings.json is malformed'))
    backend.getJobs.mockResolvedValue([job()])
    render(App)
    const start = await screen.findByRole('button', { name: /开始队列|Start queue/ })
    expect((start as HTMLButtonElement).disabled).toBe(true)
    await fireEvent.click(screen.getAllByRole('button', { name: /设置|Settings/ })[0])
    expect(await screen.findByText('settings /config/ConvertGo/settings.json is malformed')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /保存设置|Save settings/ })).toBeNull()
  })
})
