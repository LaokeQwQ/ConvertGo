import {
  ProbeFile, SelectFiles, SelectFFmpegPath, AddJob, GetJobs, StartQueue, CancelJob, RemoveJob, GetSleepInhibitionStatus,
  GetSettings, GetCurrentVersion, SaveSettings, ListProfiles, SaveProfile, ListFonts, CheckForUpdate, DownloadUpdate,
} from '../../wailsjs/go/main/App.js'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime.js'
import type { Job, JobSpec, MediaInfo, Profile, Progress, Release, Settings } from './types'
import { GetCapabilities } from '../../wailsjs/go/main/App.js'
import type { Capabilities } from './capabilities'

export const api = {
  selectFiles: async (): Promise<string[]> => SelectFiles(),
  selectFFmpegPath: async (): Promise<string> => SelectFFmpegPath(),
  probeFile: async (path: string): Promise<MediaInfo> => await ProbeFile(path) as unknown as MediaInfo,
  addJob: async (spec: JobSpec): Promise<Job> => await AddJob(spec as Parameters<typeof AddJob>[0]) as unknown as Job,
  getJobs: async (): Promise<Job[]> => (await GetJobs() ?? []) as unknown as Job[],
  startQueue: async (): Promise<void> => StartQueue(),
  cancelJob: async (id: string): Promise<void> => CancelJob(id),
  removeJob: async (id: string): Promise<void> => RemoveJob(id),
  getSettings: async (): Promise<Settings> => await GetSettings() as unknown as Settings,
  saveSettings: async (settings: Settings): Promise<void> => SaveSettings(settings as Parameters<typeof SaveSettings>[0]),
  listProfiles: async (): Promise<Profile[]> => (await ListProfiles()) ?? [],
  saveProfile: async (profile: Profile): Promise<void> => SaveProfile(profile as Parameters<typeof SaveProfile>[0]),
  listFonts: async (): Promise<string[]> => (await ListFonts()) ?? [],
  getCurrentVersion: async (): Promise<string> => GetCurrentVersion(),
  getCapabilities: async (): Promise<Capabilities> => {
    const detected = await GetCapabilities() as unknown as Capabilities
    return {
      containers: detected.containers ?? [], videoEncoders: detected.videoEncoders ?? [],
      audioEncoders: detected.audioEncoders ?? [], hardwareDecoders: detected.hardwareDecoders ?? [],
      subtitleCopy: detected.subtitleCopy ?? {}, subtitleBurn: detected.subtitleBurn ?? [],
      concatSubtitles: detected.concatSubtitles ?? false,
    }
  },
	getSleepInhibitionStatus: async () => await GetSleepInhibitionStatus() as { active: boolean; error: string },
  checkForUpdate: async (): Promise<Release> => CheckForUpdate(),
  downloadUpdate: async (version: string): Promise<string> => DownloadUpdate(version),
}

export function subscribeJobs(onJob: (job: Job) => void, onProgress: (id: string, progress: Progress) => void): () => void {
  EventsOn('job:changed', onJob)
  EventsOn('job:progress', (event: { jobID: string; progress: Progress }) => onProgress(event.jobID, event.progress))
  return () => { EventsOff('job:changed'); EventsOff('job:progress') }
}

export function subscribeFiles(onFiles: (paths: string[]) => void): () => void {
  EventsOn('files:dropped', onFiles)
  return () => EventsOff('files:dropped')
}

export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
