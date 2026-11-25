<script lang="ts">
	import { onMount } from 'svelte';
	import type { BridgeState, SerialState } from './api.ts';
	import {
		clearHistory,
		fetchBridgeState,
		formatBytes,
		formatTime,
		loadConfig,
		normalizeUrl,
		saveConfig,
		serialBabystep,
		serialClearConsole,
		serialEStop,
		serialExtrude,
		serialHome,
		serialJog,
		serialMotorsOff,
		serialSendGCode,
		serialSetFan,
		serialSetTemp,
		updateBridgeSettings,
		uploadGCodeFile
	} from './api.ts';

	type TabId = 'control' | 'filament' | 'console' | 'upload';
	type ConfirmAction = 'estop' | 'home' | 'motors' | null;

	const COLD_EXTRUSION_MIN_C = 175;

	let activeTab = $state<TabId>('control');
	let bridgeUrl = $state('http://127.0.0.1:5000');
	let urlDraft = $state('http://127.0.0.1:5000');
	let stateData = $state<BridgeState | null>(null);
	let bridgeError = $state('');
	let busy = $state(false);
	let banner = $state('');
	let bannerKind = $state<'info' | 'ok' | 'err'>('info');

	// Two-tap safety confirmation state (auto-clears after 4s)
	let armedAction = $state<ConfirmAction>(null);
	let armTimer: ReturnType<typeof setTimeout> | null = null;

	// Machine control settings
	let jogStep = $state(10);
	let jogSpeedMode = $state<'slow' | 'normal' | 'fast'>('normal');
	let nozzleInput = $state('220');
	let bedInput = $state('60');
	let extrudeLen = $state(10);
	let extrudeSpeed = $state(180);
	let extrudeSpeedInput = $state('180');

	// Filament Change Wizard settings
	let wizardMaterial = $state<'PLA' | 'PETG'>('PLA');
	let wizardTarget = $state(220);
	let bowdenLen = $state(450);
	let wizardStepNote = $state('Step 1: Pick PLA or PETG and heat the nozzle before unloading or loading.');

	// Console
	let gcodeInput = $state('');

	// Upload
	let uploadPath = $state('');
	let uploadName = $state('');
	let uploading = $state(false);

	const serial = $derived<SerialState | null>(stateData?.serial ?? null);
	const serialReady = $derived(Boolean(serial?.enabled && serial?.connected));
	const wifiOnline = $derived(Boolean(stateData?.status?.reachable));
	const isPrinting = $derived(
		Boolean(serialReady && serial?.working_status && serial.working_status !== 'IDLE')
	);
	const controlsEnabled = $derived(serialReady && !busy && !isPrinting);
	const hotendHot = $derived(
		Boolean(serialReady && serial && serial.nozzle_actual >= COLD_EXTRUSION_MIN_C)
	);
	const extrudeEnabled = $derived(controlsEnabled && hotendHot);

	function xyFeedrate(): number {
		if (jogSpeedMode === 'slow') return 1200;
		if (jogSpeedMode === 'fast') return 6000;
		return 3000;
	}

	function zFeedrate(): number {
		if (jogSpeedMode === 'slow') return 600;
		if (jogSpeedMode === 'fast') return 3000;
		return 1500;
	}

	function setNotice(kind: 'info' | 'ok' | 'err', msg: string) {
		bannerKind = kind;
		banner = msg;
	}

	function applySerial(s?: SerialState) {
		if (s && stateData) {
			stateData = { ...stateData, serial: s };
		}
	}

	function clearArm() {
		if (armTimer) {
			clearTimeout(armTimer);
			armTimer = null;
		}
		armedAction = null;
	}

	function requireTwoTap(action: Exclude<ConfirmAction, null>, label: string, run: () => Promise<void>) {
		if (armedAction === action) {
			clearArm();
			void run();
			return;
		}
		clearArm();
		armedAction = action;
		setNotice('info', `Tap "${label}" again within 4s to confirm.`);
		armTimer = setTimeout(() => {
			armedAction = null;
			armTimer = null;
		}, 4000);
	}

	async function refresh(silent = false) {
		try {
			const data = await fetchBridgeState(bridgeUrl);
			stateData = data;
			bridgeError = '';
		} catch (err) {
			bridgeError = err instanceof Error ? err.message : String(err);
			if (!silent) {
				setNotice('err', `Bridge unreachable: ${bridgeError}`);
			}
		}
	}

	async function applyBridgeUrl() {
		const normalized = normalizeUrl(urlDraft);
		bridgeUrl = normalized;
		urlDraft = normalized;
		try {
			await saveConfig({ bridgeUrl: normalized });
		} catch {
			// Ignore local config save error.
		}
		setNotice('info', `Connecting to ${normalized}...`);
		await refresh(false);
		if (!bridgeError) {
			setNotice('ok', `Connected to ${normalized}`);
		}
	}

	async function runSerial(label: string, fn: () => Promise<{ serial?: SerialState }>) {
		if (!serialReady) {
			setNotice('err', 'USB serial is not connected on ivi-bridge.');
			return;
		}
		if (isPrinting) {
			setNotice('err', `Controls locked while printing (${serial?.working_status}).`);
			return;
		}
		busy = true;
		try {
			const res = await fn();
			applySerial(res.serial);
			setNotice('ok', label);
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		} finally {
			busy = false;
		}
	}

	async function doJog(axis: 'X' | 'Y' | 'Z', sign: 1 | -1) {
		const dist = jogStep * sign;
		const feed = axis === 'Z' ? zFeedrate() : xyFeedrate();
		await runSerial(`Jogged ${axis} ${dist > 0 ? '+' : ''}${dist} mm`, () =>
			serialJog(bridgeUrl, axis, dist, feed)
		);
	}

	async function doHome() {
		await runSerial('Homed all towers (G28)', () => serialHome(bridgeUrl));
	}

	async function doSetTemp(heater: 'nozzle' | 'bed', target: number) {
		const label = heater === 'nozzle' ? 'Nozzle' : 'Bed';
		await runSerial(`${label} target set to ${target}°C`, () =>
			serialSetTemp(bridgeUrl, heater, target)
		);
	}

	async function doCooldownAll() {
		if (!controlsEnabled) return;
		busy = true;
		try {
			await serialSetTemp(bridgeUrl, 'nozzle', 0);
			await serialSetTemp(bridgeUrl, 'bed', 0);
			const res = await serialSetFan(bridgeUrl, 0);
			applySerial(res.serial);
			setNotice('ok', 'Cooldown: Nozzle 0°C, Bed 0°C, Fan Off');
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		} finally {
			busy = false;
		}
	}

	async function doExtrude(sign: 1 | -1) {
		if (!hotendHot) {
			setNotice('err', `Cold extrusion blocked: heat nozzle to at least ${COLD_EXTRUSION_MIN_C}°C first.`);
			return;
		}
		const amount = extrudeLen * sign;
		const parsedSpeed = Number(extrudeSpeedInput);
		const speed = Number.isFinite(parsedSpeed) && parsedSpeed > 0 ? parsedSpeed : extrudeSpeed;
		const verb = sign > 0 ? 'Extruded' : 'Retracted';
		await runSerial(`${verb} ${extrudeLen} mm @ ${speed} mm/min`, () =>
			serialExtrude(bridgeUrl, amount, speed)
		);
	}

	async function doFan(percent: number) {
		await runSerial(`Part fan set to ${percent}%`, () => serialSetFan(bridgeUrl, percent));
	}

	async function doBabystep(delta: number) {
		await runSerial(`Z babystep ${delta > 0 ? '+' : ''}${delta.toFixed(2)} mm`, () =>
			serialBabystep(bridgeUrl, delta)
		);
	}

	async function doMotorsOff() {
		await runSerial('Motors disabled (M84)', () => serialMotorsOff(bridgeUrl));
	}

	async function doEStop() {
		try {
			const res = await serialEStop(bridgeUrl);
			applySerial(res.serial);
			setNotice('err', 'EMERGENCY STOP (M112) sent! Power-cycle or reset printer to resume.');
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		}
	}

	// Filament Change Wizard actions (chunked <= 150 mm per bridge call)
	async function extrudeChunked(totalMm: number, speed: number, label: string) {
		const sign = totalMm >= 0 ? 1 : -1;
		let remaining = Math.abs(totalMm);
		const chunkSize = 150;
		const totalChunks = Math.max(1, Math.ceil(remaining / chunkSize));
		let idx = 0;
		while (remaining > 0.01) {
			idx++;
			const step = Math.min(chunkSize, remaining) * sign;
			wizardStepNote = `${label} (${idx}/${totalChunks})...`;
			const res = await serialExtrude(bridgeUrl, step, speed);
			applySerial(res.serial);
			remaining -= Math.abs(step);
		}
	}

	async function wizardHeat(mat: 'PLA' | 'PETG', target: number) {
		wizardMaterial = mat;
		wizardTarget = target;
		nozzleInput = String(target);
		wizardStepNote = `Heating nozzle to ${target}°C for ${mat}... wait until actual temp ≥ ${COLD_EXTRUSION_MIN_C}°C.`;
		await doSetTemp('nozzle', target);
	}

	async function wizardUnload() {
		if (!extrudeEnabled) {
			setNotice('err', `Heat nozzle to ≥ ${COLD_EXTRUSION_MIN_C}°C before unloading.`);
			return;
		}
		busy = true;
		try {
			wizardStepNote = 'Tip-shaping micro-purge (+5 mm)...';
			await serialExtrude(bridgeUrl, 5, 120);
			wizardStepNote = 'Fast pull out of melt zone (-20 mm)...';
			await serialExtrude(bridgeUrl, -20, 900);
			await extrudeChunked(-bowdenLen, 1200, `Retracting Bowden (-${bowdenLen} mm)`);
			wizardStepNote = 'Unloaded! Pull old spool out, cut new filament at 45°, feed through filament sensor until gears grip, then click Load.';
			setNotice('ok', 'Filament unloaded cleanly.');
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			wizardStepNote = `Unload stopped: ${msg}`;
			setNotice('err', msg);
		} finally {
			busy = false;
		}
	}

	async function wizardLoad() {
		if (!extrudeEnabled) {
			setNotice('err', `Heat nozzle to ≥ ${COLD_EXTRUSION_MIN_C}°C before loading.`);
			return;
		}
		busy = true;
		try {
			const fastFeed = Math.max(50, bowdenLen - 30);
			await extrudeChunked(fastFeed, 1200, `Fast-feeding Bowden (+${fastFeed} mm)`);
			wizardStepNote = 'Slow purging melt zone (+40 mm @ 120 mm/min)...';
			const res = await serialExtrude(bridgeUrl, 40, 120);
			applySerial(res.serial);
			wizardStepNote = 'Loaded & purged! Click "Purge +15 mm" if color is not clean yet, or "Cooldown Nozzle" when done.';
			setNotice('ok', 'Filament loaded and purged.');
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			wizardStepNote = `Load stopped: ${msg}`;
			setNotice('err', msg);
		} finally {
			busy = false;
		}
	}

	async function wizardPurgeMore() {
		if (!extrudeEnabled) return;
		await runSerial('Purged +15 mm @ 120 mm/min', () => serialExtrude(bridgeUrl, 15, 120));
	}

	async function doSendGCode(cmd?: string) {
		const line = (cmd ?? gcodeInput).trim();
		if (!line) return;
		if (!cmd) gcodeInput = '';
		await runSerial(`Sent: ${line}`, () => serialSendGCode(bridgeUrl, line));
	}

	async function doClearConsole() {
		busy = true;
		try {
			const res = await serialClearConsole(bridgeUrl);
			applySerial(res.serial);
			setNotice('ok', 'Console cleared');
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		} finally {
			busy = false;
		}
	}

	async function toggleSetting(key: 'auto_transform' | 'unique_names') {
		if (!stateData) return;
		busy = true;
		try {
			const nextVal = !stateData[key];
			const updated = await updateBridgeSettings(bridgeUrl, { [key]: nextVal });
			stateData = {
				...stateData,
				auto_transform: updated.auto_transform,
				unique_names: updated.unique_names
			};
			setNotice('ok', `Updated ${key} = ${nextVal ? 'ON' : 'OFF'}`);
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		} finally {
			busy = false;
		}
	}

	async function handleUpload() {
		if (!uploadPath.trim()) {
			setNotice('err', 'Enter a local .gcode file path to upload.');
			return;
		}
		uploading = true;
		setNotice('info', 'Transforming & uploading G-code...');
		try {
			const res = await uploadGCodeFile(bridgeUrl, uploadPath, uploadName);
			setNotice('ok', `Uploaded ${res.record.sent_name} (${formatBytes(res.record.size_bytes)})`);
			uploadPath = '';
			uploadName = '';
			await refresh(true);
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		} finally {
			uploading = false;
		}
	}

	async function handleClearHistory() {
		busy = true;
		try {
			await clearHistory(bridgeUrl);
			setNotice('ok', 'Cleared bridge upload history.');
			await refresh(true);
		} catch (err) {
			setNotice('err', err instanceof Error ? err.message : String(err));
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		let stopped = false;
		void (async () => {
			const cfg = await loadConfig();
			if (stopped) return;
			bridgeUrl = cfg.bridgeUrl;
			urlDraft = cfg.bridgeUrl;
			await refresh(true);
		})();
		const timer = setInterval(() => {
			if (!stopped) void refresh(true);
		}, 2000);
		return () => {
			stopped = true;
			clearArm();
			clearInterval(timer);
		};
	});
</script>

<div class="root">
	<!-- Top Bar -->
	<div class="header">
		<div class="brand-row">
			<div class="brand-col">
				<text class="title">IVI Control Panel</text>
				<text class="subtitle">
					{serial?.firmware ? serial.firmware : 'Shared Wi-Fi + USB Marlin Control via ivi-bridge'}
				</text>
			</div>

			<div class="header-right">
				<div class={wifiOnline ? 'badge badge-ok' : 'badge badge-off'}>
					<text class="badge-text">{wifiOnline ? 'Wi-Fi: Online' : 'Wi-Fi: Offline'}</text>
				</div>
				<div class={isPrinting ? 'badge badge-print' : serialReady ? 'badge badge-ok' : 'badge badge-warn'}>
					<text class="badge-text">
						{serialReady ? `USB: ${serial?.working_status || 'Connected'}` : 'USB: Disconnected'}
					</text>
				</div>
				<button
					class={armedAction === 'estop' ? 'btn-estop-armed' : 'btn-estop'}
					onclick={() => requireTwoTap('estop', 'Confirm E-STOP?', doEStop)}
				>
					<text class="btn-estop-text">
						{armedAction === 'estop' ? 'CONFIRM E-STOP!' : 'E-STOP (M112)'}
					</text>
				</button>
			</div>
		</div>

		<div class="conn-row">
			<text class="conn-label">Bridge:</text>
			<div class="conn-input-wrap">
				<input
					class="input"
					value={urlDraft}
					placeholder="http://127.0.0.1:5000"
					onchange={(e) => (urlDraft = e.value)}
				/>
			</div>
			<button class="btn-secondary" onclick={applyBridgeUrl}>
				<text class="btn-text">Connect</text>
			</button>
			<button class="btn-secondary" onclick={() => refresh(false)}>
				<text class="btn-text">Refresh</text>
			</button>
		</div>
	</div>

	<!-- Print-Active Safety Lockout Banner -->
	{#if isPrinting}
		<div class="lock-banner">
			<text class="lock-banner-text">
				PRINT IN PROGRESS ({serial?.working_status}) — Monitor-only mode: motion, heater, filament, and console controls are locked to protect your print.
			</text>
		</div>
	{/if}

	<!-- Live Telemetry Strip -->
	<div class="telemetry-strip">
		<div class="tele-card">
			<text class="tele-label">NOZZLE TEMP</text>
			<div class="tele-val-row">
				<text class={serial && serial.nozzle_target > 0 ? 'tele-val tele-hot' : 'tele-val'}>
					{serialReady && serial ? `${serial.nozzle_actual.toFixed(1)}°C` : '—'}
				</text>
				<text class="tele-sub">
					{serialReady && serial ? `/ ${serial.nozzle_target.toFixed(0)}°C` : ''}
				</text>
			</div>
		</div>

		<div class="tele-card">
			<text class="tele-label">BED TEMP</text>
			<div class="tele-val-row">
				<text class={serial && serial.bed_target > 0 ? 'tele-val tele-warm' : 'tele-val'}>
					{serialReady && serial ? `${serial.bed_actual.toFixed(1)}°C` : '—'}
				</text>
				<text class="tele-sub">
					{serialReady && serial ? `/ ${serial.bed_target.toFixed(0)}°C` : ''}
				</text>
			</div>
		</div>

		<div class="tele-card tele-card-wide">
			<text class="tele-label">TOOLHEAD POSITION (MM)</text>
			<div class="tele-val-row">
				<text class="tele-val-mono">
					{serialReady && serial
						? `X ${serial.x.toFixed(2)}   Y ${serial.y.toFixed(2)}   Z ${serial.z.toFixed(2)}`
						: '—'}
				</text>
			</div>
		</div>

		<div class="tele-card">
			<text class="tele-label">STATUS / FAN</text>
			<div class="tele-val-row">
				<text class="tele-val-sm">
					{serialReady && serial
						? `${serial.at_home ? 'Homed' : serial.homed ? 'Moved' : 'Unhomed'} · Fan ${serial.fan_percent}%`
						: 'No USB'}
				</text>
			</div>
		</div>
	</div>

	<!-- Tabs + Notice -->
	<div class="tab-bar">
		<div class="tabs-left">
			<button
				class={activeTab === 'control' ? 'tab-btn tab-active' : 'tab-btn'}
				onclick={() => (activeTab = 'control')}
			>
				<text class={activeTab === 'control' ? 'tab-text-active' : 'tab-text'}>
					Machine Control
				</text>
			</button>
			<button
				class={activeTab === 'filament' ? 'tab-btn tab-active' : 'tab-btn'}
				onclick={() => (activeTab = 'filament')}
			>
				<text class={activeTab === 'filament' ? 'tab-text-active' : 'tab-text'}>
					Filament Wizard
				</text>
			</button>
			<button
				class={activeTab === 'console' ? 'tab-btn tab-active' : 'tab-btn'}
				onclick={() => (activeTab = 'console')}
			>
				<text class={activeTab === 'console' ? 'tab-text-active' : 'tab-text'}>
					G-Code Console ({serial?.console?.length ?? 0})
				</text>
			</button>
			<button
				class={activeTab === 'upload' ? 'tab-btn tab-active' : 'tab-btn'}
				onclick={() => (activeTab = 'upload')}
			>
				<text class={activeTab === 'upload' ? 'tab-text-active' : 'tab-text'}>
					Upload & History ({stateData?.history?.length ?? 0})
				</text>
			</button>
		</div>

		{#if banner}
			<div class={bannerKind === 'err' ? 'notice notice-err' : bannerKind === 'ok' ? 'notice notice-ok' : 'notice notice-info'}>
				<text class="notice-text">{banner}</text>
			</div>
		{:else if bridgeError}
			<div class="notice notice-err">
				<text class="notice-text">{bridgeError}</text>
			</div>
		{/if}
	</div>

	<!-- Main Content -->
	<div class="body-wrap">
		<div class="scroll-area">
			{#if activeTab === 'control'}
				<div class="two-col">
					<!-- LEFT COLUMN: Motion, Homing & Z Babystep -->
					<div class="col">
						<div class="card">
							<div class="card-head">
								<text class="card-title">Toolhead Position & Homing</text>
								<div class="pill-group">
									{#each [0.1, 1, 10, 50] as step (step)}
										<button
											class={jogStep === step ? 'pill pill-on' : 'pill'}
											onclick={() => (jogStep = step)}
										>
											<text class={jogStep === step ? 'pill-text-on' : 'pill-text'}>{step}mm</text>
										</button>
									{/each}
								</div>
							</div>

							<div class="jog-layout">
								<!-- XY Cross Pad -->
								<div class="xy-pad">
									<div class="xy-row">
										<div class="xy-spacer"></div>
										<button class="jog-btn" disabled={!controlsEnabled} onclick={() => doJog('Y', 1)}>
											<text class="jog-btn-text">+Y</text>
										</button>
										<div class="xy-spacer"></div>
									</div>
									<div class="xy-row">
										<button class="jog-btn" disabled={!controlsEnabled} onclick={() => doJog('X', -1)}>
											<text class="jog-btn-text">-X</text>
										</button>
										<button
											class={armedAction === 'home' ? 'jog-home-armed' : 'jog-home'}
											disabled={!controlsEnabled}
											onclick={() => requireTwoTap('home', 'Confirm HOME?', doHome)}
										>
											<text class="jog-home-text">
												{armedAction === 'home' ? 'SURE?' : 'HOME'}
											</text>
										</button>
										<button class="jog-btn" disabled={!controlsEnabled} onclick={() => doJog('X', 1)}>
											<text class="jog-btn-text">+X</text>
										</button>
									</div>
									<div class="xy-row">
										<div class="xy-spacer"></div>
										<button class="jog-btn" disabled={!controlsEnabled} onclick={() => doJog('Y', -1)}>
											<text class="jog-btn-text">-Y</text>
										</button>
										<div class="xy-spacer"></div>
									</div>
								</div>

								<!-- Z Column -->
								<div class="z-col">
									<text class="mini-label">Z AXIS</text>
									<button class="jog-btn jog-z" disabled={!controlsEnabled} onclick={() => doJog('Z', 1)}>
										<text class="jog-btn-text">+Z Up</text>
									</button>
									<div class="z-readout">
										<text class="z-readout-text">
											{serialReady && serial ? `${serial.z.toFixed(1)}` : '—'}
										</text>
									</div>
									<button class="jog-btn jog-z" disabled={!controlsEnabled} onclick={() => doJog('Z', -1)}>
										<text class="jog-btn-text">-Z Down</text>
									</button>
								</div>
							</div>

							<div class="sub-row">
								<text class="mini-label">Jog Speed:</text>
								<div class="pill-group">
									<button
										class={jogSpeedMode === 'slow' ? 'pill pill-on' : 'pill'}
										onclick={() => (jogSpeedMode = 'slow')}
									>
										<text class={jogSpeedMode === 'slow' ? 'pill-text-on' : 'pill-text'}>Slow</text>
									</button>
									<button
										class={jogSpeedMode === 'normal' ? 'pill pill-on' : 'pill'}
										onclick={() => (jogSpeedMode = 'normal')}
									>
										<text class={jogSpeedMode === 'normal' ? 'pill-text-on' : 'pill-text'}>Normal</text>
									</button>
									<button
										class={jogSpeedMode === 'fast' ? 'pill pill-on' : 'pill'}
										onclick={() => (jogSpeedMode = 'fast')}
									>
										<text class={jogSpeedMode === 'fast' ? 'pill-text-on' : 'pill-text'}>Fast</text>
									</button>
								</div>
								<button
									class={armedAction === 'motors' ? 'btn-warn-armed' : 'btn-secondary'}
									disabled={!controlsEnabled}
									onclick={() => requireTwoTap('motors', 'Confirm Motors Off?', doMotorsOff)}
								>
									<text class="btn-text">
										{armedAction === 'motors' ? 'Confirm M84?' : 'Motors Off (M84)'}
									</text>
								</button>
							</div>
						</div>

						<!-- Z Babystepping & Fan -->
						<div class="card">
							<text class="card-title">Z Babystepping & Part Cooling Fan</text>
							<div class="sub-row">
								<text class="mini-label">Z Babystep:</text>
								<div class="pill-group">
									{#each [-0.05, -0.01, 0.01, 0.05] as d (d)}
										<button
											class="btn-secondary"
											disabled={!controlsEnabled}
											onclick={() => doBabystep(d)}
										>
											<text class="btn-text">{d > 0 ? `+${d.toFixed(2)}` : d.toFixed(2)}mm</text>
										</button>
									{/each}
								</div>
							</div>

							<div class="sub-row">
								<text class="mini-label">Part Fan:</text>
								<div class="pill-group">
									{#each [0, 25, 50, 75, 100] as pct (pct)}
										<button
											class={serial?.fan_percent === pct ? 'pill pill-on' : 'pill'}
											disabled={!controlsEnabled}
											onclick={() => doFan(pct)}
										>
											<text class={serial?.fan_percent === pct ? 'pill-text-on' : 'pill-text'}>
												{pct === 0 ? 'Off' : `${pct}%`}
											</text>
										</button>
									{/each}
								</div>
							</div>
						</div>
					</div>

					<!-- RIGHT COLUMN: Heaters & Extruder -->
					<div class="col">
						<div class="card">
							<div class="card-head">
								<text class="card-title">Temperature Control</text>
								<button class="btn-cool" disabled={!controlsEnabled} onclick={doCooldownAll}>
									<text class="btn-cool-text">Cooldown All</text>
								</button>
							</div>

							<!-- Nozzle -->
							<div class="heater-block">
								<div class="heater-row">
									<text class="heater-name">Nozzle (Hotend)</text>
									<text class="heater-live">
										{serialReady && serial
											? `${serial.nozzle_actual.toFixed(1)}°C → ${serial.nozzle_target.toFixed(0)}°C`
											: '—'}
									</text>
								</div>
								<div class="preset-row">
									{#each [0, 180, 210, 220, 240] as t (t)}
										<button
											class={t === 0 ? 'btn-off-sm' : 'btn-heat'}
											disabled={!controlsEnabled}
											onclick={() => {
												nozzleInput = String(t);
												void doSetTemp('nozzle', t);
											}}
										>
											<text class="btn-text">{t === 0 ? 'Off' : `${t}°C`}</text>
										</button>
									{/each}
									<div class="small-input-wrap">
										<input
											class="input"
											value={nozzleInput}
											onchange={(e) => (nozzleInput = e.value)}
										/>
									</div>
									<button
										class="btn-primary"
										disabled={!controlsEnabled}
										onclick={() => doSetTemp('nozzle', Number(nozzleInput) || 0)}
									>
										<text class="btn-primary-text">Set</text>
									</button>
								</div>
							</div>

							<!-- Bed -->
							<div class="heater-block">
								<div class="heater-row">
									<text class="heater-name">Heated Bed</text>
									<text class="heater-live">
										{serialReady && serial
											? `${serial.bed_actual.toFixed(1)}°C → ${serial.bed_target.toFixed(0)}°C`
											: '—'}
									</text>
								</div>
								<div class="preset-row">
									{#each [0, 50, 60, 75, 80] as t (t)}
										<button
											class={t === 0 ? 'btn-off-sm' : 'btn-bed'}
											disabled={!controlsEnabled}
											onclick={() => {
												bedInput = String(t);
												void doSetTemp('bed', t);
											}}
										>
											<text class="btn-text">{t === 0 ? 'Off' : `${t}°C`}</text>
										</button>
									{/each}
									<div class="small-input-wrap">
										<input
											class="input"
											value={bedInput}
											onchange={(e) => (bedInput = e.value)}
										/>
									</div>
									<button
										class="btn-primary"
										disabled={!controlsEnabled}
										onclick={() => doSetTemp('bed', Number(bedInput) || 0)}
									>
										<text class="btn-primary-text">Set</text>
									</button>
								</div>
							</div>
						</div>

						<!-- Filament / Extruder Control -->
						<div class="card">
							<div class="card-head">
								<text class="card-title">Filament / Extruder</text>
								<text class={hotendHot ? 'hint-ok' : 'hint-warn'}>
									{hotendHot
										? 'Nozzle hot — ready to extrude'
										: `Locked until nozzle ≥ ${COLD_EXTRUSION_MIN_C}°C`}
								</text>
							</div>

							<div class="sub-row">
								<text class="mini-label">Length:</text>
								<div class="pill-group">
									{#each [1, 5, 10, 25, 50] as len (len)}
										<button
											class={extrudeLen === len ? 'pill pill-on' : 'pill'}
											onclick={() => (extrudeLen = len)}
										>
											<text class={extrudeLen === len ? 'pill-text-on' : 'pill-text'}>{len}mm</text>
										</button>
									{/each}
								</div>
							</div>

							<div class="sub-row">
								<text class="mini-label">Speed (mm/min):</text>
								<div class="pill-group">
									{#each [60, 180, 300, 600] as spd (spd)}
										<button
											class={extrudeSpeed === spd ? 'pill pill-on' : 'pill'}
											onclick={() => {
												extrudeSpeed = spd;
												extrudeSpeedInput = String(spd);
											}}
										>
											<text class={extrudeSpeed === spd ? 'pill-text-on' : 'pill-text'}>{spd}</text>
										</button>
									{/each}
								</div>
								<div class="small-input-wrap">
									<input
										class="input"
										value={extrudeSpeedInput}
										onchange={(e) => {
											extrudeSpeedInput = e.value;
											const n = Number(e.value);
											if (Number.isFinite(n) && n > 0) extrudeSpeed = n;
										}}
									/>
								</div>
							</div>

							<div class="extrude-actions">
								<button
									class="btn-retract"
									disabled={!extrudeEnabled}
									onclick={() => doExtrude(-1)}
								>
									<text class="btn-primary-text">Retract -{extrudeLen} mm</text>
								</button>
								<button
									class="btn-extrude"
									disabled={!extrudeEnabled}
									onclick={() => doExtrude(1)}
								>
									<text class="btn-primary-text">Extrude +{extrudeLen} mm</text>
								</button>
							</div>
						</div>
					</div>
				</div>
			{:else if activeTab === 'filament'}
				<!-- Guided Filament Change Wizard -->
				<div class="col">
					<div class="card">
						<div class="card-head">
							<text class="card-title">Guided Filament Change Wizard (Bowden Delta)</text>
							<text class={hotendHot ? 'hint-ok' : 'hint-warn'}>
								{serialReady && serial
									? `Nozzle: ${serial.nozzle_actual.toFixed(1)}°C / ${serial.nozzle_target.toFixed(0)}°C`
									: 'No USB'}
							</text>
						</div>

						<div class="wizard-status">
							<text class="wizard-status-text">{wizardStepNote}</text>
						</div>

						<!-- Step 1: Material & Heat -->
						<div class="wizard-step">
							<text class="step-heading">Step 1 — Select Material & Heat Nozzle</text>
							<div class="sub-row">
								<div class="pill-group">
									<button
										class={wizardMaterial === 'PLA' ? 'btn-heat' : 'btn-secondary'}
										disabled={!controlsEnabled}
										onclick={() => wizardHeat('PLA', 220)}
									>
										<text class="btn-text">Heat PLA (220°C)</text>
									</button>
									<button
										class={wizardMaterial === 'PETG' ? 'btn-heat' : 'btn-secondary'}
										disabled={!controlsEnabled}
										onclick={() => wizardHeat('PETG', 240)}
									>
										<text class="btn-text">Heat PETG (240°C)</text>
									</button>
								</div>
								<div class="pill-group">
									<text class="mini-label">Bowden Length:</text>
									{#each [350, 450, 550] as len (len)}
										<button
											class={bowdenLen === len ? 'pill pill-on' : 'pill'}
											onclick={() => (bowdenLen = len)}
										>
											<text class={bowdenLen === len ? 'pill-text-on' : 'pill-text'}>{len}mm</text>
										</button>
									{/each}
								</div>
							</div>
						</div>

						<!-- Step 2: Unload Old Filament -->
						<div class="wizard-step">
							<text class="step-heading">Step 2 — Unload Old Filament (Tip-Shaped Clean Pull)</text>
							<text class="hint">
								Purges +5 mm slowly, yanks -20 mm fast out of the melt zone to prevent clogs, then retracts -{bowdenLen} mm through the Bowden tube.
							</text>
							<div class="extrude-actions">
								<button class="btn-retract" disabled={!extrudeEnabled} onclick={wizardUnload}>
									<text class="btn-primary-text">
										{busy ? 'Working...' : `Unload Old Filament (-${bowdenLen + 20} mm)`}
									</text>
								</button>
							</div>
						</div>

						<!-- Step 3: Insert New Filament -->
						<div class="wizard-step">
							<text class="step-heading">Step 3 — Insert New Spool</text>
							<text class="hint">
								1. Pull the old filament completely free.  2. Trim the new filament tip at a 45° angle.  3. Feed it through the filament sensor and into the extruder gears until it grips, then click Load below.
							</text>
						</div>

						<!-- Step 4: Load & Purge -->
						<div class="wizard-step">
							<text class="step-heading">Step 4 — Fast-Feed, Purge & Finish</text>
							<div class="extrude-actions">
								<button class="btn-extrude" disabled={!extrudeEnabled} onclick={wizardLoad}>
									<text class="btn-primary-text">
										{busy ? 'Working...' : `Load & Purge (+${bowdenLen + 10} mm)`}
									</text>
								</button>
								<button class="btn-primary" disabled={!extrudeEnabled} onclick={wizardPurgeMore}>
									<text class="btn-primary-text">Purge +15 mm More</text>
								</button>
								<button
									class="btn-cool"
									disabled={!controlsEnabled}
									onclick={() => doSetTemp('nozzle', 0)}
								>
									<text class="btn-cool-text">Done — Cooldown Nozzle</text>
								</button>
							</div>
						</div>
					</div>
				</div>
			{:else if activeTab === 'console'}
				<div class="card">
					<div class="card-head">
						<text class="card-title">Live Marlin G-Code Console</text>
						<div class="pill-group">
							<button class="btn-secondary" disabled={!controlsEnabled} onclick={() => doSendGCode('M105')}>
								<text class="btn-text">M105 Temps</text>
							</button>
							<button class="btn-secondary" disabled={!controlsEnabled} onclick={() => doSendGCode('M114')}>
								<text class="btn-text">M114 Pos</text>
							</button>
							<button class="btn-secondary" disabled={!controlsEnabled} onclick={() => doSendGCode('M115')}>
								<text class="btn-text">M115 Info</text>
							</button>
							<button class="btn-secondary" disabled={!controlsEnabled} onclick={() => doSendGCode('M503')}>
								<text class="btn-text">M503 EEPROM</text>
							</button>
							<button class="btn-secondary" disabled={busy || !serialReady} onclick={doClearConsole}>
								<text class="btn-text">Clear</text>
							</button>
						</div>
					</div>

					<div class="console-input-row">
						<div class="conn-input-wrap">
							<input
								class="input"
								value={gcodeInput}
								placeholder="Enter Marlin G-code (e.g. M105, M114, G28, M503)..."
								onchange={(e) => (gcodeInput = e.value)}
							/>
						</div>
						<button class="btn-primary" disabled={!controlsEnabled} onclick={() => doSendGCode()}>
							<text class="btn-primary-text">Send G-Code</text>
						</button>
					</div>

					<div class="console-box">
						{#if serial?.console && serial.console.length > 0}
							{#each [...serial.console].reverse() as line, idx (idx)}
								<div class="console-line">
									<text class="console-ts">{formatTime(line.timestamp)}</text>
									<text
										class={line.dir === 'tx'
											? 'console-tx'
											: line.dir === 'sys'
												? 'console-sys'
												: 'console-rx'}
									>
										{line.dir === 'tx' ? '>> ' : line.dir === 'sys' ? '** ' : '<< '}{line.text}
									</text>
								</div>
							{/each}
						{:else}
							<text class="hint">No serial console output yet.</text>
						{/if}
					</div>
				</div>
			{:else}
				<!-- Upload & History Tab -->
				<div class="col">
					<div class="card">
						<div class="card-head">
							<text class="card-title">Upload .gcode to IVI over Wi-Fi</text>
							<div class="pill-group">
								<button
									class={stateData?.auto_transform ? 'pill pill-on' : 'pill'}
									disabled={busy || !stateData}
									onclick={() => toggleSetting('auto_transform')}
								>
									<text class={stateData?.auto_transform ? 'pill-text-on' : 'pill-text'}>
										Auto-Transform: {stateData?.auto_transform ? 'ON' : 'OFF'}
									</text>
								</button>
								<button
									class={stateData?.unique_names ? 'pill pill-on' : 'pill'}
									disabled={busy || !stateData}
									onclick={() => toggleSetting('unique_names')}
								>
									<text class={stateData?.unique_names ? 'pill-text-on' : 'pill-text'}>
										Unique Names: {stateData?.unique_names ? 'ON' : 'OFF'}
									</text>
								</button>
							</div>
						</div>

						<div class="upload-row">
							<div class="upload-path-wrap">
								<input
									class="input"
									value={uploadPath}
									placeholder="/path/to/model.gcode"
									onchange={(e) => (uploadPath = e.value)}
								/>
							</div>
							<div class="upload-name-wrap">
								<input
									class="input"
									value={uploadName}
									placeholder="Optional remote name"
									onchange={(e) => (uploadName = e.value)}
								/>
							</div>
							<button class="btn-primary" disabled={uploading} onclick={handleUpload}>
								<text class="btn-primary-text">{uploading ? 'Uploading...' : 'Upload'}</text>
							</button>
						</div>
					</div>

					<div class="card">
						<div class="card-head">
							<text class="card-title">Upload History ({stateData?.history?.length ?? 0})</text>
							<button
								class="btn-secondary"
								disabled={busy || !stateData?.history?.length}
								onclick={handleClearHistory}
							>
								<text class="btn-text">Clear History</text>
							</button>
						</div>

						{#if stateData && stateData.history.length > 0}
							<div class="history-list">
								{#each stateData.history as rec (rec.timestamp + rec.sent_name)}
									<div class="history-item">
										<div class="history-top">
											<text class="history-name">{rec.sent_name}</text>
											<text class="history-time">{formatTime(rec.timestamp)}</text>
										</div>
										<text class="history-meta">
											{formatBytes(rec.size_bytes)} · {rec.meta.material || 'PLA'} · Nozzle {rec.meta.nozzle_temp || '?'}°C · Bed {rec.meta.bed_temp || '?'}°C · Z {rec.meta.min_z.toFixed(1)}..{rec.meta.max_z.toFixed(1)}mm · {rec.response}
										</text>
									</div>
								{/each}
							</div>
						{:else}
							<text class="hint">No uploads recorded yet.</text>
						{/if}
					</div>
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.root {
		display: flex;
		flex-direction: column;
		width: 100%;
		height: 100%;
		background-color: #0f172a;
		color: #e2e8f0;
		padding: 16px;
		gap: 12px;
	}

	.header {
		display: flex;
		flex-direction: column;
		gap: 10px;
		background-color: #1e293b;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 10px;
		padding: 12px 14px;
	}

	.brand-row {
		display: flex;
		flex-direction: row;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}

	.brand-col {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.title {
		font-size: 20px;
		font-weight: 700;
		color: #f8fafc;
	}

	.subtitle {
		font-size: 12px;
		color: #94a3b8;
	}

	.header-right {
		display: flex;
		flex-direction: row;
		align-items: center;
		gap: 8px;
	}

	.badge {
		padding: 5px 10px;
		border-radius: 999px;
		border-width: 1px;
		border-style: solid;
	}

	.badge-ok {
		background-color: #064e3b;
		border-color: #10b981;
	}

	.badge-print {
		background-color: #1e3a8a;
		border-color: #38bdf8;
	}

	.badge-warn {
		background-color: #451a03;
		border-color: #f59e0b;
	}

	.badge-off {
		background-color: #3f1d2e;
		border-color: #f43f5e;
	}

	.badge-text {
		font-size: 12px;
		font-weight: 600;
		color: #f8fafc;
	}

	.btn-estop {
		background-color: #991b1b;
		border-width: 1px;
		border-style: solid;
		border-color: #f87171;
		border-radius: 8px;
		padding: 6px 12px;
	}

	.btn-estop-armed {
		background-color: #dc2626;
		border-width: 2px;
		border-style: solid;
		border-color: #fef08a;
		border-radius: 8px;
		padding: 6px 14px;
	}

	.btn-estop-text {
		font-size: 12px;
		font-weight: 700;
		color: #ffffff;
	}

	.lock-banner {
		background-color: #1e3a8a;
		border-width: 1px;
		border-style: solid;
		border-color: #38bdf8;
		border-radius: 8px;
		padding: 8px 14px;
	}

	.lock-banner-text {
		font-size: 12px;
		font-weight: 700;
		color: #e0f2fe;
	}

	.conn-row {
		display: flex;
		flex-direction: row;
		align-items: center;
		gap: 8px;
	}

	.conn-label {
		font-size: 12px;
		color: #94a3b8;
		font-weight: 600;
	}

	.conn-input-wrap {
		flex-grow: 1;
		flex-basis: 0;
	}

	.telemetry-strip {
		display: flex;
		flex-direction: row;
		gap: 10px;
	}

	.tele-card {
		flex-grow: 1;
		flex-basis: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
		background-color: #1e293b;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 10px;
		padding: 10px 14px;
	}

	.tele-card-wide {
		flex-grow: 2;
		flex-basis: 0;
	}

	.tele-label {
		font-size: 10px;
		font-weight: 700;
		color: #94a3b8;
	}

	.tele-val-row {
		display: flex;
		flex-direction: row;
		align-items: baseline;
		gap: 6px;
	}

	.tele-val {
		font-size: 22px;
		font-weight: 700;
		color: #f8fafc;
	}

	.tele-hot {
		color: #fb7185;
	}

	.tele-warm {
		color: #fbbf24;
	}

	.tele-sub {
		font-size: 13px;
		color: #94a3b8;
	}

	.tele-val-mono {
		font-size: 18px;
		font-weight: 700;
		color: #38bdf8;
	}

	.tele-val-sm {
		font-size: 14px;
		font-weight: 600;
		color: #cbd5e1;
	}

	.tab-bar {
		display: flex;
		flex-direction: row;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}

	.tabs-left {
		display: flex;
		flex-direction: row;
		gap: 6px;
	}

	.tab-btn {
		background-color: #1e293b;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 8px;
		padding: 7px 14px;
	}

	.tab-active {
		background-color: #0284c7;
		border-color: #38bdf8;
	}

	.tab-text {
		font-size: 13px;
		font-weight: 600;
		color: #cbd5e1;
	}

	.tab-text-active {
		font-size: 13px;
		font-weight: 700;
		color: #ffffff;
	}

	.notice {
		padding: 6px 12px;
		border-radius: 8px;
		border-width: 1px;
		border-style: solid;
	}

	.notice-ok {
		background-color: #064e3b;
		border-color: #10b981;
	}

	.notice-err {
		background-color: #4c0519;
		border-color: #f43f5e;
	}

	.notice-info {
		background-color: #172554;
		border-color: #3b82f6;
	}

	.notice-text {
		font-size: 12px;
		color: #f8fafc;
	}

	.body-wrap {
		flex-grow: 1;
		flex-basis: 0;
		display: flex;
		flex-direction: column;
		min-height: 0;
	}

	.scroll-area {
		flex-grow: 1;
		flex-basis: 0;
		min-height: 0;
		overflow: scroll;
	}

	.two-col {
		display: flex;
		flex-direction: row;
		gap: 12px;
	}

	.col {
		flex-grow: 1;
		flex-basis: 0;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.card {
		display: flex;
		flex-direction: column;
		gap: 12px;
		background-color: #1e293b;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 10px;
		padding: 14px;
	}

	.card-head {
		display: flex;
		flex-direction: row;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}

	.card-title {
		font-size: 15px;
		font-weight: 700;
		color: #f8fafc;
	}

	.pill-group {
		display: flex;
		flex-direction: row;
		gap: 6px;
		align-items: center;
	}

	.pill {
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #475569;
		border-radius: 6px;
		padding: 5px 9px;
	}

	.pill-on {
		background-color: #0284c7;
		border-color: #38bdf8;
	}

	.pill-text {
		font-size: 12px;
		font-weight: 600;
		color: #cbd5e1;
	}

	.pill-text-on {
		font-size: 12px;
		font-weight: 700;
		color: #ffffff;
	}

	.jog-layout {
		display: flex;
		flex-direction: row;
		align-items: center;
		justify-content: space-around;
		gap: 16px;
		padding: 6px 0;
	}

	.xy-pad {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.xy-row {
		display: flex;
		flex-direction: row;
		gap: 8px;
		align-items: center;
	}

	.xy-spacer {
		width: 72px;
		height: 52px;
	}

	.jog-btn {
		width: 72px;
		height: 52px;
		display: flex;
		align-items: center;
		justify-content: center;
		background-color: #334155;
		border-width: 1px;
		border-style: solid;
		border-color: #64748b;
		border-radius: 10px;
	}

	.jog-z {
		width: 88px;
		height: 50px;
	}

	.jog-btn-text {
		font-size: 14px;
		font-weight: 700;
		color: #f8fafc;
	}

	.jog-home {
		width: 72px;
		height: 52px;
		display: flex;
		align-items: center;
		justify-content: center;
		background-color: #0284c7;
		border-width: 1px;
		border-style: solid;
		border-color: #38bdf8;
		border-radius: 10px;
	}

	.jog-home-armed {
		width: 72px;
		height: 52px;
		display: flex;
		align-items: center;
		justify-content: center;
		background-color: #d97706;
		border-width: 2px;
		border-style: solid;
		border-color: #fef08a;
		border-radius: 10px;
	}

	.jog-home-text {
		font-size: 13px;
		font-weight: 700;
		color: #ffffff;
	}

	.z-col {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
	}

	.z-readout {
		width: 88px;
		height: 36px;
		display: flex;
		align-items: center;
		justify-content: center;
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 8px;
	}

	.z-readout-text {
		font-size: 14px;
		font-weight: 700;
		color: #38bdf8;
	}

	.sub-row {
		display: flex;
		flex-direction: row;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}

	.mini-label {
		font-size: 12px;
		font-weight: 600;
		color: #94a3b8;
	}

	.heater-block {
		display: flex;
		flex-direction: column;
		gap: 8px;
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 8px;
		padding: 10px 12px;
	}

	.heater-row {
		display: flex;
		flex-direction: row;
		justify-content: space-between;
		align-items: center;
	}

	.heater-name {
		font-size: 13px;
		font-weight: 700;
		color: #e2e8f0;
	}

	.heater-live {
		font-size: 13px;
		font-weight: 700;
		color: #38bdf8;
	}

	.preset-row {
		display: flex;
		flex-direction: row;
		align-items: center;
		gap: 6px;
	}

	.small-input-wrap {
		width: 68px;
	}

	.input {
		width: 100%;
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #475569;
		border-radius: 6px;
		padding: 6px 10px;
		font-size: 13px;
		color: #f8fafc;
	}

	.btn-primary {
		background-color: #0284c7;
		border-width: 1px;
		border-style: solid;
		border-color: #38bdf8;
		border-radius: 6px;
		padding: 6px 12px;
	}

	.btn-primary-text {
		font-size: 13px;
		font-weight: 700;
		color: #ffffff;
	}

	.btn-secondary {
		background-color: #334155;
		border-width: 1px;
		border-style: solid;
		border-color: #475569;
		border-radius: 6px;
		padding: 6px 10px;
	}

	.btn-warn-armed {
		background-color: #b45309;
		border-width: 2px;
		border-style: solid;
		border-color: #fef08a;
		border-radius: 6px;
		padding: 6px 10px;
	}

	.btn-heat {
		background-color: #7f1d1d;
		border-width: 1px;
		border-style: solid;
		border-color: #ef4444;
		border-radius: 6px;
		padding: 6px 9px;
	}

	.btn-bed {
		background-color: #78350f;
		border-width: 1px;
		border-style: solid;
		border-color: #f59e0b;
		border-radius: 6px;
		padding: 6px 9px;
	}

	.btn-off-sm {
		background-color: #334155;
		border-width: 1px;
		border-style: solid;
		border-color: #64748b;
		border-radius: 6px;
		padding: 6px 9px;
	}

	.btn-cool {
		background-color: #0c4a6e;
		border-width: 1px;
		border-style: solid;
		border-color: #38bdf8;
		border-radius: 6px;
		padding: 5px 10px;
	}

	.btn-cool-text {
		font-size: 12px;
		font-weight: 700;
		color: #e0f2fe;
	}

	.btn-text {
		font-size: 12px;
		font-weight: 600;
		color: #f1f5f9;
	}

	.extrude-actions {
		display: flex;
		flex-direction: row;
		gap: 10px;
	}

	.btn-retract {
		flex-grow: 1;
		flex-basis: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		background-color: #b45309;
		border-width: 1px;
		border-style: solid;
		border-color: #f59e0b;
		border-radius: 8px;
		padding: 10px 12px;
	}

	.btn-extrude {
		flex-grow: 1;
		flex-basis: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		background-color: #047857;
		border-width: 1px;
		border-style: solid;
		border-color: #10b981;
		border-radius: 8px;
		padding: 10px 12px;
	}

	.hint {
		font-size: 12px;
		color: #94a3b8;
	}

	.hint-ok {
		font-size: 12px;
		font-weight: 600;
		color: #34d399;
	}

	.hint-warn {
		font-size: 12px;
		font-weight: 600;
		color: #fbbf24;
	}

	.wizard-status {
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #38bdf8;
		border-radius: 8px;
		padding: 10px 12px;
	}

	.wizard-status-text {
		font-size: 13px;
		font-weight: 600;
		color: #e0f2fe;
	}

	.wizard-step {
		display: flex;
		flex-direction: column;
		gap: 8px;
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 8px;
		padding: 12px;
	}

	.step-heading {
		font-size: 14px;
		font-weight: 700;
		color: #f8fafc;
	}

	.console-input-row {
		display: flex;
		flex-direction: row;
		gap: 8px;
		align-items: center;
	}

	.console-box {
		display: flex;
		flex-direction: column;
		gap: 4px;
		background-color: #020617;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 8px;
		padding: 10px 12px;
		min-height: 360px;
	}

	.console-line {
		display: flex;
		flex-direction: row;
		gap: 8px;
	}

	.console-ts {
		font-size: 11px;
		color: #64748b;
	}

	.console-tx {
		font-size: 12px;
		color: #38bdf8;
	}

	.console-rx {
		font-size: 12px;
		color: #86efac;
	}

	.console-sys {
		font-size: 12px;
		color: #fbbf24;
	}

	.upload-row {
		display: flex;
		flex-direction: row;
		gap: 8px;
		align-items: center;
	}

	.upload-path-wrap {
		flex-grow: 2;
		flex-basis: 0;
	}

	.upload-name-wrap {
		flex-grow: 1;
		flex-basis: 0;
	}

	.history-list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.history-item {
		display: flex;
		flex-direction: column;
		gap: 3px;
		background-color: #0f172a;
		border-width: 1px;
		border-style: solid;
		border-color: #334155;
		border-radius: 8px;
		padding: 10px 12px;
	}

	.history-top {
		display: flex;
		flex-direction: row;
		justify-content: space-between;
		align-items: center;
	}

	.history-name {
		font-size: 13px;
		font-weight: 700;
		color: #f8fafc;
	}

	.history-time {
		font-size: 11px;
		color: #94a3b8;
	}

	.history-meta {
		font-size: 12px;
		color: #94a3b8;
	}
</style>
