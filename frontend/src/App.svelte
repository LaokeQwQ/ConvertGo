<script lang="ts">
	import { onMount } from 'svelte'
	import { EventsOn } from '../wailsjs/runtime/runtime.js'
	import Queue from './routes/Queue.svelte'
	import SettingsScreen from './routes/Settings.svelte'
	import { api, errorText } from './lib/api'
	import { applyAppearance, defaults } from './lib/appearance'
	import { translator } from './lib/i18n'
	import type { Release, Settings, SleepInhibitionStatus } from './lib/types'

  let screen: 'queue' | 'settings' = 'queue'
  let settings: Settings | null = null
  let preview: Settings | null = null
  let loading = true
  let loadError = ''
  let fonts: string[] = []
  let fontError = ''
  let currentVersion = ''
  let versionError = ''
  let systemDark = false
  let active = false
  let automaticRelease: Release | null = null
  let automaticUpdateError = ''
	let automaticTimer: ReturnType<typeof setInterval> | undefined
	let sleepStatus: SleepInhibitionStatus = { active: false, error: '' }
  let updateGeneration = 0
  let configurationRevision = 0
  const initial = defaults(typeof navigator === 'undefined' ? [] : navigator.languages)

  $: appearance = preview || settings || initial
  $: t = translator(appearance.locale)
  $: applyAppearance(appearance, fonts, systemDark)

	onMount(() => {
	  active = true
	  const media = window.matchMedia('(prefers-color-scheme: dark)')
	  systemDark = media.matches
	  const changed = (event: MediaQueryListEvent) => { systemDark = event.matches }
	  media.addEventListener('change', changed)
	  const stopSleep = EventsOn('sleep:status', (status: SleepInhibitionStatus) => { sleepStatus = status })
	  void api.getSleepInhibitionStatus().then(status => { if (active) sleepStatus = status }).catch(() => {})
	  void loadSettings()
	  void loadFonts()
	  void api.getCurrentVersion().then(value => { if (active) currentVersion = value }).catch(cause => { if (active) versionError = errorText(cause) })
	  return () => {
	    active = false
	    media.removeEventListener('change', changed)
	    stopSleep()
	    clearInterval(automaticTimer)
	    updateGeneration++
	  }
	})

  async function loadSettings() {
    loading = true; loadError = ''
    try {
      const loaded = await api.getSettings()
      if (!active) return
      settings = loaded
      configureAutomaticChecks(loaded)
    } catch (cause) { if (active) { loadError = errorText(cause); settings = null } }
    finally { if (active) loading = false }
  }

  async function loadFonts() {
    fontError = ''
    try { const loaded = await api.listFonts(); if (active) fonts = loaded }
    catch (cause) { if (active) { fonts = []; fontError = errorText(cause) } }
  }

  function configureAutomaticChecks(saved: Settings) {
    clearInterval(automaticTimer)
    const generation = ++updateGeneration
    automaticRelease = null; automaticUpdateError = ''
    if (!saved.auto_check_updates) return
    const check = async () => {
      try {
        const release = await api.checkForUpdate()
        if (generation === updateGeneration) { automaticRelease = release; automaticUpdateError = '' }
      } catch (cause) { if (generation === updateGeneration) automaticUpdateError = errorText(cause) }
    }
    void check()
    automaticTimer = setInterval(() => { void check() }, 6 * 60 * 60 * 1000)
  }

  function previewSettings(value: Settings | null) {
    preview = value
  }

  function settingsSaved(saved: Settings) {
    const engineChanged = settings?.ffmpeg_path !== saved.ffmpeg_path
    settings = saved; preview = null
    configureAutomaticChecks(saved)
    if (engineChanged) configurationRevision++
  }
</script>

<div class="application-shell">
  <header class="app-bar">
    <div class="wordmark">ConvertGo<span>{t('localOnly')}</span></div>
    <nav aria-label="ConvertGo">
      <button class:current={screen === 'queue'} aria-current={screen === 'queue' ? 'page' : undefined} on:click={() => screen = 'queue'}>{t('queue')}</button>
      <button class:current={screen === 'settings'} aria-current={screen === 'settings' ? 'page' : undefined} on:click={() => screen = 'settings'}>{t('settings')}</button>
    </nav>
  </header>
  <main>
    {#if loadError && screen === 'queue'}<div role="alert" class="settings-warning"><span>{t('settingsLoadError')}</span><button on:click={() => screen = 'settings'}>{t('settings')}</button></div>{/if}
    <div hidden={screen !== 'queue'}><Queue locale={appearance.locale} {configurationRevision} blocked={loading || !settings || !!loadError} /></div>
    {#if screen === 'settings'}
      <SettingsScreen locale={appearance.locale} {settings} {loading} {loadError} {fonts} {fontError} {automaticRelease} {automaticUpdateError}
        {currentVersion} {versionError} sleepStatusActive={sleepStatus.active} sleepStatusError={sleepStatus.error}
        onRetry={loadSettings} onSaved={settingsSaved} onPreview={previewSettings} />
    {/if}
  </main>
</div>
