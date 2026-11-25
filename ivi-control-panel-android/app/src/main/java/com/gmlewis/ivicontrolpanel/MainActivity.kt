package com.gmlewis.ivicontrolpanel

import android.content.Context
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableDoubleStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.util.Locale
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.max
import kotlin.math.min

private const val COLD_EXTRUSION_MIN_C = 175.0

private val BgDark = Color(0xFF0F172A)
private val CardDark = Color(0xFF1E293B)
private val WellDark = Color(0xFF020617)
private val BorderSlate = Color(0xFF334155)
private val AccentCyan = Color(0xFF0284C7)
private val AccentCyanLight = Color(0xFF38BDF8)
private val AccentRed = Color(0xFFDC2626)
private val AccentRedDim = Color(0xFF991B1B)
private val AccentGreen = Color(0xFF059669)
private val AccentAmber = Color(0xFFD97706)
private val TextPrimary = Color(0xFFF8FAFC)
private val TextMuted = Color(0xFF94A3B8)

private val AppColors = darkColorScheme(
    primary = AccentCyan,
    secondary = AccentCyanLight,
    background = BgDark,
    surface = CardDark,
    onPrimary = Color.White,
    onBackground = TextPrimary,
    onSurface = TextPrimary
)

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme(colorScheme = AppColors) {
                Surface(modifier = Modifier.fillMaxSize(), color = BgDark) {
                    IviControlPanelApp()
                }
            }
        }
    }
}

@Composable
private fun IviControlPanelApp() {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("ivi_control_panel", Context.MODE_PRIVATE) }
    val scope = rememberCoroutineScope()

    var bridgeUrl by remember {
        mutableStateOf(prefs.getString("bridge_url", BridgeApi.DEFAULT_URL) ?: BridgeApi.DEFAULT_URL)
    }
    var urlDraft by remember { mutableStateOf(bridgeUrl) }
    var activeTab by remember { mutableIntStateOf(0) } // 0=Control, 1=Filament, 2=Console, 3=History
    var state by remember { mutableStateOf<BridgeState?>(null) }
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf("") }
    var noticeError by remember { mutableStateOf(false) }

    // Two-tap safety confirmation state (auto-expires after 4s)
    var armedAction by remember { mutableStateOf<String?>(null) }
    var armJob by remember { mutableStateOf<Job?>(null) }

    var jogStep by remember { mutableDoubleStateOf(10.0) }
    var speedMode by remember { mutableIntStateOf(1) } // 0=Slow, 1=Normal, 2=Fast
    var nozzleDraft by remember { mutableStateOf("220") }
    var bedDraft by remember { mutableStateOf("60") }
    var extrudeLen by remember { mutableDoubleStateOf(10.0) }
    var extrudeSpeed by remember { mutableDoubleStateOf(180.0) }
    var extrudeSpeedDraft by remember { mutableStateOf("180") }
    var gcodeDraft by remember { mutableStateOf("") }

    // Filament Change Wizard state
    var wizardMaterial by remember { mutableStateOf("PLA") }
    var bowdenLen by remember { mutableIntStateOf(450) }
    var wizardNote by remember {
        mutableStateOf("Step 1: Pick PLA or PETG and heat the nozzle before unloading or loading.")
    }

    val serial = state?.serial
    val serialReady = serial?.enabled == true && serial.connected
    val wifiOnline = state?.status?.reachable == true
    val isPrinting = serialReady && !serial?.workingStatus.isNullOrBlank() && serial?.workingStatus != "IDLE"
    val controlsEnabled = serialReady && !busy && !isPrinting
    val hotendHot = serialReady && (serial?.nozzleActual ?: 0.0) >= COLD_EXTRUSION_MIN_C
    val extrudeEnabled = controlsEnabled && hotendHot

    fun setMsg(msg: String, isErr: Boolean = false) {
        notice = msg
        noticeError = isErr
    }

    fun requireTwoTap(key: String, label: String, onConfirmed: () -> Unit) {
        if (armedAction == key) {
            armJob?.cancel()
            armJob = null
            armedAction = null
            onConfirmed()
            return
        }
        armJob?.cancel()
        armedAction = key
        setMsg("Tap \"$label\" again within 4s to confirm.")
        armJob = scope.launch {
            delay(4000)
            armedAction = null
            armJob = null
        }
    }

    suspend fun pollOnce(showError: Boolean = false) {
        try {
            state = BridgeApi.fetchState(bridgeUrl)
            if (showError) setMsg("Connected to $bridgeUrl")
        } catch (e: Exception) {
            if (showError) setMsg("Bridge unreachable: ${e.message}", true)
        }
    }

    fun runAction(label: String, allowWhilePrinting: Boolean = false, block: suspend () -> Unit) {
        if (isPrinting && !allowWhilePrinting) {
            setMsg("Controls locked while printing (${serial?.workingStatus}).", true)
            return
        }
        scope.launch {
            busy = true
            try {
                block()
                setMsg(label)
                pollOnce(false)
            } catch (e: Exception) {
                setMsg(e.message ?: "Request failed", true)
            } finally {
                busy = false
            }
        }
    }

    suspend fun extrudeChunked(totalMm: Double, speed: Double, label: String) {
        val sign = if (totalMm >= 0) 1.0 else -1.0
        var remaining = abs(totalMm)
        val chunk = 150.0
        val totalChunks = max(1, ceil(remaining / chunk).toInt())
        var idx = 0
        while (remaining > 0.01) {
            idx++
            val step = min(chunk, remaining) * sign
            wizardNote = "$label ($idx/$totalChunks)..."
            BridgeApi.extrude(bridgeUrl, step, speed)
            remaining -= abs(step)
        }
    }

    LaunchedEffect(bridgeUrl) {
        while (true) {
            pollOnce(false)
            delay(2000)
        }
    }

    val isWide = LocalConfiguration.current.screenWidthDp >= 720

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding()
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp)
    ) {
        // Header + 2-tap E-STOP + Bridge URL
        Card(
            colors = CardDefaults.cardColors(containerColor = CardDark),
            border = BorderStroke(1.dp, BorderSlate),
            shape = RoundedCornerShape(12.dp)
        ) {
            Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text("IVI Control Panel", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                        Text(
                            text = serial?.firmware?.ifBlank { "Wi-Fi + USB Marlin Bridge" } ?: "Wi-Fi + USB Marlin Bridge",
                            fontSize = 11.sp,
                            color = TextMuted,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis
                        )
                    }
                    Spacer(Modifier.width(8.dp))
                    val estopArmed = armedAction == "estop"
                    Button(
                        onClick = {
                            requireTwoTap("estop", "CONFIRM E-STOP") {
                                runAction("EMERGENCY STOP (M112) SENT!", allowWhilePrinting = true) {
                                    BridgeApi.eStop(bridgeUrl)
                                }
                            }
                        },
                        colors = ButtonDefaults.buttonColors(containerColor = if (estopArmed) AccentRed else AccentRedDim),
                        border = if (estopArmed) BorderStroke(2.dp, Color(0xFFFEF08A)) else null,
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 6.dp),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text(
                            if (estopArmed) "CONFIRM E-STOP!" else "E-STOP",
                            fontWeight = FontWeight.Bold,
                            fontSize = 12.sp,
                            color = Color.White
                        )
                    }
                }

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    StatusPill(
                        label = if (wifiOnline) "Wi-Fi: Online" else "Wi-Fi: Offline",
                        bg = if (wifiOnline) Color(0xFF064E3B) else Color(0xFF451A03)
                    )
                    StatusPill(
                        label = if (serialReady) "USB: ${serial?.workingStatus?.ifBlank { "OK" }}" else "USB: Off",
                        bg = when {
                            isPrinting -> Color(0xFF1E3A8A)
                            serialReady -> Color(0xFF064E3B)
                            else -> Color(0xFF451A03)
                        }
                    )
                    if (serialReady && serial != null) {
                        if (serial.atHome) {
                            StatusPill(label = "Homed", bg = Color(0xFF064E3B))
                        } else if (!serial.homed) {
                            StatusPill(label = "Unhomed", bg = Color(0xFF451A03))
                        }
                    }
                }

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    OutlinedTextField(
                        value = urlDraft,
                        onValueChange = { urlDraft = it },
                        singleLine = true,
                        modifier = Modifier.weight(1f),
                        label = { Text("ivi-bridge URL (Tailscale/LAN)", fontSize = 11.sp) },
                        colors = OutlinedTextFieldDefaults.colors(
                            focusedBorderColor = AccentCyanLight,
                            unfocusedBorderColor = BorderSlate,
                            focusedTextColor = TextPrimary,
                            unfocusedTextColor = TextPrimary
                        )
                    )
                    Button(
                        onClick = {
                            val norm = BridgeApi.normalizeUrl(urlDraft)
                            bridgeUrl = norm
                            urlDraft = norm
                            prefs.edit().putString("bridge_url", norm).apply()
                            scope.launch { pollOnce(true) }
                        },
                        colors = ButtonDefaults.buttonColors(containerColor = AccentCyan),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text("Connect", fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
        }

        // Print-Active Lockout Banner
        if (isPrinting) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(Color(0xFF1E3A8A), RoundedCornerShape(8.dp))
                    .padding(horizontal = 10.dp, vertical = 7.dp)
            ) {
                Text(
                    "PRINT IN PROGRESS (${serial?.workingStatus}) — Monitor-only mode: controls locked to protect your print.",
                    fontSize = 11.sp,
                    fontWeight = FontWeight.Bold,
                    color = Color(0xFFE0F2FE)
                )
            }
        }

        // Live Telemetry Strip
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            TelemetryCard(
                modifier = Modifier.weight(1f),
                label = "NOZZLE",
                value = if (serialReady && serial != null) String.format(Locale.US, "%.1f°C", serial.nozzleActual) else "—",
                sub = if (serialReady && serial != null) "/ ${serial.nozzleTarget.toInt()}°C" else "",
                accent = if ((serial?.nozzleTarget ?: 0.0) > 0) Color(0xFFFB7185) else TextPrimary
            )
            TelemetryCard(
                modifier = Modifier.weight(1f),
                label = "BED",
                value = if (serialReady && serial != null) String.format(Locale.US, "%.1f°C", serial.bedActual) else "—",
                sub = if (serialReady && serial != null) "/ ${serial.bedTarget.toInt()}°C" else "",
                accent = if ((serial?.bedTarget ?: 0.0) > 0) Color(0xFFFBBF24) else TextPrimary
            )
            TelemetryCard(
                modifier = Modifier.weight(1.4f),
                label = "XYZ POSITION",
                value = if (serialReady && serial != null) {
                    String.format(Locale.US, "%.1f, %.1f, %.1f", serial.x, serial.y, serial.z)
                } else "—",
                sub = if (serialReady && serial != null) "Fan ${serial.fanPercent}%" else "",
                accent = AccentCyanLight
            )
        }

        // Tab Bar
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(6.dp)
        ) {
            TabButton("Control", activeTab == 0, Modifier.weight(1f)) { activeTab = 0 }
            TabButton("Filament", activeTab == 1, Modifier.weight(1f)) { activeTab = 1 }
            TabButton("Console", activeTab == 2, Modifier.weight(1f)) { activeTab = 2 }
            TabButton("History", activeTab == 3, Modifier.weight(1f)) { activeTab = 3 }
        }

        if (notice.isNotBlank()) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(if (noticeError) Color(0xFF4C0519) else Color(0xFF064E3B), RoundedCornerShape(8.dp))
                    .padding(horizontal = 10.dp, vertical = 6.dp)
            ) {
                Text(notice, fontSize = 12.sp, color = TextPrimary, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }

        // Main Tab Body
        when (activeTab) {
            0 -> {
                val scroll = rememberScrollState()
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .verticalScroll(scroll),
                    verticalArrangement = Arrangement.spacedBy(10.dp)
                ) {
                    val onJogAction: (String, Int) -> Unit = { axis, sign ->
                        val feed = when (speedMode) {
                            0 -> if (axis == "Z") 600.0 else 1200.0
                            2 -> if (axis == "Z") 3000.0 else 6000.0
                            else -> if (axis == "Z") 1500.0 else 3000.0
                        }
                        val dist = jogStep * sign
                        runAction("Jogged $axis ${if (dist > 0) "+" else ""}$dist mm") {
                            BridgeApi.jog(bridgeUrl, axis, dist, feed)
                        }
                    }
                    val onHomeAction: () -> Unit = {
                        requireTwoTap("home", "HOME") {
                            runAction("Homed all towers (G28)") { BridgeApi.home(bridgeUrl) }
                        }
                    }
                    val onMotorsOffAction: () -> Unit = {
                        requireTwoTap("motors", "Motors Off") {
                            runAction("Motors disabled (M84)") { BridgeApi.motorsOff(bridgeUrl) }
                        }
                    }
                    val onTempAction: (String, Int) -> Unit = { heater, target ->
                        runAction("${heater.replaceFirstChar { it.uppercase() }} set to $target°C") {
                            BridgeApi.setTemp(bridgeUrl, heater, target)
                        }
                    }
                    val onCooldownAction: () -> Unit = {
                        runAction("Cooldown: Nozzle 0°C, Bed 0°C, Fan Off") {
                            BridgeApi.setTemp(bridgeUrl, "nozzle", 0)
                            BridgeApi.setTemp(bridgeUrl, "bed", 0)
                            BridgeApi.setFan(bridgeUrl, 0)
                        }
                    }
                    val onExtrudeAction: (Int) -> Unit = { sign ->
                        val spd = extrudeSpeedDraft.toDoubleOrNull()?.takeIf { it > 0 } ?: extrudeSpeed
                        val verb = if (sign > 0) "Extruded" else "Retracted"
                        runAction("$verb ${extrudeLen.toInt()} mm @ ${spd.toInt()} mm/min") {
                            BridgeApi.extrude(bridgeUrl, extrudeLen * sign, spd)
                        }
                    }

                    if (isWide) {
                        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                                MotionCard(
                                    enabled = controlsEnabled,
                                    homeArmed = armedAction == "home",
                                    motorsArmed = armedAction == "motors",
                                    jogStep = jogStep,
                                    speedMode = speedMode,
                                    zPos = serial?.z,
                                    onStepChange = { jogStep = it },
                                    onSpeedChange = { speedMode = it },
                                    onJog = onJogAction,
                                    onHome = onHomeAction,
                                    onMotorsOff = onMotorsOffAction
                                )
                                BabystepFanCard(
                                    enabled = controlsEnabled,
                                    fanPercent = serial?.fanPercent ?: 0,
                                    onBabystep = { d ->
                                        runAction("Z babystep ${if (d > 0) "+" else ""}$d mm") {
                                            BridgeApi.babystep(bridgeUrl, d)
                                        }
                                    },
                                    onFan = { pct ->
                                        runAction("Part fan $pct%") { BridgeApi.setFan(bridgeUrl, pct) }
                                    }
                                )
                            }
                            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                                ThermalCard(
                                    enabled = controlsEnabled,
                                    nozzleDraft = nozzleDraft,
                                    bedDraft = bedDraft,
                                    onNozzleDraft = { nozzleDraft = it },
                                    onBedDraft = { bedDraft = it },
                                    onSetTemp = onTempAction,
                                    onCooldownAll = onCooldownAction
                                )
                                ExtruderCard(
                                    enabled = extrudeEnabled,
                                    hotendHot = hotendHot,
                                    extrudeLen = extrudeLen,
                                    extrudeSpeed = extrudeSpeed,
                                    extrudeSpeedDraft = extrudeSpeedDraft,
                                    onLenChange = { extrudeLen = it },
                                    onSpeedChange = { spd ->
                                        extrudeSpeed = spd
                                        extrudeSpeedDraft = spd.toInt().toString()
                                    },
                                    onSpeedDraft = { txt ->
                                        extrudeSpeedDraft = txt
                                        txt.toDoubleOrNull()?.takeIf { it > 0 }?.let { extrudeSpeed = it }
                                    },
                                    onExtrude = onExtrudeAction
                                )
                            }
                        }
                    } else {
                        MotionCard(
                            enabled = controlsEnabled,
                            homeArmed = armedAction == "home",
                            motorsArmed = armedAction == "motors",
                            jogStep = jogStep,
                            speedMode = speedMode,
                            zPos = serial?.z,
                            onStepChange = { jogStep = it },
                            onSpeedChange = { speedMode = it },
                            onJog = onJogAction,
                            onHome = onHomeAction,
                            onMotorsOff = onMotorsOffAction
                        )
                        ThermalCard(
                            enabled = controlsEnabled,
                            nozzleDraft = nozzleDraft,
                            bedDraft = bedDraft,
                            onNozzleDraft = { nozzleDraft = it },
                            onBedDraft = { bedDraft = it },
                            onSetTemp = onTempAction,
                            onCooldownAll = onCooldownAction
                        )
                        ExtruderCard(
                            enabled = extrudeEnabled,
                            hotendHot = hotendHot,
                            extrudeLen = extrudeLen,
                            extrudeSpeed = extrudeSpeed,
                            extrudeSpeedDraft = extrudeSpeedDraft,
                            onLenChange = { extrudeLen = it },
                            onSpeedChange = { spd ->
                                extrudeSpeed = spd
                                extrudeSpeedDraft = spd.toInt().toString()
                            },
                            onSpeedDraft = { txt ->
                                extrudeSpeedDraft = txt
                                txt.toDoubleOrNull()?.takeIf { it > 0 }?.let { extrudeSpeed = it }
                            },
                            onExtrude = onExtrudeAction
                        )
                        BabystepFanCard(
                            enabled = controlsEnabled,
                            fanPercent = serial?.fanPercent ?: 0,
                            onBabystep = { d ->
                                runAction("Z babystep ${if (d > 0) "+" else ""}$d mm") {
                                    BridgeApi.babystep(bridgeUrl, d)
                                }
                            },
                            onFan = { pct ->
                                runAction("Part fan $pct%") { BridgeApi.setFan(bridgeUrl, pct) }
                            }
                        )
                    }
                    Spacer(Modifier.height(64.dp))
                }
            }

            1 -> {
                FilamentWizardTab(
                    modifier = Modifier.weight(1f),
                    controlsEnabled = controlsEnabled,
                    extrudeEnabled = extrudeEnabled,
                    hotendHot = hotendHot,
                    busy = busy,
                    nozzleActual = serial?.nozzleActual ?: 0.0,
                    nozzleTarget = serial?.nozzleTarget ?: 0.0,
                    material = wizardMaterial,
                    bowdenLen = bowdenLen,
                    statusNote = wizardNote,
                    onHeat = { mat, target ->
                        wizardMaterial = mat
                        nozzleDraft = target.toString()
                        wizardNote = "Heating nozzle to ${target}°C for $mat... wait until ≥ ${COLD_EXTRUSION_MIN_C.toInt()}°C."
                        runAction("Nozzle target set to ${target}°C") {
                            BridgeApi.setTemp(bridgeUrl, "nozzle", target)
                        }
                    },
                    onBowdenLen = { bowdenLen = it },
                    onUnload = {
                        runAction("Filament unloaded cleanly.") {
                            wizardNote = "Tip-shaping micro-purge (+5 mm)..."
                            BridgeApi.extrude(bridgeUrl, 5.0, 120.0)
                            wizardNote = "Fast pull out of melt zone (-20 mm)..."
                            BridgeApi.extrude(bridgeUrl, -20.0, 900.0)
                            extrudeChunked(-bowdenLen.toDouble(), 1200.0, "Retracting Bowden (-$bowdenLen mm)")
                            wizardNote = "Unloaded! Pull old spool free, cut new tip at 45°, feed past filament sensor into gears, then tap Load."
                        }
                    },
                    onLoad = {
                        runAction("Filament loaded and purged.") {
                            val fastFeed = max(50, bowdenLen - 30)
                            extrudeChunked(fastFeed.toDouble(), 1200.0, "Fast-feeding Bowden (+$fastFeed mm)")
                            wizardNote = "Slow purging melt zone (+40 mm @ 120 mm/min)..."
                            BridgeApi.extrude(bridgeUrl, 40.0, 120.0)
                            wizardNote = "Loaded & purged! Tap Purge +15 mm until color is clean, or tap Cooldown Nozzle."
                        }
                    },
                    onPurgeMore = {
                        runAction("Purged +15 mm @ 120 mm/min") {
                            BridgeApi.extrude(bridgeUrl, 15.0, 120.0)
                        }
                    },
                    onCooldownNozzle = {
                        runAction("Nozzle target set to 0°C") {
                            BridgeApi.setTemp(bridgeUrl, "nozzle", 0)
                        }
                    }
                )
            }

            2 -> {
                ConsoleTab(
                    modifier = Modifier.weight(1f),
                    enabled = controlsEnabled,
                    clearEnabled = serialReady && !busy,
                    lines = serial?.console ?: emptyList(),
                    gcodeDraft = gcodeDraft,
                    onDraftChange = { gcodeDraft = it },
                    onSend = { cmd ->
                        val line = cmd.trim()
                        if (line.isNotEmpty()) {
                            gcodeDraft = ""
                            runAction("Sent: $line") { BridgeApi.sendGCode(bridgeUrl, line) }
                        }
                    },
                    onClear = {
                        runAction("Console cleared", allowWhilePrinting = true) { BridgeApi.clearConsole(bridgeUrl) }
                    }
                )
            }

            else -> {
                HistoryTab(
                    modifier = Modifier.weight(1f),
                    state = state,
                    busy = busy,
                    onToggleTransform = {
                        val next = !(state?.autoTransform ?: true)
                        runAction("Auto-transform ${if (next) "ON" else "OFF"}", allowWhilePrinting = true) {
                            BridgeApi.updateSettings(bridgeUrl, autoTransform = next)
                        }
                    },
                    onToggleUnique = {
                        val next = !(state?.uniqueNames ?: true)
                        runAction("Unique names ${if (next) "ON" else "OFF"}", allowWhilePrinting = true) {
                            BridgeApi.updateSettings(bridgeUrl, uniqueNames = next)
                        }
                    },
                    onClearHistory = {
                        runAction("Cleared upload history", allowWhilePrinting = true) { BridgeApi.clearHistory(bridgeUrl) }
                    }
                )
            }
        }
    }
}

@Composable
private fun StatusPill(label: String, bg: Color) {
    Box(
        modifier = Modifier
            .background(bg, RoundedCornerShape(999.dp))
            .padding(horizontal = 10.dp, vertical = 4.dp)
    ) {
        Text(label, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, color = TextPrimary)
    }
}

@Composable
private fun TelemetryCard(modifier: Modifier, label: String, value: String, sub: String, accent: Color) {
    Card(
        modifier = modifier,
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(10.dp)
    ) {
        Column(modifier = Modifier.padding(10.dp)) {
            Text(label, fontSize = 10.sp, fontWeight = FontWeight.Bold, color = TextMuted)
            Text(value, fontSize = 16.sp, fontWeight = FontWeight.Bold, color = accent, maxLines = 1)
            if (sub.isNotBlank()) {
                Text(sub, fontSize = 11.sp, color = TextMuted, maxLines = 1)
            }
        }
    }
}

@Composable
private fun TabButton(title: String, selected: Boolean, modifier: Modifier, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        modifier = modifier,
        colors = ButtonDefaults.buttonColors(
            containerColor = if (selected) AccentCyan else CardDark,
            contentColor = if (selected) Color.White else TextMuted
        ),
        border = BorderStroke(1.dp, if (selected) AccentCyanLight else BorderSlate),
        shape = RoundedCornerShape(8.dp),
        contentPadding = PaddingValues(vertical = 8.dp, horizontal = 4.dp)
    ) {
        Text(title, fontSize = 11.sp, fontWeight = FontWeight.Bold, maxLines = 1)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MotionCard(
    enabled: Boolean,
    homeArmed: Boolean,
    motorsArmed: Boolean,
    jogStep: Double,
    speedMode: Int,
    zPos: Double?,
    onStepChange: (Double) -> Unit,
    onSpeedChange: (Int) -> Unit,
    onJog: (String, Int) -> Unit,
    onHome: () -> Unit,
    onMotorsOff: () -> Unit
) {
    Card(
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Toolhead & Homing", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                OutlinedButton(
                    onClick = onMotorsOff,
                    enabled = enabled,
                    border = BorderStroke(if (motorsArmed) 2.dp else 1.dp, if (motorsArmed) Color(0xFFFEF08A) else BorderSlate),
                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Text(
                        if (motorsArmed) "Confirm M84?" else "Motors Off",
                        fontSize = 11.sp,
                        color = if (motorsArmed) Color(0xFFFEF08A) else TextPrimary
                    )
                }
            }

            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(0.1, 1.0, 10.0, 50.0).forEach { step ->
                    val label = if (step < 1.0) "${step}mm" else "${step.toInt()}mm"
                    ChoiceChipBtn(label, jogStep == step) { onStepChange(step) }
                }
                Spacer(Modifier.width(4.dp))
                listOf("Slow", "Normal", "Fast").forEachIndexed { idx, name ->
                    ChoiceChipBtn(name, speedMode == idx) { onSpeedChange(idx) }
                }
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceEvenly,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    JogBtn("+Y", enabled) { onJog("Y", 1) }
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        JogBtn("-X", enabled) { onJog("X", -1) }
                        Button(
                            onClick = onHome,
                            enabled = enabled,
                            modifier = Modifier.size(64.dp, 48.dp),
                            colors = ButtonDefaults.buttonColors(containerColor = if (homeArmed) AccentAmber else AccentCyan),
                            border = if (homeArmed) BorderStroke(2.dp, Color(0xFFFEF08A)) else null,
                            shape = RoundedCornerShape(10.dp),
                            contentPadding = PaddingValues(0.dp)
                        ) {
                            Text(if (homeArmed) "SURE?" else "HOME", fontSize = 12.sp, fontWeight = FontWeight.Bold)
                        }
                        JogBtn("+X", enabled) { onJog("X", 1) }
                    }
                    JogBtn("-Y", enabled) { onJog("Y", -1) }
                }

                Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("Z AXIS", fontSize = 11.sp, fontWeight = FontWeight.Bold, color = TextMuted)
                    JogBtn("+Z", enabled, widthDp = 76) { onJog("Z", 1) }
                    Text(
                        text = zPos?.let { String.format(Locale.US, "%.1f", it) } ?: "—",
                        fontSize = 14.sp,
                        fontWeight = FontWeight.Bold,
                        color = AccentCyanLight
                    )
                    JogBtn("-Z", enabled, widthDp = 76) { onJog("Z", -1) }
                }
            }
        }
    }
}

@Composable
private fun JogBtn(label: String, enabled: Boolean, widthDp: Int = 64, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        enabled = enabled,
        modifier = Modifier.size(widthDp.dp, 48.dp),
        colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF334155)),
        shape = RoundedCornerShape(10.dp),
        contentPadding = PaddingValues(0.dp)
    ) {
        Text(label, fontSize = 14.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ThermalCard(
    enabled: Boolean,
    nozzleDraft: String,
    bedDraft: String,
    onNozzleDraft: (String) -> Unit,
    onBedDraft: (String) -> Unit,
    onSetTemp: (String, Int) -> Unit,
    onCooldownAll: () -> Unit
) {
    Card(
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Temperature Control", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                OutlinedButton(
                    onClick = onCooldownAll,
                    enabled = enabled,
                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Text("Cooldown All", fontSize = 11.sp, color = AccentCyanLight)
                }
            }

            Text("Nozzle (Hotend)", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = TextMuted)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(0, 180, 210, 220, 240).forEach { t ->
                    Button(
                        onClick = {
                            onNozzleDraft(t.toString())
                            onSetTemp("nozzle", t)
                        },
                        enabled = enabled,
                        colors = ButtonDefaults.buttonColors(containerColor = if (t == 0) Color(0xFF334155) else Color(0xFF7F1D1D)),
                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 6.dp),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text(if (t == 0) "Off" else "${t}°C", fontSize = 12.sp)
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = nozzleDraft,
                    onValueChange = onNozzleDraft,
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.width(100.dp),
                    label = { Text("°C", fontSize = 11.sp) }
                )
                Button(
                    onClick = { onSetTemp("nozzle", nozzleDraft.toIntOrNull() ?: 0) },
                    enabled = enabled,
                    colors = ButtonDefaults.buttonColors(containerColor = AccentCyan),
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Text("Set Nozzle", fontSize = 12.sp)
                }
            }

            Text("Heated Bed", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = TextMuted)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(0, 50, 60, 75, 80).forEach { t ->
                    Button(
                        onClick = {
                            onBedDraft(t.toString())
                            onSetTemp("bed", t)
                        },
                        enabled = enabled,
                        colors = ButtonDefaults.buttonColors(containerColor = if (t == 0) Color(0xFF334155) else Color(0xFF78350F)),
                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 6.dp),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text(if (t == 0) "Off" else "${t}°C", fontSize = 12.sp)
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = bedDraft,
                    onValueChange = onBedDraft,
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.width(100.dp),
                    label = { Text("°C", fontSize = 11.sp) }
                )
                Button(
                    onClick = { onSetTemp("bed", bedDraft.toIntOrNull() ?: 0) },
                    enabled = enabled,
                    colors = ButtonDefaults.buttonColors(containerColor = AccentCyan),
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Text("Set Bed", fontSize = 12.sp)
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ExtruderCard(
    enabled: Boolean,
    hotendHot: Boolean,
    extrudeLen: Double,
    extrudeSpeed: Double,
    extrudeSpeedDraft: String,
    onLenChange: (Double) -> Unit,
    onSpeedChange: (Double) -> Unit,
    onSpeedDraft: (String) -> Unit,
    onExtrude: (Int) -> Unit
) {
    Card(
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Filament / Extruder", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                Text(
                    if (hotendHot) "Hotend ready" else "Locked < ${COLD_EXTRUSION_MIN_C.toInt()}°C",
                    fontSize = 11.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = if (hotendHot) Color(0xFF34D399) else Color(0xFFFBBF24)
                )
            }

            Text("Length (mm)", fontSize = 12.sp, color = TextMuted)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(1.0, 5.0, 10.0, 25.0, 50.0).forEach { len ->
                    ChoiceChipBtn("${len.toInt()}mm", extrudeLen == len) { onLenChange(len) }
                }
            }

            Text("Extruder Speed (mm/min)", fontSize = 12.sp, color = TextMuted)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(60.0, 180.0, 300.0, 600.0).forEach { spd ->
                    ChoiceChipBtn("${spd.toInt()}", extrudeSpeed == spd) { onSpeedChange(spd) }
                }
            }
            OutlinedTextField(
                value = extrudeSpeedDraft,
                onValueChange = onSpeedDraft,
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                modifier = Modifier.width(140.dp),
                label = { Text("Custom mm/min", fontSize = 11.sp) }
            )

            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                Button(
                    onClick = { onExtrude(-1) },
                    enabled = enabled,
                    modifier = Modifier.weight(1f),
                    colors = ButtonDefaults.buttonColors(containerColor = AccentAmber),
                    shape = RoundedCornerShape(10.dp)
                ) {
                    Text("Retract -${extrudeLen.toInt()} mm", fontWeight = FontWeight.Bold, fontSize = 13.sp)
                }
                Button(
                    onClick = { onExtrude(1) },
                    enabled = enabled,
                    modifier = Modifier.weight(1f),
                    colors = ButtonDefaults.buttonColors(containerColor = AccentGreen),
                    shape = RoundedCornerShape(10.dp)
                ) {
                    Text("Extrude +${extrudeLen.toInt()} mm", fontWeight = FontWeight.Bold, fontSize = 13.sp)
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FilamentWizardTab(
    modifier: Modifier,
    controlsEnabled: Boolean,
    extrudeEnabled: Boolean,
    hotendHot: Boolean,
    busy: Boolean,
    nozzleActual: Double,
    nozzleTarget: Double,
    material: String,
    bowdenLen: Int,
    statusNote: String,
    onHeat: (String, Int) -> Unit,
    onBowdenLen: (Int) -> Unit,
    onUnload: () -> Unit,
    onLoad: () -> Unit,
    onPurgeMore: () -> Unit,
    onCooldownNozzle: () -> Unit
) {
    val scroll = rememberScrollState()
    Column(
        modifier = modifier.verticalScroll(scroll),
        verticalArrangement = Arrangement.spacedBy(10.dp)
    ) {
        Card(
            colors = CardDefaults.cardColors(containerColor = CardDark),
            border = BorderStroke(1.dp, BorderSlate),
            shape = RoundedCornerShape(12.dp)
        ) {
            Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text("Filament Change Wizard", fontSize = 16.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                    Text(
                        String.format(Locale.US, "%.1f°C / %.0f°C", nozzleActual, nozzleTarget),
                        fontSize = 12.sp,
                        fontWeight = FontWeight.Bold,
                        color = if (hotendHot) Color(0xFF34D399) else Color(0xFFFBBF24)
                    )
                }

                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(BgDark, RoundedCornerShape(8.dp))
                        .padding(10.dp)
                ) {
                    Text(statusNote, fontSize = 12.sp, color = Color(0xFFE0F2FE))
                }

                // Step 1
                Text("Step 1 — Select Material & Heat", fontSize = 13.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Button(
                        onClick = { onHeat("PLA", 220) },
                        enabled = controlsEnabled,
                        colors = ButtonDefaults.buttonColors(containerColor = if (material == "PLA") Color(0xFF7F1D1D) else Color(0xFF334155)),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text("Heat PLA (220°C)", fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    }
                    Button(
                        onClick = { onHeat("PETG", 240) },
                        enabled = controlsEnabled,
                        colors = ButtonDefaults.buttonColors(containerColor = if (material == "PETG") Color(0xFF7F1D1D) else Color(0xFF334155)),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text("Heat PETG (240°C)", fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    }
                }
                Text("Bowden Tube Length", fontSize = 11.sp, color = TextMuted)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    listOf(350, 450, 550).forEach { len ->
                        ChoiceChipBtn("${len}mm", bowdenLen == len) { onBowdenLen(len) }
                    }
                }

                // Step 2
                Text("Step 2 — Unload Old Filament", fontSize = 13.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                Text(
                    "Micro-purges +5 mm, pulls -20 mm fast out of the melt zone to prevent clogs, then retracts -$bowdenLen mm.",
                    fontSize = 11.sp,
                    color = TextMuted
                )
                Button(
                    onClick = onUnload,
                    enabled = extrudeEnabled,
                    modifier = Modifier.fillMaxWidth(),
                    colors = ButtonDefaults.buttonColors(containerColor = AccentAmber),
                    shape = RoundedCornerShape(10.dp)
                ) {
                    Text(
                        if (busy) "Working..." else "Unload Old Filament (-${bowdenLen + 20} mm)",
                        fontWeight = FontWeight.Bold,
                        fontSize = 13.sp
                    )
                }

                // Step 3
                Text("Step 3 — Insert New Spool", fontSize = 13.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                Text(
                    "1. Pull old filament free.\n2. Cut new tip at 45°.\n3. Feed through filament sensor until gears grip, then tap Load below.",
                    fontSize = 11.sp,
                    color = TextMuted
                )

                // Step 4
                Text("Step 4 — Load, Purge & Finish", fontSize = 13.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                Button(
                    onClick = onLoad,
                    enabled = extrudeEnabled,
                    modifier = Modifier.fillMaxWidth(),
                    colors = ButtonDefaults.buttonColors(containerColor = AccentGreen),
                    shape = RoundedCornerShape(10.dp)
                ) {
                    Text(
                        if (busy) "Working..." else "Load & Purge (+${bowdenLen + 10} mm)",
                        fontWeight = FontWeight.Bold,
                        fontSize = 13.sp
                    )
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    Button(
                        onClick = onPurgeMore,
                        enabled = extrudeEnabled,
                        modifier = Modifier.weight(1f),
                        colors = ButtonDefaults.buttonColors(containerColor = AccentCyan),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text("Purge +15 mm", fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    }
                    OutlinedButton(
                        onClick = onCooldownNozzle,
                        enabled = controlsEnabled,
                        modifier = Modifier.weight(1f),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text("Cooldown Nozzle", fontSize = 12.sp, color = AccentCyanLight)
                    }
                }
            }
        }
        Spacer(Modifier.height(64.dp))
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun BabystepFanCard(
    enabled: Boolean,
    fanPercent: Int,
    onBabystep: (Double) -> Unit,
    onFan: (Int) -> Unit
) {
    Card(
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("Z Babystepping & Part Fan", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(-0.05, -0.01, 0.01, 0.05).forEach { d ->
                    val label = if (d > 0) "+${String.format(Locale.US, "%.2f", d)}" else String.format(Locale.US, "%.2f", d)
                    OutlinedButton(
                        onClick = { onBabystep(d) },
                        enabled = enabled,
                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 6.dp),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text("${label}mm", fontSize = 12.sp, color = TextPrimary)
                    }
                }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf(0, 25, 50, 75, 100).forEach { pct ->
                    ChoiceChipBtn(if (pct == 0) "Fan Off" else "$pct%", fanPercent == pct, enabled) { onFan(pct) }
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ConsoleTab(
    modifier: Modifier,
    enabled: Boolean,
    clearEnabled: Boolean,
    lines: List<ConsoleLine>,
    gcodeDraft: String,
    onDraftChange: (String) -> Unit,
    onSend: (String) -> Unit,
    onClear: () -> Unit
) {
    Card(
        modifier = modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp).fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf("M105", "M114", "M115", "M503").forEach { cmd ->
                    OutlinedButton(
                        onClick = { onSend(cmd) },
                        enabled = enabled,
                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                        shape = RoundedCornerShape(8.dp)
                    ) {
                        Text(cmd, fontSize = 12.sp, color = TextPrimary)
                    }
                }
                OutlinedButton(
                    onClick = onClear,
                    enabled = clearEnabled,
                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Text("Clear", fontSize = 12.sp, color = TextMuted)
                }
            }

            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = gcodeDraft,
                    onValueChange = onDraftChange,
                    singleLine = true,
                    enabled = enabled,
                    modifier = Modifier.weight(1f),
                    label = { Text("Marlin G-code (e.g. G28, M105)", fontSize = 11.sp) }
                )
                Button(
                    onClick = { onSend(gcodeDraft) },
                    enabled = enabled,
                    colors = ButtonDefaults.buttonColors(containerColor = AccentCyan),
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Text("Send", fontSize = 12.sp, fontWeight = FontWeight.Bold)
                }
            }

            LazyColumn(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .background(WellDark, RoundedCornerShape(8.dp))
                    .padding(10.dp),
                contentPadding = PaddingValues(bottom = 64.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp)
            ) {
                items(lines.asReversed()) { line ->
                    val prefix = when (line.dir) {
                        "tx" -> ">> "
                        "sys" -> "** "
                        else -> "<< "
                    }
                    val color = when (line.dir) {
                        "tx" -> AccentCyanLight
                        "sys" -> Color(0xFFFBBF24)
                        else -> Color(0xFF86EFAC)
                    }
                    Text(
                        text = "$prefix${line.text}",
                        fontSize = 12.sp,
                        fontFamily = FontFamily.Monospace,
                        color = color
                    )
                }
            }
        }
    }
}

@Composable
private fun HistoryTab(
    modifier: Modifier,
    state: BridgeState?,
    busy: Boolean,
    onToggleTransform: () -> Unit,
    onToggleUnique: () -> Unit,
    onClearHistory: () -> Unit
) {
    Card(
        modifier = modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = CardDark),
        border = BorderStroke(1.dp, BorderSlate),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(12.dp).fillMaxSize(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                ChoiceChipBtn(
                    "Auto-Transform: ${if (state?.autoTransform == true) "ON" else "OFF"}",
                    state?.autoTransform == true,
                    !busy
                ) { onToggleTransform() }
                ChoiceChipBtn(
                    "Unique Names: ${if (state?.uniqueNames == true) "ON" else "OFF"}",
                    state?.uniqueNames == true,
                    !busy
                ) { onToggleUnique() }
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Upload History", fontSize = 15.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                OutlinedButton(
                    onClick = onClearHistory,
                    enabled = !busy && (state?.history?.isNotEmpty() == true),
                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp)
                ) {
                    Text("Clear", fontSize = 12.sp, color = TextPrimary)
                }
            }

            LazyColumn(
                modifier = Modifier.weight(1f),
                contentPadding = PaddingValues(bottom = 64.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                items(state?.history ?: emptyList()) { rec ->
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(BgDark, RoundedCornerShape(8.dp))
                            .padding(10.dp)
                    ) {
                        Text(rec.sentName, fontSize = 13.sp, fontWeight = FontWeight.Bold, color = TextPrimary)
                        Spacer(Modifier.height(2.dp))
                        Text(
                            "${rec.material} · Nozzle ${rec.nozzleTemp}°C · Bed ${rec.bedTemp}°C · Z ${String.format(Locale.US, "%.1f..%.1f", rec.minZ, rec.maxZ)}mm · ${rec.response}",
                            fontSize = 11.sp,
                            color = TextMuted
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ChoiceChipBtn(label: String, selected: Boolean, enabled: Boolean = true, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        enabled = enabled,
        colors = ButtonDefaults.buttonColors(
            containerColor = if (selected) AccentCyan else BgDark,
            contentColor = if (selected) Color.White else TextMuted
        ),
        border = BorderStroke(1.dp, if (selected) AccentCyanLight else BorderSlate),
        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 6.dp),
        shape = RoundedCornerShape(8.dp)
    ) {
        Text(label, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
    }
}
