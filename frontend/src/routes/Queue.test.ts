import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import Queue from './Queue.svelte'
import { capabilities, job, media, profile } from '../test/fixtures'
import type { Job, Progress } from '../lib/types'

const state = vi.hoisted(() => ({
  onJob: null as ((value: Job) => void) | null,
  onProgress: null as ((id: string, value: Progress) => void) | null,
  onFiles: null as ((paths: string[]) => void) | null,
  api: {
    getJobs: vi.fn(), listProfiles: vi.fn(), selectFiles: vi.fn(), probeFile: vi.fn(),
    getCapabilities: vi.fn(),
    addJob: vi.fn(), startQueue: vi.fn(), cancelJob: vi.fn(), removeJob: vi.fn(), saveProfile: vi.fn(),
  },
}))
vi.mock('../lib/api', () => ({
  api: state.api, errorText: (cause: unknown) => cause instanceof Error ? cause.message : String(cause),
  subscribeJobs: (changed: typeof state.onJob, progress: typeof state.onProgress) => { state.onJob = changed; state.onProgress = progress; return vi.fn() },
  subscribeFiles: (files: typeof state.onFiles) => { state.onFiles = files; return vi.fn() },
}))

beforeEach(() => {
  vi.resetAllMocks()
  state.api.getJobs.mockResolvedValue([])
  state.api.listProfiles.mockResolvedValue([profile])
  state.api.getCapabilities.mockResolvedValue(capabilities)
  state.api.selectFiles.mockResolvedValue([media.path])
  state.api.probeFile.mockResolvedValue(media)
  state.api.addJob.mockImplementation(async spec => ({ ...job(), spec }))
  state.api.startQueue.mockResolvedValue(undefined)
  state.api.cancelJob.mockResolvedValue(undefined)
  state.api.removeJob.mockResolvedValue(undefined)
  state.api.saveProfile.mockResolvedValue(undefined)
})

async function addSource() {
  await fireEvent.click(screen.getAllByRole('button', { name: 'Add files' })[0])
  await screen.findByLabelText('Output file')
}

describe('queue states and real operations', () => {
  it('shows a specific empty state and a native file add action', async () => {
    render(Queue, { locale: 'en-US' })
    expect(await screen.findByText('No files in the queue')).toBeTruthy()
    await addSource()
    expect(state.api.selectFiles).toHaveBeenCalledOnce()
    expect(state.api.probeFile).toHaveBeenCalledWith(media.path)
  })
  it('shows queue loading and recovers from a backend error', async () => {
    const deferred = Promise.withResolvers<Job[]>()
    state.api.getJobs.mockReturnValueOnce(deferred.promise)
    render(Queue, { locale: 'en-US' })
    expect(screen.getByText('Loading the conversion queue…')).toBeTruthy()
    deferred.reject(new Error('Queue unavailable'))
    expect(await screen.findByText('Queue unavailable')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Reload queue' }))
    expect(await screen.findByText('No files in the queue')).toBeTruthy()
  })
  it('shows per-file probing, failure and retry without inventing metadata', async () => {
    const deferred = Promise.withResolvers<typeof media>()
    state.api.probeFile.mockReturnValueOnce(deferred.promise)
    render(Queue, { locale: 'en-US' })
    await screen.findByText('No files in the queue')
    await fireEvent.click(screen.getAllByRole('button', { name: 'Add files' })[0])
    expect(await screen.findByText('Inspecting media…')).toBeTruthy()
    deferred.reject(new Error('File does not exist'))
    expect(await screen.findByText('File does not exist')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByLabelText('Output file')).toBeTruthy()
  })
  it('adds a validated job with the approved stream, duration and overwrite defaults', async () => {
    render(Queue, { locale: 'en-US' })
    await screen.findByText('No files in the queue')
    await addSource()
    await fireEvent.click(screen.getByRole('button', { name: 'Add to queue' }))
    await waitFor(() => expect(state.api.addJob).toHaveBeenCalledOnce())
    expect(state.api.addJob.mock.calls[0][0]).toMatchObject({
      inputs: [media.path], mode: 'individual', videoStream: null, audioStreams: [],
      trimStart: 0, trimEnd: 0, subtitleMode: 'none', overwriteConfirmed: false,
    })
    await fireEvent.click(await screen.findByRole('button', { name: 'Start queue' }))
    expect(state.api.startQueue).toHaveBeenCalledOnce()
  })
  it('asks before replacing an existing output and only then retries with confirmation', async () => {
    const confirmation = vi.spyOn(window, 'confirm').mockReturnValue(true)
    state.api.addJob.mockRejectedValueOnce(new Error('OUTPUT_EXISTS:/media/source_converted.mp4'))
    render(Queue, { locale: 'en-US' })
    await screen.findByText('No files in the queue')
    await addSource()
    await fireEvent.click(screen.getByRole('button', { name: 'Add to queue' }))
    await waitFor(() => expect(state.api.addJob).toHaveBeenCalledTimes(2))
    expect(confirmation).toHaveBeenCalledOnce()
    expect(state.api.addJob.mock.calls[0][0].overwriteConfirmed).toBe(false)
    expect(state.api.addJob.mock.calls[1][0].overwriteConfirmed).toBe(true)
  })
  it('keeps the source intact when overwrite is declined', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    state.api.addJob.mockRejectedValueOnce(new Error('OUTPUT_EXISTS:/media/source_converted.mp4'))
    render(Queue, { locale: 'en-US' })
    await screen.findByText('No files in the queue')
    await addSource()
    await fireEvent.click(screen.getByRole('button', { name: 'Add to queue' }))
    await screen.findByRole('alert')
    expect(state.api.addJob).toHaveBeenCalledOnce()
    expect(screen.getByLabelText('Output file')).toBeTruthy()
  })
  it('subscribes to running progress, cancels and removes terminal jobs', async () => {
    state.api.getJobs.mockResolvedValue([job('running')])
    render(Queue, { locale: 'en-US' })
    const cancel = await screen.findByRole('button', { name: 'Cancel' })
    expect((screen.getByRole('progressbar') as HTMLProgressElement).value).toBe(25)
    state.onProgress?.('job-1', { frame: 200, outTime: 60e9, totalDuration: 120e9, speed: 2 })
    await waitFor(() => expect((screen.getByRole('progressbar') as HTMLProgressElement).value).toBe(50))
    await fireEvent.click(cancel)
    expect(state.api.cancelJob).toHaveBeenCalledWith('job-1')
    state.onJob?.(job('completed'))
    await fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    expect(state.api.removeJob).toHaveBeenCalledWith('job-1')
  })
  it('blocks job start when persisted settings are invalid', async () => {
    state.api.getJobs.mockResolvedValue([job()])
    render(Queue, { locale: 'en-US', blocked: true })
    const start = await screen.findByRole('button', { name: 'Start queue' })
    expect((start as HTMLButtonElement).disabled).toBe(true)
    expect(state.api.startQueue).not.toHaveBeenCalled()
  })
  it('does not enqueue incompatible concatenate inputs', async () => {
    state.api.selectFiles.mockResolvedValue([media.path, '/media/second.mp4'])
    state.api.probeFile.mockImplementation(async path => path === media.path ? media : { ...media, path, streams: [{ ...media.streams[0], width: 1280 }] })
    render(Queue, { locale: 'en-US' })
    await screen.findByText('No files in the queue')
    await fireEvent.click(screen.getAllByRole('button', { name: 'Add files' })[0])
    await waitFor(() => expect(screen.getAllByLabelText('Output file')).toHaveLength(2))
    await fireEvent.change(screen.getByLabelText('Conversion mode'), { target: { value: 'concatenate' } })
    await fireEvent.input(screen.getByLabelText('Output file'), { target: { value: '/media/joined.mp4' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Add to queue' }))
    expect(await screen.findByText(/Cannot concatenate/)).toBeTruthy()
    expect(state.api.addJob).not.toHaveBeenCalled()
  })
  it('preserves an explicit encoder selection when a refreshed device capability disappears', async () => {
    render(Queue, { locale: 'en-US' })
    await screen.findByText('No files in the queue')
    await addSource()
    state.api.getCapabilities.mockResolvedValue({ ...capabilities, videoEncoders: [] })
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh capabilities' }))
    expect(await screen.findByText(/no encoder was silently substituted/)).toBeTruthy()
    expect((screen.getByLabelText('Video encoder') as HTMLSelectElement).value).toBe('libx264')
    expect((screen.getByRole('button', { name: 'Add to queue' }) as HTMLButtonElement).disabled).toBe(true)
    expect(state.api.addJob).not.toHaveBeenCalled()
  })
})
