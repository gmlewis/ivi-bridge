import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { basename, dirname, join } from 'node:path';

export interface GCodeMeta {
	transformed: boolean;
	total_lines: number;
	valid_lines: number;
	nozzle_temp: string;
	bed_temp: string;
	material: string;
	min_x: number;
	max_x: number;
	min_y: number;
	max_y: number;
	min_z: number;
	max_z: number;
}

export interface UploadRecord {
	timestamp: string;
	original_name: string;
	sent_name: string;
	renamed: boolean;
	size_bytes: number;
	meta: GCodeMeta;
	response: string;
}

export interface PrinterStatus {
	reachable: boolean;
	raw_state: string;
	octo_state: string;
	error?: string;
}

export interface ConsoleLine {
	timestamp: string;
	dir: string;
	text: string;
}

export interface PrintJobStatus {
	active: boolean;
	source?: string; // "usb" | "touchscreen"
	state: string;
	file_name: string;
	total_lines: number;
	sent_lines: number;
	completion: number;
	current_z?: number;
	max_z?: number;
	started_at?: string;
	est_total_sec?: number;
	elapsed_sec: number;
	estimated_left_sec: number;
	est_finish_at?: string;
	last_error?: string;
}

export interface SerialState {
	enabled: boolean;
	connected: boolean;
	port_spec: string;
	port: string;
	baud: number;
	firmware?: string;
	working_status?: string;
	homed: boolean;
	at_home?: boolean;
	nozzle_actual: number;
	nozzle_target: number;
	nozzle_power: number;
	bed_actual: number;
	bed_target: number;
	bed_power: number;
	fan_percent: number;
	x: number;
	y: number;
	z: number;
	e: number;
	last_update?: string;
	last_error?: string;
	print_job?: PrintJobStatus | null;
	console: ConsoleLine[];
}

export interface BridgeState {
	printer_url: string;
	dry_run: boolean;
	auto_transform: boolean;
	unique_names: boolean;
	allow_start: boolean;
	status: PrinterStatus;
	serial?: SerialState;
	history: UploadRecord[];
}

export interface PanelConfig {
	bridgeUrl: string;
}

const DEFAULT_BRIDGE_URL = 'http://127.0.0.1:5000';

function configPath(): string {
	return join(homedir(), '.config', 'ivi-control-panel', 'config.json');
}

export function normalizeUrl(raw: string): string {
	let s = raw.trim().replace(/\/+$/, '');
	if (!s) return DEFAULT_BRIDGE_URL;
	if (!s.startsWith('http://') && !s.startsWith('https://')) {
		s = `http://${s}`;
	}
	return s;
}

export async function loadConfig(): Promise<PanelConfig> {
	const envUrl = process.env.IVI_BRIDGE_URL?.trim();
	if (envUrl) {
		return { bridgeUrl: normalizeUrl(envUrl) };
	}
	try {
		const raw = await readFile(configPath(), 'utf-8');
		const parsed = JSON.parse(raw) as Partial<PanelConfig>;
		if (parsed.bridgeUrl) {
			return { bridgeUrl: normalizeUrl(parsed.bridgeUrl) };
		}
	} catch {
		// Fall back to default localhost bridge URL.
	}
	return { bridgeUrl: DEFAULT_BRIDGE_URL };
}

export async function saveConfig(cfg: PanelConfig): Promise<void> {
	const path = configPath();
	await mkdir(dirname(path), { recursive: true });
	await writeFile(path, JSON.stringify({ bridgeUrl: normalizeUrl(cfg.bridgeUrl) }, null, 2) + '\n', 'utf-8');
}

async function postJson<T>(baseUrl: string, path: string, payload: unknown): Promise<T> {
	const resp = await fetch(`${normalizeUrl(baseUrl)}${path}`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload),
	});
	const data = (await resp.json().catch(() => ({}))) as T & { error?: string };
	if (!resp.ok) {
		throw new Error(data.error ?? `HTTP ${resp.status}`);
	}
	return data;
}

export async function fetchBridgeState(baseUrl: string): Promise<BridgeState> {
	const resp = await fetch(`${normalizeUrl(baseUrl)}/api/ivi/state`);
	if (!resp.ok) {
		throw new Error(`Bridge returned HTTP ${resp.status}`);
	}
	return (await resp.json()) as BridgeState;
}

export async function updateBridgeSettings(
	baseUrl: string,
	patch: { auto_transform?: boolean; unique_names?: boolean },
): Promise<{ auto_transform: boolean; unique_names: boolean }> {
	return postJson(baseUrl, '/api/ivi/settings', patch);
}

export async function clearHistory(baseUrl: string): Promise<void> {
	await postJson(baseUrl, '/api/ivi/history/clear', {});
}

export async function uploadGCodeFile(
	baseUrl: string,
	filePath: string,
	overrideName?: string,
): Promise<{ ok: boolean; record: UploadRecord }> {
	const trimmed = filePath.trim();
	if (!trimmed) {
		throw new Error('Enter a path to a .gcode file');
	}
	const bytes = await readFile(trimmed);
	const filename = overrideName?.trim() || basename(trimmed);
	const form = new FormData();
	form.append('file', new Blob([bytes], { type: 'application/octet-stream' }), filename);
	const resp = await fetch(`${normalizeUrl(baseUrl)}/api/ivi/upload`, {
		method: 'POST',
		body: form,
	});
	const data = (await resp.json().catch(() => ({}))) as {
		ok?: boolean;
		record?: UploadRecord;
		error?: string;
	};
	if (!resp.ok || !data.record) {
		throw new Error(data.error ?? `Upload failed (HTTP ${resp.status})`);
	}
	return { ok: true, record: data.record };
}

export interface SerialActionResp {
	ok: boolean;
	responses?: string[];
	serial?: SerialState;
}

export function serialJog(baseUrl: string, axis: 'X' | 'Y' | 'Z', distance: number, feedrate: number): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/jog', { axis, distance, feedrate });
}

export function serialHome(baseUrl: string): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/home', {});
}

export function serialSetTemp(baseUrl: string, heater: 'nozzle' | 'bed', target: number): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/temp', { heater, target });
}

export function serialExtrude(baseUrl: string, amount: number, speed: number): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/extrude', { amount, speed });
}

export function serialSetFan(baseUrl: string, percent: number): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/fan', { percent });
}

export function serialBabystep(baseUrl: string, delta: number): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/babystep', { delta });
}

export function serialMotorsOff(baseUrl: string): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/motors_off', {});
}

export function serialEStop(baseUrl: string): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/estop', {});
}

export function serialSendGCode(baseUrl: string, gcode: string): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/gcode', { gcode });
}

export function serialClearConsole(baseUrl: string): Promise<SerialActionResp> {
	return postJson(baseUrl, '/api/ivi/serial/console/clear', {});
}

export async function sendJobCommand(
	baseUrl: string,
	command: 'pause' | 'resume' | 'cancel'
): Promise<void> {
	const payload =
		command === 'resume' ? { command: 'pause', action: 'resume' } : { command };
	const res = await fetch(`${normalizeUrl(baseUrl)}/api/job`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload),
	});
	if (!res.ok && res.status !== 204) {
		const data = (await res.json().catch(() => ({}))) as { error?: string };
		throw new Error(data.error || `HTTP ${res.status}`);
	}
}

export function formatBytes(n: number): string {
	if (n < 1024) return `${n} B`;
	if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
	return `${(n / (1024 * 1024)).toFixed(2)} MB`;
}

export function formatTime(iso: string): string {
	try {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
	} catch {
		return iso;
	}
}
