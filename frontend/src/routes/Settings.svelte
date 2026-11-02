<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, errorText } from '../lib/api'
  import { settingsError } from '../lib/appearance'
  import { translator } from '../lib/i18n'
  import { compareVersions } from '../lib/versions'
  import type { Locale, Release, Settings } from '../lib/types'

  export let locale: Locale
  export let settings: Settings | null
  export let loading = false
  export let loadError = ''
  export let fonts: string[] = []
  export let fontError = ''
  export let currentVersion = ''
  export let versionError = ''
  export let automaticRelease: Release | null = null
  export let automaticUpdateError = ''
  export let sleepStatusActive = false
  export let sleepStatusError = ''
  export let onRetry: () => void
  export let onSaved: (settings: Settings) => void
  export let onPreview: (settings: Settings | null) => void

  let draft: Settings | null = null
  let saving = false
  let checking = false
  let downloading = false
  let error = ''
  let notice = ''
  let release: Release | null = null
  let confirmDownload = false

  $: t = translator(locale)
  $: if (settings) draft = { ...settings }
  $: if (draft) onPreview(draft)
  $: sourceDirty = !!draft && !!settings && (draft.update_source !== settings.update_source || draft.update_mirror_url !== settings.update_mirror_url)
  $: availableRelease = release || automaticRelease
  $: versionComparison = availableRelease ? compareVersions(availableRelease.version, currentVersion) : null
  $: canDownload = !!availableRelease && (currentVersion === 'dev' || versionComparison === 1)
  $: sleepStatusLabel = sleepStatusError ? `${t('sleepUnavailable')} ${sleepStatusError}` : sleepStatusActive ? t('sleepActive') : draft?.prevent_sleep ? t('sleepWaiting') : t('sleepInactive')
  onDestroy(() => onPreview(null))

  async function save() {
    if (!draft || saving) return
    error = ''; notice = ''
    const invalid = settingsError(draft)
    if (invalid) { error = t(invalid); return }
    saving = true
    const snapshot = { ...draft, update_mirror_url: draft.update_source === 'github' ? '' : draft.update_mirror_url.trim() }
    try {
      await api.saveSettings(snapshot)
      onSaved(snapshot)
      notice = t('settingsSaved')
      if (sourceDirty) { release = null; confirmDownload = false }
    } catch (cause) { error = errorText(cause) }
    finally { saving = false }
  }

  async function browseFFmpeg() {
    error = ''
    try {
      const path = await api.selectFFmpegPath()
      if (path && draft) draft.ffmpeg_path = path
    } catch (cause) { error = errorText(cause) }
  }

  async function checkUpdate() {
    checking = true; error = ''; notice = ''; release = null; confirmDownload = false
    try { release = await api.checkForUpdate() }
    catch (cause) { error = errorText(cause) }
    finally { checking = false }
  }

  async function downloadUpdate() {
    if (!availableRelease || !canDownload || sourceDirty || downloading) return
    downloading = true; error = ''; confirmDownload = false
    try { notice = t('updateDownloaded', { path: await api.downloadUpdate(availableRelease.version) }) }
    catch (cause) { error = errorText(cause) }
    finally { downloading = false }
  }
</script>

<section class="screen settings-screen" aria-labelledby="settings-title">
  <header class="screen-heading">
    <div><h1 id="settings-title">{t('settingsTitle')}</h1><p>{t('settingsIntro')}</p></div>
  </header>
  {#if loading}
    <p role="status" class="state-panel">{t('loadingSettings')}</p>
  {:else if loadError || !settings || !draft}
    <div class="state-panel" role="alert">
      <h2>{t('settingsLoadError')}</h2><p class="error-detail">{loadError}</p>
      <p>{t('settingsRepairHelp')}</p>
      <button on:click={onRetry}>{t('reloadSettings')}</button>
    </div>
  {:else}
    <form on:submit|preventDefault={save}>
      <fieldset disabled={saving || downloading} class="settings-section">
        <legend>{t('appearance')}</legend>
        <div class="field-grid">
          <label>{t('language')}
            <select bind:value={draft.locale}><option value="zh-CN">简体中文</option><option value="en-US">English (US)</option></select>
          </label>
          <label>{t('theme')}
            <select bind:value={draft.theme}>
              <option value="system">{t('system')}</option><option value="light">{t('light')}</option><option value="dark">{t('dark')}</option>
            </select>
          </label>
          <div class="field full-width">
            <label for="accent">{t('accent')}</label>
            <div class="inline-controls">
              <input id="accent" bind:value={draft.accent_hex} placeholder="#RRGGBB" spellcheck="false" aria-describedby="accent-help" />
              <button type="button" on:click={() => { if (draft) draft.accent_hex = '' }}>{t('resetAccent')}</button>
            </div>
            <p class="hint" id="accent-help">{t('accentHelp')}</p>
          </div>
          <label class="full-width">{t('font')}
            <select bind:value={draft.font_family} aria-describedby="font-help">
              <option value="">{t('systemFont')}</option>
              {#if draft.font_family && !fonts.includes(draft.font_family)}<option value={draft.font_family}>{draft.font_family} ({t('systemFont')})</option>{/if}
              {#each fonts as family}<option value={family}>{family}</option>{/each}
            </select>
          </label>
        </div>
        <p class="hint" id="font-help">{t('fontHelp')}</p>
        {#if fontError}<p role="status">{t('fontError')} <span class="error-detail">{fontError}</span></p>{/if}
      </fieldset>
      <fieldset disabled={saving || downloading} class="settings-section">
        <legend>{t('processing')}</legend>
        <div class="field-grid">
          <div class="field full-width">
            <label for="ffmpeg-path">{t('ffmpegPath')}</label>
            <div class="inline-controls"><input id="ffmpeg-path" bind:value={draft.ffmpeg_path} spellcheck="false" aria-describedby="ffmpeg-help" /><button type="button" on:click={browseFFmpeg}>{t('browse')}</button></div>
            <p class="hint" id="ffmpeg-help">{t('ffmpegHelp')}</p>
          </div>
          <label>{t('concurrency')}
            <input type="number" min="1" step="1" required bind:value={draft.max_concurrent_jobs} aria-describedby="concurrency-help" />
          </label>
          <label class="check-field"><input type="checkbox" bind:checked={draft.prevent_sleep} />{t('preventSleep')}</label>
        </div>
        <p class="hint" id="concurrency-help">{t('concurrencyHelp')}</p>
        <p role="status">{sleepStatusLabel}</p>
      </fieldset>
      <fieldset disabled={saving || downloading} class="settings-section">
        <legend>{t('updates')}</legend>
        <p class="section-description">{t('updateHelp')}</p>
        <div class="field-grid">
          <label>{t('updateSource')}
            <select bind:value={draft.update_source}>
              <option value="github">{t('github')}</option><option value="mirror">{t('mirror')}</option>
            </select>
          </label>
          {#if draft.update_source === 'mirror'}
            <label>{t('mirrorURL')}<input type="url" bind:value={draft.update_mirror_url} placeholder="https://" required /></label>
          {/if}
        </div>
        <div class="button-row">
          <button type="button" on:click={() => { if (draft) { draft.update_source = 'github'; draft.update_mirror_url = '' } }}>{t('resetUpdateSource')}</button>
        </div>
        <label class="check-field"><input type="checkbox" bind:checked={draft.auto_check_updates} />{t('autoUpdates')}</label>
        {#if sourceDirty}<p class="hint">{t('saveSourceFirst')}</p>{/if}
        <div class="button-row">
          <button type="button" disabled={checking || sourceDirty} on:click={checkUpdate}>{checking ? t('checkingUpdate') : t('checkUpdate')}</button>
          {#if canDownload && !sourceDirty}
            <button type="button" disabled={checking} on:click={() => confirmDownload = true}>{t('downloadUpdate')}</button>
          {/if}
        </div>
        {#if currentVersion}<p>{t('currentVersion', { version: currentVersion })}</p>{/if}
        {#if versionError}<p role="status" class="error-detail">{versionError}</p>{/if}
        {#if availableRelease && !sourceDirty}<p role="status">{t('latestVersion', { version: availableRelease.version })}{versionComparison !== null && versionComparison <= 0 ? ` ${t('upToDate')}` : ''}</p>{/if}
        {#if automaticUpdateError}<p role="status" class="error-detail">{automaticUpdateError}</p>{/if}
      </fieldset>
      {#if confirmDownload && availableRelease && canDownload && !sourceDirty}
        <div class="confirmation" role="group" aria-labelledby="update-confirmation">
          <p id="update-confirmation">{t('confirmUpdate', { version: availableRelease.version })}</p>
          <div class="button-row"><button type="button" on:click={downloadUpdate}>{t('confirmDownload')}</button><button type="button" on:click={() => confirmDownload = false}>{t('cancel')}</button></div>
        </div>
      {/if}
      {#if downloading}<p role="status">{t('downloadingUpdate')}</p>{/if}
      {#if error}<p role="alert" class="message error-detail">{error}</p>{/if}
      {#if notice}<p role="status" class="message">{notice}</p>{/if}
      <div class="form-footer"><button class="strong" type="submit" disabled={saving || downloading}>{saving ? t('saving') : t('saveSettings')}</button></div>
    </form>
  {/if}
</section>
