<script lang="ts">
  import { onMount } from 'svelte'
  import { api, errorText, subscribeFiles, subscribeJobs } from '../lib/api'
  import { translator } from '../lib/i18n'
  import { parseEncoderOptions } from '../lib/encoder-options'
  import { defaultOutput, fileName, formatTime, isLocalPath, makeSpec, progressPercent, validateConversion } from '../lib/queue'
  import type { ConversionOptions } from '../lib/queue'
  import type { Job, JobSpec, Locale, MediaStream, Profile, SourceFile } from '../lib/types'
  import { concatSubtitleModesAllowed, hardwareBackends, subtitleSupported, supportedProfile } from '../lib/capabilities'
  import type { Capabilities } from '../lib/capabilities'

  export let locale: Locale
  export let configurationRevision = 0
  export let blocked = false
  let sources: SourceFile[] = []
  let jobs: Job[] = []
  let profiles: Profile[] = []
  let capabilities: Capabilities | null = null
  let draftProfile: Profile | null = null
  let profileID = ''
  let loading = true
  let profilesLoading = true
  let loadError = ''
  let profileError = ''
  let error = ''
  let notice = ''
  let picking = false
  let pathEditor = false
  let pathText = ''
  let enqueueing = false
  let starting = false
  let busyJobs: string[] = []
  let profileName = ''
  let videoOptionsText = ''
  let audioOptionsText = ''
  $: if (configurationRevision > 0) void loadProfiles()
  let savingProfile = false
  let mode: JobSpec['mode'] = 'individual'
  let concatOutput = ''
  let videoStream: number | null = null
  let audioStreams: number[] = []
  let subtitleStream: number | null = null
  let subtitleMode: JobSpec['subtitleMode'] = 'none'
  let trimStart = 0
  let trimEnd = 0
  let cropEnabled = false
  let crop = { x: 0, y: 0, width: 0, height: 0 }
  let width = 0
  let height = 0
  let frameRate = ''
  let deinterlace = false
  let hardwareDecode = 'auto'
  let live = true
  let changedDuringLoad = new Set<string>()

  $: t = translator(locale)
	function displayProfileName(item: Profile): string {
		const key = ({
			'mp4-h264-aac': 'presetH264',
			'mp4-hevc-aac': 'presetHevc',
			'webm-vp9-opus': 'presetVp9',
			'audio-mp3': 'presetMp3',
			'audio-aac': 'presetAac',
			'audio-flac': 'presetFlac',
		} as const)[item.id]
		return key ? t(key) : item.name
	}
  $: profile = draftProfile || profiles.find(item => item.id === profileID)
  $: streamList = sources.find(source => source.info)?.info?.streams ?? []
  $: options = { mode, profile, videoStream, audioStreams, subtitleStream, subtitleMode, trimStart, trimEnd, cropEnabled, crop, width, height, frameRate, deinterlace, hardwareDecode, concatOutput } satisfies ConversionOptions
  $: ready = sources.length > 0 && sources.every(source => source.state === 'ready')
  $: supportedSetup = !!profile && !!capabilities && supportedProfile(profile, capabilities)
  $: queued = jobs.filter(job => job.state === 'queued').length
  $: videoEncoders = capabilities ? ['', ...capabilities.videoEncoders.map(encoder => encoder.name)] : []
  $: audioEncoders = capabilities ? ['', ...capabilities.audioEncoders.map(encoder => encoder.name)] : []
  $: containers = capabilities?.containers ?? []
  $: decoderBackends = capabilities ? hardwareBackends(sources, videoStream, capabilities) : []
  $: concatSubtitleAllowed = concatSubtitleModesAllowed(mode, capabilities)
  $: subtitleCopyAllowed = concatSubtitleAllowed && subtitleSupported(sources, subtitleStream, capabilities?.subtitleCopy[profile?.container || ''] ?? [])
  $: subtitleBurnAllowed = concatSubtitleAllowed && !!profile?.video_encoder && subtitleSupported(sources, subtitleStream, capabilities?.subtitleBurn ?? [])

  onMount(() => {
    live = true
    const stopJobs = subscribeJobs(job => {
      if (loading) changedDuringLoad.add(job.id)
      jobs = jobs.some(item => item.id === job.id) ? jobs.map(item => item.id === job.id ? job : item) : [...jobs, job]
    }, (id, progress) => {
      if (loading) changedDuringLoad.add(id)
      jobs = jobs.map(job => job.id === id ? { ...job, progress } : job)
    })
    const stopFiles = subscribeFiles(paths => { void addPaths(paths) })
    void loadQueue()
    void loadProfiles()
    return () => { live = false; stopJobs(); stopFiles() }
  })

  async function loadQueue() {
    loading = true; loadError = ''; changedDuringLoad = new Set()
    try {
      const loaded = await api.getJobs()
      if (live) jobs = [...loaded.filter(job => !changedDuringLoad.has(job.id)), ...jobs.filter(job => changedDuringLoad.has(job.id))]
    } catch (cause) { if (live) loadError = errorText(cause) }
    finally { if (live) loading = false }
  }

  async function loadProfiles() {
    profilesLoading = true; profileError = ''
    const selected = profile
    try {
      const [loaded, detected] = await Promise.all([api.listProfiles(), api.getCapabilities()])
      if (!live) return
      capabilities = detected
      profiles = loaded.filter(item => supportedProfile(item, detected))
      if (selected && !supportedProfile(selected, detected)) {
        draftProfile = selected; profileID = ''; profileError = t('profileUnavailable')
      } else if (!selected) chooseProfile(profiles[0]?.id ?? '')
    } catch (cause) { if (live) { profileError = errorText(cause); capabilities = null; profiles = []; draftProfile = selected || null; profileID = '' } }
    finally { if (live) profilesLoading = false }
  }

  function chooseProfile(id: string, override?: Profile) {
    const previous = profile
    const next = override || profiles.find(item => item.id === id)
    profileID = id
    draftProfile = override || null
    if (!next) return
    if (capabilities && supportedProfile(next, capabilities)) profileError = ''
    videoOptionsText = Object.entries(next.video_options || {}).map(([key, value]) => `${key}=${value}`).join('\n')
    audioOptionsText = Object.entries(next.audio_options || {}).map(([key, value]) => `${key}=${value}`).join('\n')
    sources = sources.map(source => !source.output || (previous && source.output === defaultOutput(source.path, previous.container))
      ? { ...source, output: defaultOutput(source.path, next.container) } : source)
    if (!next.video_encoder) { videoStream = null; subtitleMode = 'none'; subtitleStream = null; cropEnabled = false; width = 0; height = 0; frameRate = ''; deinterlace = false }
    if (!next.audio_encoder) audioStreams = []
  }

  function chooseCapability(field: 'container' | 'video_encoder' | 'audio_encoder', value: string) {
    if (profile) chooseProfile('', { ...profile, [field]: value, id: '', name: t('customSetup') })
  }

  async function openFiles() {
    picking = true; error = ''
    try { await addPaths(await api.selectFiles()) }
    catch (cause) { pathEditor = true; error = `${t('filePickerError')} ${errorText(cause)}` }
    finally { picking = false }
  }

  async function inspect(path: string) {
    sources = sources.map(source => source.path === path ? { ...source, state: 'probing', error: '' } : source)
    try {
      const info = await api.probeFile(path)
      if (live) sources = sources.map(source => source.path === path ? { ...source, state: 'ready', info } : source)
    } catch (cause) {
      if (live) sources = sources.map(source => source.path === path ? { ...source, state: 'error', error: errorText(cause) } : source)
    }
  }

  async function addPaths(paths: string[]) {
    if (enqueueing) return
    const cleaned = paths.map(path => path.trim()).filter(Boolean)
    if (!cleaned.length) return
    if (cleaned.some(path => !isLocalPath(path))) { error = t('invalidPaths'); return }
    const added = [...new Set(cleaned)].filter(path => !sources.some(source => source.path === path))
    sources = [...sources, ...added.map(path => ({ path, state: 'probing' as const, output: profile ? defaultOutput(path, profile.container) : '' }))]
    pathText = ''; pathEditor = false; error = ''; notice = ''
    await Promise.all(added.map(inspect))
  }

  function moveSource(index: number, direction: number) {
    const next = [...sources]
    ;[next[index], next[index + direction]] = [next[index + direction], next[index]]
    sources = next
  }

  function streamLabel(stream: MediaStream): string {
    return `#${stream.index} ${stream.codec}${stream.language ? ` (${stream.language})` : ''}${stream.width ? ` · ${stream.width}×${stream.height}` : ''}${stream.frameRate ? ` · ${stream.frameRate} fps` : ''}${stream.channels ? ` · ${stream.channels} ${t('channels')}` : ''}${stream.sampleRate ? ` · ${stream.sampleRate} Hz` : ''}`
  }

  async function enqueue() {
    if (blocked || enqueueing || profilesLoading) return
    error = ''; notice = ''
    const videoOptions = parseEncoderOptions(videoOptionsText)
    const audioOptions = parseEncoderOptions(audioOptionsText)
    if (!videoOptions || !audioOptions) { error = t('invalidEncoderOptions'); return }
    const snapshotOptions = { ...options, profile: profile ? { ...profile, video_options: videoOptions, audio_options: audioOptions } : undefined }
    const invalid = validateConversion(sources, snapshotOptions)
    if (!profile || !capabilities || !supportedProfile(profile, capabilities)) { error = t('invalidProfile'); return }
    if ((subtitleMode === 'copy' && !subtitleCopyAllowed) || (subtitleMode === 'burn' && !subtitleBurnAllowed)) { error = t('unsupportedSubtitle'); return }
    if (hardwareDecode.startsWith('backend:') && !decoderBackends.includes(hardwareDecode.slice(8))) { error = t('hardwareUnavailable'); return }
    if (invalid) { error = t(invalid.key, { path: invalid.path || '' }); return }
    enqueueing = true
    let added = 0
    const snapshot = [...sources]
    try {
      const groups = mode === 'concatenate' ? [snapshot] : snapshot.map(source => [source])
      for (const group of groups) {
        const spec = makeSpec(group, snapshotOptions)
        let job: Job
        try { job = await api.addJob(spec) }
        catch (cause) {
          if (!errorText(cause).startsWith('OUTPUT_EXISTS:')) throw cause
          if (!window.confirm(t('confirmOverwrite', { path: spec.outputPath }))) throw new Error(t('overwriteDeclined'))
          job = await api.addJob({ ...spec, overwriteConfirmed: true })
        }
        jobs = jobs.some(item => item.id === job.id) ? jobs.map(item => item.id === job.id ? job : item) : [...jobs, job]
        added++
        const completedPaths = new Set(group.map(source => source.path))
        sources = sources.filter(source => !completedPaths.has(source.path))
      }
      notice = t('queueAdded')
    } catch (cause) { error = `${added ? `${t('partialQueue', { count: added })} ` : ''}${errorText(cause)}` }
    finally { enqueueing = false }
  }

  async function startQueue() {
    if (blocked || starting) return
    starting = true; error = ''; notice = ''
    try { await api.startQueue() }
    catch (cause) { error = errorText(cause) }
    finally { starting = false }
  }

  async function jobAction(job: Job, action: 'cancel' | 'remove') {
    busyJobs = [...busyJobs, job.id]; error = ''
    try {
      if (action === 'cancel') await api.cancelJob(job.id)
      else { await api.removeJob(job.id); jobs = jobs.filter(item => item.id !== job.id) }
    } catch (cause) { error = errorText(cause) }
    finally { busyJobs = busyJobs.filter(id => id !== job.id) }
  }

  async function saveProfile() {
    if (blocked || savingProfile) return
    if (!profile || !profileName.trim()) { error = t('invalidProfileName'); return }
    if (!capabilities || !supportedProfile(profile, capabilities)) { error = t('invalidProfile'); return }
    savingProfile = true; error = ''; notice = ''
    const videoOptions = parseEncoderOptions(videoOptionsText)
    const audioOptions = parseEncoderOptions(audioOptionsText)
    if (!videoOptions || !audioOptions) { error = t('invalidEncoderOptions'); savingProfile = false; return }
    const custom = { ...profile, id: `custom-${crypto.randomUUID()}`, name: profileName.trim(), video_options: videoOptions, audio_options: audioOptions }
    try {
      await api.saveProfile(custom)
      profiles = [...profiles, custom]; draftProfile = null; profileID = custom.id; profileName = ''; notice = t('profileSaved')
    } catch (cause) { error = errorText(cause) }
    finally { savingProfile = false }
  }
</script>

<section class="screen queue-screen" aria-labelledby="queue-title">
  <header class="screen-heading">
    <div><h1 id="queue-title">{t('queueTitle')}</h1><p>{t('queueIntro')}</p></div>
    <div class="button-row"><button class="strong" disabled={picking || enqueueing} on:click={openFiles}>{t('addFiles')}</button><button aria-expanded={pathEditor} on:click={() => pathEditor = !pathEditor} disabled={enqueueing}>{t('paths')}</button></div>
  </header>
  {#if pathEditor}
    <form class="path-editor" on:submit|preventDefault={() => addPaths(pathText.split(/\r?\n/))}>
      <label for="file-paths">{t('paths')}</label>
      <textarea id="file-paths" rows="4" bind:value={pathText} required aria-describedby="paths-help"></textarea>
      <p class="hint" id="paths-help">{t('pathsHelp')}</p>
      <div class="button-row"><button class="strong" type="submit">{t('probeFiles')}</button><button type="button" on:click={() => pathEditor = false}>{t('close')}</button></div>
    </form>
  {/if}
  {#if loading}<p class="state-panel" role="status">{t('loadingQueue')}</p>{/if}
  {#if loadError}<div class="state-panel" role="alert"><h2>{t('queueLoadError')}</h2><p class="error-detail">{loadError}</p><button on:click={loadQueue}>{t('retryLoad')}</button></div>{/if}
  {#if !loading && !loadError && !sources.length && !jobs.length}
    <div class="empty-state"><h2>{t('emptyTitle')}</h2><p>{t('emptyHelp')}</p><button on:click={openFiles} disabled={picking}>{t('addFiles')}</button></div>
  {/if}

  {#if sources.length}
    <section class="source-section" aria-labelledby="sources-title">
      <div class="section-heading"><h2 id="sources-title">{t('sources')}</h2><p class="hint">{t('sourceHelp')}</p></div>
      <ol class="source-list">
        {#each sources as source, index (source.path)}
          <li class="source-row">
            <div class="source-heading"><strong title={source.path}>{fileName(source.path)}</strong><div class="row-actions">
              <button disabled={index === 0 || enqueueing} on:click={() => moveSource(index, -1)} aria-label={`${t('up')}: ${fileName(source.path)}`}>{t('up')}</button>
              <button disabled={index === sources.length - 1 || enqueueing} on:click={() => moveSource(index, 1)} aria-label={`${t('down')}: ${fileName(source.path)}`}>{t('down')}</button>
              <button disabled={enqueueing} aria-label={t('removeSource', { name: fileName(source.path) })} on:click={() => sources = sources.filter(item => item.path !== source.path)}>{t('remove')}</button>
            </div></div>
            <p class="file-path">{source.path}</p>
            {#if source.state === 'probing'}<p role="status">{t('probing')}</p>
            {:else if source.state === 'error'}<div role="alert"><p>{t('probeError')}</p><p class="error-detail">{source.error}</p><button on:click={() => inspect(source.path)} disabled={enqueueing}>{t('retry')}</button></div>
            {:else if source.info}
              <p class="media-summary">{source.info.container} · {t('duration')}: {formatTime(source.info.durationSeconds)} · {t('size')}: {(source.info.sizeBytes / 1024 / 1024).toFixed(1)} MiB</p>
              <ul class="stream-summary">{#each source.info.streams as stream}<li>{stream.type === 'video' ? t('video') : stream.type === 'audio' ? t('audio') : stream.type === 'subtitle' ? t('subtitle') : t('dataStream')} {streamLabel(stream)}</li>{/each}</ul>
              {#if mode === 'individual'}<label>{t('output')}<input bind:value={source.output} disabled={enqueueing} spellcheck="false" placeholder={t('outputPending')} /></label>{/if}
            {/if}
          </li>
        {/each}
      </ol>
    </section>
    <section class="conversion-section" aria-labelledby="conversion-title">
      <div class="section-heading"><h2 id="conversion-title">{t('conversion')}</h2><button on:click={loadProfiles} disabled={profilesLoading || enqueueing}>{t('refreshCapabilities')}</button></div>
      {#if profilesLoading}<p role="status">{t('loadingProfiles')}</p>{/if}
      {#if profileError}<div role="alert"><p class="error-detail">{profileError}</p><button on:click={loadProfiles}>{t('retry')}</button></div>{/if}
      {#if !profilesLoading && !profiles.length}<p role="status">{t('noProfiles')}</p>{/if}
      <fieldset disabled={enqueueing || savingProfile || profilesLoading || !profiles.length}>
        <div class="field-grid">
          <label>{t('mode')}<select bind:value={mode}><option value="individual">{t('individual')}</option><option value="concatenate">{t('concatenate')}</option></select></label>
			{#if mode === 'concatenate'}<label class="full-width">{t('output')}<input bind:value={concatOutput} spellcheck="false" required /></label>{/if}
			<label>{t('profile')}<select value={profileID} on:change={event => chooseProfile(event.currentTarget.value)}>{#if draftProfile}<option value="">{t('customSetup')}</option>{/if}{#each profiles as item}<option value={item.id}>{displayProfileName(item)}</option>{/each}</select></label>
          <label>{t('container')}<select value={profile?.container} on:change={event => chooseCapability('container', event.currentTarget.value)}>{#if profile && !containers.includes(profile.container)}<option value={profile.container} disabled>{profile.container} ({t('unavailable')})</option>{/if}{#each containers as value}<option value={value}>{value}</option>{/each}</select></label>
          <label>{t('videoEncoder')}<select value={profile?.video_encoder} on:change={event => chooseCapability('video_encoder', event.currentTarget.value)}>{#if profile && !videoEncoders.includes(profile.video_encoder)}<option value={profile.video_encoder} disabled>{profile.video_encoder} ({t('unavailable')})</option>{/if}{#each videoEncoders as value}<option value={value}>{value || t('noVideo')}</option>{/each}</select></label>
          <label>{t('audioEncoder')}<select value={profile?.audio_encoder} on:change={event => chooseCapability('audio_encoder', event.currentTarget.value)}>{#if profile && !audioEncoders.includes(profile.audio_encoder)}<option value={profile.audio_encoder} disabled>{profile.audio_encoder} ({t('unavailable')})</option>{/if}{#each audioEncoders as value}<option value={value}>{value || t('noAudio')}</option>{/each}</select></label>
          <label>{t('hardwareDecode')}<select bind:value={hardwareDecode}><option value="auto">{t('decodeAuto')}</option><option value="off">{t('decodeOff')}</option>{#if hardwareDecode.startsWith('backend:') && !decoderBackends.includes(hardwareDecode.slice(8))}<option value={hardwareDecode} disabled>{hardwareDecode.slice(8)} ({t('unavailable')})</option>{/if}{#each decoderBackends as backend}<option value={`backend:${backend}`}>{backend}</option>{/each}</select></label>
        </div>
        <details class="advanced-section">
          <summary>{t('streams')}</summary><p class="hint">{t('streamHelp')}</p>
          {#if mode === 'concatenate' && capabilities && !capabilities.concatSubtitles}<p class="hint">{t('concatSubtitleHelp')}</p>{/if}
          <div class="field-grid">
            {#if profile?.video_encoder}<label>{t('videoStream')}<select bind:value={videoStream}><option value={null}>{t('automatic')}</option>{#each streamList.filter(stream => stream.type === 'video') as stream}<option value={stream.index}>{streamLabel(stream)}</option>{/each}</select></label>{/if}
            {#if profile?.audio_encoder}<fieldset class="audio-streams"><legend>{t('audioStreams')}</legend>{#each streamList.filter(stream => stream.type === 'audio') as stream}<label class="check-field"><input type="checkbox" bind:group={audioStreams} value={stream.index} />{streamLabel(stream)}</label>{/each}</fieldset>{/if}
            {#if profile?.video_encoder}<label>{t('subtitleStream')}<select bind:value={subtitleStream} on:change={() => { if (subtitleStream === null) subtitleMode = 'none' }}><option value={null}>{t('none')}</option>{#each streamList.filter(stream => stream.type === 'subtitle') as stream}<option value={stream.index}>{streamLabel(stream)}</option>{/each}</select></label>
            <label>{t('subtitle')}<select bind:value={subtitleMode} disabled={subtitleStream === null}><option value="none">{t('none')}</option><option value="copy" disabled={!subtitleCopyAllowed}>{t('copy')}</option><option value="burn" disabled={!subtitleBurnAllowed}>{t('burn')}</option></select></label>{/if}
          </div>
        </details>
        <details class="advanced-section">
          <summary>{t('filters')}</summary>
          <div class="field-grid">
            <label>{t('start')} ({t('seconds')})<input type="number" min="0" step="any" bind:value={trimStart} /></label>
            <label>{t('end')} ({t('seconds')})<input type="number" min="0" step="any" bind:value={trimEnd} /></label>
          </div>
          {#if profile?.video_encoder}
            <label class="check-field"><input type="checkbox" bind:checked={cropEnabled} />{t('cropEnabled')}</label>
            {#if cropEnabled}<div class="field-grid four-columns"><label>{t('x')}<input type="number" min="0" step="1" bind:value={crop.x} /></label><label>{t('y')}<input type="number" min="0" step="1" bind:value={crop.y} /></label><label>{t('width')}<input type="number" min="1" step="1" bind:value={crop.width} /></label><label>{t('height')}<input type="number" min="1" step="1" bind:value={crop.height} /></label></div>{/if}
            <h3>{t('scale')}</h3><div class="field-grid"><label>{t('width')}<input type="number" min="0" step="1" bind:value={width} /></label><label>{t('height')}<input type="number" min="0" step="1" bind:value={height} /></label><label>{t('fps')}<input bind:value={frameRate} placeholder="24000/1001" /></label><label class="check-field"><input type="checkbox" bind:checked={deinterlace} />{t('deinterlace')}</label></div>
          {/if}
        </details>
        <details class="advanced-section">
          <summary>{t('encoderOptions')}</summary><p class="hint">{t('encoderOptionsHelp')}</p>
          <div class="field-grid"><label>{t('videoOptions')}<textarea rows="3" bind:value={videoOptionsText} spellcheck="false" disabled={!profile?.video_encoder}></textarea></label><label>{t('audioOptions')}<textarea rows="3" bind:value={audioOptionsText} spellcheck="false" disabled={!profile?.audio_encoder}></textarea></label></div>
        </details>
        <div class="profile-save"><label for="profile-name">{t('profileName')}</label><div class="inline-controls"><input id="profile-name" bind:value={profileName} /><button disabled={blocked || savingProfile || !supportedSetup} on:click={saveProfile}>{savingProfile ? t('saving') : t('saveProfile')}</button></div></div>
        <p class="hint">{t('outputHelp')}</p><div class="form-footer"><button class="strong" disabled={blocked || profilesLoading || !ready || !supportedSetup || enqueueing} on:click={enqueue}>{enqueueing ? t('saving') : t('queueJobs')}</button></div>
      </fieldset>
    </section>
  {/if}

  {#if error}<p role="alert" class="message error-detail">{error}</p>{/if}
  {#if notice}<p role="status" class="message">{notice}</p>{/if}
  {#if jobs.length}
    <section class="jobs-section" aria-labelledby="jobs-title">
      <div class="section-heading"><h2 id="jobs-title">{t('queuedJobs')} <span class="count">{jobs.length}</span></h2><button class="strong" disabled={blocked || !queued || starting || loading} on:click={startQueue}>{starting ? t('starting') : t('startQueue')}</button></div>
      <ol class="job-list">
        {#each jobs as job (job.id)}
          {@const percent = job.state === 'completed' ? 100 : progressPercent(job.progress?.outTime || 0, job.progress?.totalDuration || 0)}
          <li class="job-row" class:encoding={job.state === 'running'}>
            <div class="job-heading"><div class="job-name"><strong>{fileName(job.spec.outputPath)}</strong><span class="job-state">{t(job.state)}</span></div>
              <div class="row-actions">{#if job.state === 'running' || job.state === 'probing'}<button disabled={busyJobs.includes(job.id)} on:click={() => jobAction(job, 'cancel')}>{t('cancel')}</button>{:else}<button disabled={blocked || busyJobs.includes(job.id)} on:click={() => jobAction(job, 'remove')}>{t('remove')}</button>{/if}</div>
            </div>
            <p class="file-path">{job.spec.outputPath}</p><p class="hint">{job.spec.profile.name} · {t('fileCount', { count: job.spec.inputs.length })}</p>
            {#if job.state === 'running' || job.state === 'completed'}<div class="progress-line"><progress max="100" value={percent} aria-label={`${fileName(job.spec.outputPath)}: ${t(job.state)}`}></progress><span>{percent.toFixed(1)}%</span></div>{/if}
            {#if job.state === 'running'}<div class="progress-details"><span>{t('encoded')}: {formatTime((job.progress?.outTime || 0) / 1e9)}</span>{#if job.progress?.speed > 0}<span>{t('speed')}: {job.progress.speed.toFixed(2)}×</span>{#if job.progress.totalDuration > 0}<span>{t('remaining')}: {formatTime(Math.max(0, (job.progress.totalDuration - job.progress.outTime) / 1e9 / job.progress.speed))}</span>{/if}{/if}</div>{/if}
            {#if job.state === 'completed'}<p>{t('completedHelp')}</p>{/if}
            {#if job.state === 'failed'}<div role="alert"><p class="error-detail">{job.error}</p><p>{t('failedHelp')}</p></div>{/if}
            {#if job.state === 'cancelled'}<p>{t('cancelledHelp')}</p>{/if}
          </li>
        {/each}
      </ol>
    </section>
  {/if}
</section>
