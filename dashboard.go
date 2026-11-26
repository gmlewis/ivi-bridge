package main

// dashboardHTML is served at GET / and powers OrcaSlicer's embedded "Device"
// tab (as well as any browser pointed at http://<bridge-host>:5000/).
// It uses zero external CDN assets so it works 100% offline inside OrcaSlicer's
// WebView with full live USB-serial telemetry, print progress, Pause/Resume/
// Cancel, temperature & fan controls, motion/extruder/babystep, G-code console,
// and drag-and-drop Upload & Print.
const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>IVI 3D — Device Control</title>
<style>
  :root {
    --bg:#0f1216; --card:#171c22; --card2:#1d232b; --border:#2a323d;
    --text:#e6edf3; --muted:#8b949e; --accent:#2f81f7; --accent2:#1f6feb;
    --ok:#3fb950; --warn:#d29922; --err:#f85149; --purple:#a371f7;
  }
  * { box-sizing:border-box; }
  body { margin:0; padding:16px; background:var(--bg); color:var(--text); font:13px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; }
  .wrap { max-width:1240px; margin:0 auto; }
  .topbar { display:flex; flex-wrap:wrap; align-items:center; justify-content:space-between; gap:10px; margin-bottom:14px; padding-bottom:12px; border-bottom:1px solid var(--border); }
  .brand { display:flex; align-items:center; gap:10px; }
  .brand h1 { margin:0; font-size:18px; font-weight:700; letter-spacing:-.01em; }
  .brand .sub { font-size:12px; color:var(--muted); }
  .top-actions { display:flex; flex-wrap:wrap; align-items:center; gap:8px; }
  .pill { display:inline-flex; align-items:center; gap:5px; padding:3px 9px; border-radius:999px; font-size:11.5px; font-weight:600; background:#252c35; color:var(--text); }
  .pill.ok { background:rgba(63,185,80,.16); color:var(--ok); }
  .pill.err { background:rgba(248,81,73,.18); color:var(--err); }
  .pill.warn { background:rgba(210,153,34,.18); color:var(--warn); }
  .pill.info { background:rgba(47,129,247,.18); color:#79c0ff; }

  .btn { border:1px solid var(--border); background:var(--card2); color:var(--text); border-radius:7px; padding:6px 11px; font-size:12px; font-weight:600; cursor:pointer; transition:.12s; }
  .btn:hover:not(:disabled) { border-color:#445060; background:#242c36; }
  .btn:disabled { opacity:.42; cursor:not-allowed; }
  .btn.primary { background:var(--accent2); border-color:var(--accent); color:#fff; }
  .btn.primary:hover:not(:disabled) { background:var(--accent); }
  .btn.warn { background:rgba(210,153,34,.18); border-color:rgba(210,153,34,.45); color:#f0b849; }
  .btn.danger { background:rgba(248,81,73,.18); border-color:rgba(248,81,73,.5); color:#ff7b72; }
  .btn.danger.armed { background:var(--err); border-color:#ff7b72; color:#fff; }
  .btn.sm { padding:4px 8px; font-size:11.5px; }
  .btn.active { background:var(--accent2); border-color:var(--accent); color:#fff; }

  .grid { display:grid; grid-template-columns:repeat(12, 1fr); gap:12px; }
  .col-12 { grid-column:span 12; }
  .col-8 { grid-column:span 8; }
  .col-6 { grid-column:span 6; }
  .col-4 { grid-column:span 4; }
  @media (max-width: 920px) { .col-8, .col-6, .col-4 { grid-column:span 12; } }

  .card { background:var(--card); border:1px solid var(--border); border-radius:10px; padding:14px 16px; }
  .card-hd { display:flex; align-items:center; justify-content:space-between; margin-bottom:10px; }
  .label { font-size:11px; color:var(--muted); text-transform:uppercase; letter-spacing:.06em; font-weight:700; }

  .job-bar-wrap { width:100%; height:10px; background:#0d1117; border-radius:999px; overflow:hidden; margin:10px 0 8px; border:1px solid var(--border); }
  .job-bar { height:100%; width:0%; background:linear-gradient(90deg, var(--accent), var(--ok)); transition:width .35s ease; }
  .job-meta { display:flex; flex-wrap:wrap; gap:14px; font-size:12px; color:var(--muted); }
  .job-meta strong { color:var(--text); }
  .job-actions { display:flex; flex-wrap:wrap; gap:8px; margin-top:10px; }

  .temp-row { display:grid; grid-template-columns:92px 1fr auto; align-items:center; gap:10px; padding:8px 0; border-bottom:1px solid rgba(42,50,61,.6); }
  .temp-row:last-of-type { border-bottom:none; }
  .temp-val { font-size:18px; font-weight:700; font-variant-numeric:tabular-nums; }
  .temp-sub { font-size:11px; color:var(--muted); }
  .presets { display:flex; flex-wrap:wrap; align-items:center; gap:5px; }
  input[type=number], input[type=text] { background:#0d1117; border:1px solid var(--border); color:var(--text); border-radius:6px; padding:5px 8px; font-size:12px; }
  input[type=number] { width:64px; }
  canvas#tempChart { width:100%; height:105px; display:block; background:#0d1117; border:1px solid var(--border); border-radius:7px; margin-top:10px; }

  .coords { display:grid; grid-template-columns:repeat(4,1fr); gap:8px; margin-bottom:10px; }
  .coord-box { background:#0d1117; border:1px solid var(--border); border-radius:7px; padding:6px 8px; text-align:center; }
  .coord-box .k { font-size:10.5px; color:var(--muted); }
  .coord-box .v { font-size:14px; font-weight:700; font-variant-numeric:tabular-nums; }

  .jog-wrap { display:flex; flex-wrap:wrap; gap:14px; align-items:center; justify-content:space-between; }
  .jog-grid { display:grid; grid-template-columns:repeat(3, 44px); gap:5px; }
  .jog-z { display:grid; grid-template-columns:48px; gap:5px; }
  .jog-grid .btn, .jog-z .btn { padding:8px 0; text-align:center; }
  .step-row { display:flex; gap:5px; margin-bottom:8px; align-items:center; }
  .row { display:flex; flex-wrap:wrap; gap:6px; align-items:center; }

  .lock-banner { background:rgba(210,153,34,.14); border:1px solid rgba(210,153,34,.4); color:#f0b849; border-radius:7px; padding:6px 10px; font-size:12px; margin-bottom:10px; display:none; }

  .console { background:#0b0e12; border:1px solid var(--border); border-radius:7px; height:210px; overflow-y:auto; padding:8px 10px; font:11.5px/1.45 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
  .console .tx { color:#79c0ff; }
  .console .rx { color:#c9d1d9; }
  .console .sys { color:#d29922; }
  .console-in { display:flex; gap:6px; margin-top:8px; }
  .console-in input { flex:1; font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }

  .drop { border:2px dashed var(--border); border-radius:9px; padding:16px; text-align:center; cursor:pointer; transition:.15s; }
  .drop:hover, .drop.over { border-color:var(--accent); background:rgba(47,129,247,.07); }
  table { width:100%; border-collapse:collapse; font-size:12px; }
  th, td { text-align:left; padding:7px 8px; border-bottom:1px solid var(--border); }
  th { color:var(--muted); font-weight:600; }
  code { background:#0d1117; padding:2px 5px; border-radius:4px; font-size:11.5px; }
  #toast { position:fixed; bottom:16px; right:16px; background:#1f2630; border:1px solid var(--border); color:var(--text); padding:9px 14px; border-radius:8px; font-size:12.5px; box-shadow:0 8px 24px rgba(0,0,0,.45); display:none; z-index:99; }
</style>
</head>
<body>
<div class="wrap">
  <div class="topbar">
    <div class="brand">
      <div>
        <h1>IVI 3D Closed-Loop Delta</h1>
        <div class="sub" id="printerSub">Connecting to ivi-bridge...</div>
      </div>
    </div>
    <div class="top-actions">
      <span class="pill" id="wifiPill">Wi-Fi: ...</span>
      <span class="pill" id="usbPill">USB: ...</span>
      <span class="pill" id="statePill">State: ...</span>
      <button class="btn warn" id="topCancelBtn" style="display:none;" onclick="confirmAction(this, 'Stop & Home?', () => jobCmd('cancel'))" title="Cleanly stops print, turns off heaters, lifts Z, and homes — no power-cycle needed">Cancel Print</button>
      <button class="btn danger" id="estopBtn" onclick="confirmAction(this, 'KILL MCU?', sendEStop)" title="M112 emergency halt — permanently freezes Marlin until hard power-cycle! Use Cancel Print for normal aborts.">E-STOP (Kill)</button>
    </div>
  </div>

  <div class="grid">
    <!-- Active Print Job Banner -->
    <div class="card col-12" id="jobCard">
      <div class="card-hd">
        <div class="label">Print Job</div>
        <span class="pill" id="jobStatePill">Idle</span>
      </div>
      <div style="display:flex;flex-wrap:wrap;justify-content:space-between;align-items:baseline;gap:8px;">
        <div style="font-size:15px;font-weight:700;" id="jobFile">No active print</div>
        <div style="font-size:18px;font-weight:700;font-variant-numeric:tabular-nums;" id="jobPct">—</div>
      </div>
      <div class="job-bar-wrap"><div class="job-bar" id="jobBar"></div></div>
      <div class="job-meta">
        <span>Lines: <strong id="jobLines">—</strong></span>
        <span>Elapsed: <strong id="jobElapsed">—</strong></span>
        <span>ETA: <strong id="jobEta">—</strong></span>
        <span id="jobErr" style="color:var(--err);"></span>
      </div>
      <div class="job-actions" id="jobActions" style="display:none;">
        <button class="btn warn" id="pauseBtn" onclick="jobCmd('pause')">Pause Print</button>
        <button class="btn primary" id="resumeBtn" style="display:none;" onclick="jobCmd('resume')">Resume Print</button>
        <button class="btn danger" id="cancelBtn" onclick="confirmAction(this, 'Cancel Print?', () => jobCmd('cancel'))">Cancel Print</button>
      </div>
    </div>

    <!-- Temperatures & Fan -->
    <div class="card col-6">
      <div class="card-hd">
        <div class="label">Temperatures &amp; Cooling</div>
        <span class="temp-sub" id="tempUpdated">—</span>
      </div>
      <div class="temp-row">
        <div>
          <div class="label">Nozzle</div>
          <div class="temp-val" style="color:#ff7b72;" id="nozTemp">--.-°</div>
          <div class="temp-sub">Target: <span id="nozTgt">0</span>° · <span id="nozPow">0</span>%</div>
        </div>
        <div class="presets">
          <button class="btn sm" onclick="setTemp('nozzle',0)">Off</button>
          <button class="btn sm" onclick="setTemp('nozzle',200)">200°</button>
          <button class="btn sm" onclick="setTemp('nozzle',220)">220° PLA</button>
          <button class="btn sm" onclick="setTemp('nozzle',240)">240° PETG</button>
        </div>
        <div class="row">
          <input type="number" id="nozIn" min="0" max="270" step="5" value="220">
          <button class="btn sm primary" onclick="setTemp('nozzle', +document.getElementById('nozIn').value)">Set</button>
        </div>
      </div>
      <div class="temp-row">
        <div>
          <div class="label">Bed</div>
          <div class="temp-val" style="color:#79c0ff;" id="bedTemp">--.-°</div>
          <div class="temp-sub">Target: <span id="bedTgt">0</span>° · <span id="bedPow">0</span>%</div>
        </div>
        <div class="presets">
          <button class="btn sm" onclick="setTemp('bed',0)">Off</button>
          <button class="btn sm" onclick="setTemp('bed',60)">60° PLA</button>
          <button class="btn sm" onclick="setTemp('bed',80)">80° PETG</button>
        </div>
        <div class="row">
          <input type="number" id="bedIn" min="0" max="110" step="5" value="60">
          <button class="btn sm primary" onclick="setTemp('bed', +document.getElementById('bedIn').value)">Set</button>
        </div>
      </div>
      <div class="temp-row">
        <div>
          <div class="label">Part Fan</div>
          <div class="temp-val" style="color:var(--ok);font-size:15px;" id="fanPct">0%</div>
        </div>
        <div class="presets">
          <button class="btn sm" onclick="setFan(0)">Off</button>
          <button class="btn sm" onclick="setFan(50)">50%</button>
          <button class="btn sm" onclick="setFan(100)">100%</button>
        </div>
        <div></div>
      </div>
      <canvas id="tempChart" width="520" height="105"></canvas>
    </div>

    <!-- Motion, Extruder & Babystep -->
    <div class="card col-6">
      <div class="card-hd">
        <div class="label">Motion, Extruder &amp; Z-Babystep</div>
        <span class="pill" id="homedPill">Homed: —</span>
      </div>
      <div class="lock-banner" id="lockBanner">Manual motion/extruder controls are locked while printing. Use Pause/Cancel or E-STOP above.</div>
      <div class="coords">
        <div class="coord-box"><div class="k">X</div><div class="v" id="posX">0.00</div></div>
        <div class="coord-box"><div class="k">Y</div><div class="v" id="posY">0.00</div></div>
        <div class="coord-box"><div class="k">Z</div><div class="v" id="posZ">0.00</div></div>
        <div class="coord-box"><div class="k">E</div><div class="v" id="posE">0.00</div></div>
      </div>
      <div class="step-row">
        <span class="label" style="margin-right:4px;">Step (mm):</span>
        <button class="btn sm step-btn" onclick="setStep(0.1,this)">0.1</button>
        <button class="btn sm step-btn" onclick="setStep(1,this)">1</button>
        <button class="btn sm step-btn active" onclick="setStep(10,this)">10</button>
        <button class="btn sm step-btn" onclick="setStep(50,this)">50</button>
      </div>
      <div class="jog-wrap">
        <div class="jog-grid">
          <div></div>
          <button class="btn ctrl" onclick="jog('Y', step)">+Y</button>
          <div></div>
          <button class="btn ctrl" onclick="jog('X', -step)">-X</button>
          <button class="btn primary ctrl" onclick="confirmAction(this,'Home?',sendHome)">HOME</button>
          <button class="btn ctrl" onclick="jog('X', step)">+X</button>
          <div></div>
          <button class="btn ctrl" onclick="jog('Y', -step)">-Y</button>
          <div></div>
        </div>
        <div class="jog-z">
          <button class="btn ctrl" onclick="jog('Z', step)">+Z</button>
          <button class="btn sm warn ctrl" onclick="confirmAction(this,'Off?',motorsOff)">M84</button>
          <button class="btn ctrl" onclick="jog('Z', -step)">-Z</button>
        </div>
        <div style="flex:1;min-width:170px;">
          <div class="label" style="margin-bottom:5px;">Extruder (≥175°C)</div>
          <div class="row" style="margin-bottom:8px;">
            <input type="number" id="extAmt" value="10" min="1" max="100" step="5">
            <button class="btn sm ctrl" onclick="extrude(-Math.abs(+document.getElementById('extAmt').value))">Retract</button>
            <button class="btn sm primary ctrl" onclick="extrude(Math.abs(+document.getElementById('extAmt').value))">Extrude</button>
          </div>
          <div class="label" style="margin-bottom:5px;">Z-Babystep (M290)</div>
          <div class="row">
            <button class="btn sm ctrl" onclick="babystep(-0.05)">-0.05</button>
            <button class="btn sm ctrl" onclick="babystep(-0.02)">-0.02</button>
            <button class="btn sm ctrl" onclick="babystep(0.02)">+0.02</button>
            <button class="btn sm ctrl" onclick="babystep(0.05)">+0.05</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Marlin Console -->
    <div class="card col-6">
      <div class="card-hd">
        <div class="label">Marlin USB Serial Console</div>
        <button class="btn sm" onclick="clearConsole()">Clear</button>
      </div>
      <div class="console" id="consoleBox"></div>
      <div class="console-in">
        <input type="text" id="gcodeIn" placeholder="Send G-code (e.g. M105, M114, G28)..." onkeydown="if(event.key==='Enter')sendManualGCode()">
        <button class="btn primary ctrl" onclick="sendManualGCode()">Send</button>
      </div>
    </div>

    <!-- Upload & Print + History -->
    <div class="card col-6">
      <div class="card-hd">
        <div class="label">G-Code Upload &amp; Print</div>
        <label style="font-size:12px;color:var(--muted);display:flex;align-items:center;gap:5px;cursor:pointer;">
          <input type="checkbox" id="startAfterUpload" checked> Start USB print immediately
        </label>
      </div>
      <div class="drop" id="drop">
        Drop a <code>.gcode</code> file here or click to choose
        <input type="file" id="fileInput" accept=".gcode,.gco,.g" style="display:none">
      </div>
      <div id="uploadMsg" style="margin-top:8px;font-size:12px;"></div>
      <div class="label" style="margin:12px 0 6px;">Recent Uploads</div>
      <div style="max-height:175px;overflow-y:auto;">
        <table>
          <thead><tr><th>Time</th><th>File</th><th>Temps</th><th>Lines</th><th>Size</th></tr></thead>
          <tbody id="histBody"><tr><td colspan="5" style="color:var(--muted)">Loading...</td></tr></tbody>
        </table>
      </div>
    </div>
  </div>
</div>
<div id="toast"></div>

<script>
let step = 10;
let lastNozzleActual = 0;
let tempHistory = [];
const MAX_PTS = 90;

function toast(msg, isErr) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.style.borderColor = isErr ? 'var(--err)' : 'var(--accent)';
  t.style.display = 'block';
  clearTimeout(t._tid);
  t._tid = setTimeout(() => { t.style.display = 'none'; }, 3200);
}

function confirmAction(btn, promptText, fn) {
  if (btn.dataset.armed === '1') {
    clearTimeout(btn._armTid);
    btn.dataset.armed = '0';
    btn.classList.remove('armed');
    btn.textContent = btn.dataset.origLabel;
    fn();
    return;
  }
  btn.dataset.origLabel = btn.textContent;
  btn.dataset.armed = '1';
  btn.classList.add('armed');
  btn.textContent = promptText;
  btn._armTid = setTimeout(() => {
    btn.dataset.armed = '0';
    btn.classList.remove('armed');
    btn.textContent = btn.dataset.origLabel;
  }, 3500);
}

function setStep(v, el) {
  step = v;
  document.querySelectorAll('.step-btn').forEach(b => b.classList.remove('active'));
  el.classList.add('active');
}

function fmtDur(sec) {
  if (!sec || sec < 0) return '0s';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  return h > 0 ? h + 'h ' + m + 'm' : (m > 0 ? m + 'm ' + s + 's' : s + 's');
}

async function postJSON(url, body) {
  try {
    const r = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {})
    });
    if (r.status === 204) { refresh(); return { ok: true }; }
    const d = await r.json().catch(() => ({}));
    if (!r.ok) {
      toast(d.error || ('HTTP ' + r.status), true);
      return null;
    }
    refresh();
    return d;
  } catch (e) {
    toast(e.message, true);
    return null;
  }
}

function setTemp(heater, target) {
  const body = heater === 'nozzle' ? { nozzle: target } : { bed: target };
  postJSON('/api/ivi/serial/temp', body).then(d => d && toast(heater + ' target -> ' + target + '°C'));
}
function setFan(pct) { postJSON('/api/ivi/serial/fan', { percent: pct }).then(d => d && toast('Fan -> ' + pct + '%')); }
function jog(axis, delta) {
  const b = {}; b[axis.toLowerCase()] = delta;
  postJSON('/api/ivi/serial/jog', b);
}
function sendHome() { postJSON('/api/ivi/serial/home', {}).then(d => d && toast('Homing (G28)...')); }
function motorsOff() { postJSON('/api/ivi/serial/motors_off', {}).then(d => d && toast('Motors disabled (M84)')); }
function sendEStop() { postJSON('/api/ivi/serial/estop', {}).then(d => d && toast('E-STOP (M112) sent!', true)); }
function babystep(mm) { postJSON('/api/ivi/serial/babystep', { z: mm }).then(d => d && toast('Z babystep ' + (mm>0?'+':'') + mm + ' mm')); }
function extrude(mm) {
  if (lastNozzleActual < 175) {
    toast('Cold extrusion blocked: nozzle is ' + lastNozzleActual.toFixed(1) + '°C (need ≥175°C)', true);
    return;
  }
  postJSON('/api/ivi/serial/extrude', { amount: mm, speed: 300 }).then(d => d && toast((mm>0?'Extrude ':'Retract ') + Math.abs(mm) + ' mm'));
}
function clearConsole() { postJSON('/api/ivi/serial/console/clear', {}); }
function sendManualGCode() {
  const inp = document.getElementById('gcodeIn');
  const gc = inp.value.trim();
  if (!gc) return;
  inp.value = '';
  postJSON('/api/ivi/serial/gcode', { gcode: gc });
}
function jobCmd(cmd) {
  const payload = cmd === 'resume' ? { command: 'pause', action: 'resume' } : { command: cmd };
  postJSON('/api/job', payload).then(d => d && toast('Print ' + cmd + ' sent'));
}

function drawTempChart() {
  const cv = document.getElementById('tempChart');
  const ctx = cv.getContext('2d');
  const W = cv.width, H = cv.height;
  ctx.clearRect(0, 0, W, H);
  // Horizontal grid lines at 50, 100, 150, 200, 250C
  ctx.strokeStyle = '#1f2630';
  ctx.lineWidth = 1;
  ctx.font = '10px sans-serif';
  ctx.fillStyle = '#6e7681';
  for (const t of [50, 100, 150, 200, 250]) {
    const y = H - (t / 270) * (H - 12) - 6;
    ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(W, y); ctx.stroke();
    ctx.fillText(t + '°', 4, y - 2);
  }
  if (tempHistory.length < 2) return;
  const plot = (key, color, dashed) => {
    ctx.save();
    ctx.strokeStyle = color;
    ctx.lineWidth = dashed ? 1 : 2;
    if (dashed) ctx.setLineDash([4, 4]);
    ctx.beginPath();
    tempHistory.forEach((p, idx) => {
      const x = (idx / (MAX_PTS - 1)) * (W - 32) + 28;
      const y = H - (Math.min(270, Math.max(0, p[key])) / 270) * (H - 12) - 6;
      if (idx === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    });
    ctx.stroke();
    ctx.restore();
  };
  plot('nt', 'rgba(255,123,114,.45)', true);
  plot('bt', 'rgba(121,192,255,.45)', true);
  plot('na', '#ff7b72', false);
  plot('ba', '#79c0ff', false);
}

async function refresh() {
  try {
    const r = await fetch('/api/ivi/state');
    const d = await r.json();
    const st = d.status || {};
    const ser = d.serial || {};
    const pj = ser.print_job || null;

    document.getElementById('printerSub').textContent =
      (d.printer_url || '') + (ser.port ? ' · USB ' + ser.port.split('/').pop() : '');

    const wp = document.getElementById('wifiPill');
    wp.className = 'pill ' + (st.reachable ? 'ok' : 'err');
    wp.textContent = st.reachable ? 'Wi-Fi: Online' : 'Wi-Fi: Offline';

    const up = document.getElementById('usbPill');
    up.className = 'pill ' + (ser.connected ? 'ok' : 'err');
    up.textContent = ser.connected ? 'USB: Connected' : 'USB: Disconnected';

    const usbPrinting = !!(pj && pj.active);
    const screenBusy = !!(ser.connected && ser.working_status && ser.working_status !== 'IDLE');
    const isBusy = usbPrinting || screenBusy;

    const sp = document.getElementById('statePill');
    if (usbPrinting) {
      sp.className = 'pill ' + (pj.state === 'Paused' ? 'warn' : 'info');
      sp.textContent = 'USB ' + pj.state;
    } else if (screenBusy) {
      sp.className = 'pill warn';
      sp.textContent = 'Touchscreen: ' + ser.working_status;
    } else {
      sp.className = 'pill ok';
      sp.textContent = 'Operational (IDLE)';
    }

    // Lock manual controls while printing
    document.getElementById('lockBanner').style.display = isBusy ? 'block' : 'none';
    document.querySelectorAll('.ctrl').forEach(b => { b.disabled = isBusy || !ser.connected; });

    // Print Job Card
    const jp = document.getElementById('jobStatePill');
    const ja = document.getElementById('jobActions');
    if (pj) {
      jp.textContent = pj.state;
      jp.className = 'pill ' + (pj.state === 'Printing' ? 'info' : pj.state === 'Completed' ? 'ok' : pj.state === 'Error' ? 'err' : 'warn');
      document.getElementById('jobFile').textContent = pj.file_name || '—';
      document.getElementById('jobPct').textContent = (pj.completion || 0).toFixed(1) + '%';
      document.getElementById('jobBar').style.width = Math.min(100, Math.max(0, pj.completion || 0)) + '%';
      document.getElementById('jobLines').textContent = (pj.sent_lines || 0) + ' / ' + (pj.total_lines || 0);
      document.getElementById('jobElapsed').textContent = fmtDur(pj.elapsed_sec || 0);
      document.getElementById('jobEta').textContent = pj.active ? fmtDur(pj.estimated_left_sec || 0) : '—';
      document.getElementById('jobErr').textContent = pj.last_error || '';
      ja.style.display = pj.active ? 'flex' : 'none';
      document.getElementById('topCancelBtn').style.display = pj.active ? 'inline-block' : 'none';
      document.getElementById('pauseBtn').style.display = pj.state === 'Paused' ? 'none' : 'inline-block';
      document.getElementById('resumeBtn').style.display = pj.state === 'Paused' ? 'inline-block' : 'none';
    } else {
      jp.textContent = screenBusy ? ser.working_status : 'Idle';
      jp.className = 'pill ' + (screenBusy ? 'warn' : '');
      document.getElementById('jobFile').textContent = screenBusy ? 'Printing from IVI touchscreen' : 'Ready — click Upload & Print in OrcaSlicer or drop a .gcode below';
      document.getElementById('jobPct').textContent = '—';
      document.getElementById('jobBar').style.width = '0%';
      document.getElementById('jobLines').textContent = '—';
      document.getElementById('jobElapsed').textContent = '—';
      document.getElementById('jobEta').textContent = '—';
      document.getElementById('jobErr').textContent = '';
      ja.style.display = 'none';
      document.getElementById('topCancelBtn').style.display = 'none';
    }

    // Temperatures & Coords
    if (ser.connected) {
      lastNozzleActual = ser.nozzle_actual || 0;
      document.getElementById('nozTemp').textContent = lastNozzleActual.toFixed(1) + '°C';
      document.getElementById('nozTgt').textContent = Math.round(ser.nozzle_target || 0);
      document.getElementById('nozPow').textContent = ser.nozzle_power || 0;
      document.getElementById('bedTemp').textContent = (ser.bed_actual || 0).toFixed(1) + '°C';
      document.getElementById('bedTgt').textContent = Math.round(ser.bed_target || 0);
      document.getElementById('bedPow').textContent = ser.bed_power || 0;
      document.getElementById('fanPct').textContent = (ser.fan_percent || 0) + '%';
      document.getElementById('tempUpdated').textContent = ser.last_update ? 'Updated ' + new Date(ser.last_update).toLocaleTimeString() : '';

      document.getElementById('posX').textContent = (ser.x || 0).toFixed(2);
      document.getElementById('posY').textContent = (ser.y || 0).toFixed(2);
      document.getElementById('posZ').textContent = (ser.z || 0).toFixed(2);
      document.getElementById('posE').textContent = (ser.e || 0).toFixed(2);
      const hp = document.getElementById('homedPill');
      if (ser.at_home) {
        hp.className = 'pill ok';
        hp.textContent = 'At Home';
      } else if (ser.homed) {
        hp.className = 'pill info';
        hp.textContent = 'XYZ Referenced';
      } else {
        hp.className = 'pill warn';
        hp.textContent = 'Not Homed';
      }

      tempHistory.push({ na: lastNozzleActual, nt: ser.nozzle_target || 0, ba: ser.bed_actual || 0, bt: ser.bed_target || 0 });
      if (tempHistory.length > MAX_PTS) tempHistory.shift();
      drawTempChart();
    }

    // Console
    if (ser.console) {
      const box = document.getElementById('consoleBox');
      const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 28;
      box.innerHTML = ser.console.slice(-120).map(c => {
        const cls = c.dir === 'tx' ? 'tx' : (c.dir === 'sys' ? 'sys' : 'rx');
        const pfx = c.dir === 'tx' ? '&gt; ' : (c.dir === 'sys' ? '* ' : '&lt; ');
        const esc = String(c.text || '').replace(/&/g,'&amp;').replace(/</g,'&lt;');
        return '<div class="' + cls + '">' + pfx + esc + '</div>';
      }).join('');
      if (atBottom) box.scrollTop = box.scrollHeight;
    }

    // History
    const tb = document.getElementById('histBody');
    if (!d.history || d.history.length === 0) {
      tb.innerHTML = '<tr><td colspan="5" style="color:var(--muted)">No uploads yet.</td></tr>';
    } else {
      tb.innerHTML = d.history.slice(0, 15).map(h => {
        const t = new Date(h.timestamp).toLocaleTimeString();
        const temps = (h.meta.nozzle_temp || '-') + '°/' + (h.meta.bed_temp || '-') + '°';
        const kb = (h.size_bytes / 1024).toFixed(0) + ' KB';
        const warn = (h.meta.warnings && h.meta.warnings.length) ? ' <span class="pill warn" title="' + h.meta.warnings.join('; ') + '">!</span>' : '';
        return '<tr><td>' + t + '</td><td><code>' + h.sent_name + '</code>' + warn + '</td><td>' + temps + '</td><td>' + (h.meta.total_lines || '-') + '</td><td>' + kb + '</td></tr>';
      }).join('');
    }
  } catch (e) {
    document.getElementById('statePill').className = 'pill err';
    document.getElementById('statePill').textContent = 'Bridge Offline';
  }
}

async function uploadFile(file) {
  const msg = document.getElementById('uploadMsg');
  const startNow = document.getElementById('startAfterUpload').checked;
  msg.textContent = (startNow ? 'Uploading & starting USB print: ' : 'Uploading: ') + file.name + '...';
  const fd = new FormData();
  fd.append('file', file, file.name);
  fd.append('print', startNow ? 'true' : 'false');
  try {
    const r = await fetch('/api/files/local', { method: 'POST', body: fd });
    const d = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(d.detail || d.error || ('HTTP ' + r.status));
    msg.innerHTML = startNow
      ? '<span class="pill info">Printing</span> <code>' + (d.files && d.files.local ? d.files.local.name : file.name) + '</code> started over USB!'
      : '<span class="pill ok">Uploaded</span> <code>' + (d.files && d.files.local ? d.files.local.name : file.name) + '</code>';
    refresh();
  } catch (e) {
    msg.innerHTML = '<span class="pill err">Error</span> ' + e.message;
  }
}

const drop = document.getElementById('drop');
const fi = document.getElementById('fileInput');
drop.onclick = () => fi.click();
fi.onchange = () => { if (fi.files[0]) uploadFile(fi.files[0]); };
drop.ondragover = e => { e.preventDefault(); drop.classList.add('over'); };
drop.ondragleave = () => drop.classList.remove('over');
drop.ondrop = e => { e.preventDefault(); drop.classList.remove('over'); if (e.dataTransfer.files[0]) uploadFile(e.dataTransfer.files[0]); };

refresh();
setInterval(refresh, 1500);
</script>
</body>
</html>
`
