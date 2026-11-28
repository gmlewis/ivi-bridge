package com.gmlewis.ivicontrolpanel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL

data class PrinterStatus(
    val reachable: Boolean = false,
    val rawState: String = "",
    val octoState: String = "Offline",
    val error: String = ""
)

data class ConsoleLine(
    val timestamp: String,
    val dir: String,
    val text: String
)

data class PrintJobStatus(
    val active: Boolean = false,
    val source: String = "",
    val state: String = "",
    val fileName: String = "",
    val totalLines: Int = 0,
    val sentLines: Int = 0,
    val completion: Double = 0.0,
    val currentZ: Double = 0.0,
    val maxZ: Double = 0.0,
    val elapsedSec: Int = 0,
    val estimatedLeftSec: Int = 0,
    val estFinishAt: String = ""
)

data class SerialState(
    val enabled: Boolean = false,
    val connected: Boolean = false,
    val port: String = "",
    val baud: Int = 115200,
    val firmware: String = "",
    val workingStatus: String = "",
    val homed: Boolean = false,
    val atHome: Boolean = false,
    val nozzleActual: Double = 0.0,
    val nozzleTarget: Double = 0.0,
    val bedActual: Double = 0.0,
    val bedTarget: Double = 0.0,
    val fanPercent: Int = 0,
    val x: Double = 0.0,
    val y: Double = 0.0,
    val z: Double = 0.0,
    val e: Double = 0.0,
    val lastError: String = "",
    val printJob: PrintJobStatus? = null,
    val console: List<ConsoleLine> = emptyList()
)

data class UploadRecord(
    val timestamp: String,
    val sentName: String,
    val sizeBytes: Long,
    val material: String,
    val nozzleTemp: String,
    val bedTemp: String,
    val minZ: Double,
    val maxZ: Double,
    val response: String
)

data class BridgeState(
    val printerUrl: String = "",
    val autoTransform: Boolean = true,
    val uniqueNames: Boolean = true,
    val status: PrinterStatus = PrinterStatus(),
    val serial: SerialState = SerialState(),
    val history: List<UploadRecord> = emptyList()
)

object BridgeApi {
    const val DEFAULT_URL = "http://127.0.0.1:5000"

    fun normalizeUrl(raw: String): String {
        var s = raw.trim().trimEnd('/')
        if (s.isEmpty()) return DEFAULT_URL
        if (!s.startsWith("http://") && !s.startsWith("https://")) {
            s = "http://$s"
        }
        return s
    }

    private suspend fun requestJson(
        baseUrl: String,
        path: String,
        method: String = "GET",
        body: JSONObject? = null,
        readTimeoutMs: Int = 15_000
    ): JSONObject = withContext(Dispatchers.IO) {
        val url = URL("${normalizeUrl(baseUrl)}$path")
        val conn = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 5_000
            readTimeout = readTimeoutMs
            setRequestProperty("Accept", "application/json")
            if (body != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
            }
        }
        try {
            if (body != null) {
                OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(body.toString()) }
            }
            val code = conn.responseCode
            val stream = if (code in 200..299) conn.inputStream else (conn.errorStream ?: conn.inputStream)
            val text = BufferedReader(InputStreamReader(stream, Charsets.UTF_8)).use { it.readText() }
            val json = if (text.trim().startsWith("{")) JSONObject(text) else JSONObject()
            if (code !in 200..299) {
                val msg = json.optString("error").ifBlank { "HTTP $code" }
                throw IllegalStateException(msg)
            }
            json
        } finally {
            conn.disconnect()
        }
    }

    suspend fun fetchState(baseUrl: String): BridgeState {
        val j = requestJson(baseUrl, "/api/ivi/state", "GET", null, 5_000)
        val stObj = j.optJSONObject("status") ?: JSONObject()
        val serObj = j.optJSONObject("serial") ?: JSONObject()
        val histArr = j.optJSONArray("history") ?: JSONArray()

        val consoleArr = serObj.optJSONArray("console") ?: JSONArray()
        val consoleLines = buildList {
            for (i in 0 until consoleArr.length()) {
                val c = consoleArr.optJSONObject(i) ?: continue
                add(
                    ConsoleLine(
                        timestamp = c.optString("timestamp"),
                        dir = c.optString("dir", "rx"),
                        text = c.optString("text")
                    )
                )
            }
        }

        val history = buildList {
            for (i in 0 until histArr.length()) {
                val h = histArr.optJSONObject(i) ?: continue
                val meta = h.optJSONObject("meta") ?: JSONObject()
                add(
                    UploadRecord(
                        timestamp = h.optString("timestamp"),
                        sentName = h.optString("sent_name"),
                        sizeBytes = h.optLong("size_bytes"),
                        material = meta.optString("material", "PLA"),
                        nozzleTemp = meta.optString("nozzle_temp", "?"),
                        bedTemp = meta.optString("bed_temp", "?"),
                        minZ = meta.optDouble("min_z", 0.0),
                        maxZ = meta.optDouble("max_z", 0.0),
                        response = h.optString("response")
                    )
                )
            }
        }

        val pjObj = serObj.optJSONObject("print_job")
        val printJob = if (pjObj != null) {
            PrintJobStatus(
                active = pjObj.optBoolean("active", false),
                source = pjObj.optString("source"),
                state = pjObj.optString("state"),
                fileName = pjObj.optString("file_name"),
                totalLines = pjObj.optInt("total_lines", 0),
                sentLines = pjObj.optInt("sent_lines", 0),
                completion = pjObj.optDouble("completion", 0.0),
                currentZ = pjObj.optDouble("current_z", 0.0),
                maxZ = pjObj.optDouble("max_z", 0.0),
                elapsedSec = pjObj.optInt("elapsed_sec", 0),
                estimatedLeftSec = pjObj.optInt("estimated_left_sec", 0),
                estFinishAt = pjObj.optString("est_finish_at")
            )
        } else null

        return BridgeState(
            printerUrl = j.optString("printer_url"),
            autoTransform = j.optBoolean("auto_transform", true),
            uniqueNames = j.optBoolean("unique_names", true),
            status = PrinterStatus(
                reachable = stObj.optBoolean("reachable", false),
                rawState = stObj.optString("raw_state"),
                octoState = stObj.optString("octo_state", "Offline"),
                error = stObj.optString("error")
            ),
            serial = SerialState(
                enabled = serObj.optBoolean("enabled", false),
                connected = serObj.optBoolean("connected", false),
                port = serObj.optString("port"),
                baud = serObj.optInt("baud", 115200),
                firmware = serObj.optString("firmware"),
                workingStatus = serObj.optString("working_status"),
                homed = serObj.optBoolean("homed", false),
                atHome = serObj.optBoolean("at_home", false),
                nozzleActual = serObj.optDouble("nozzle_actual", 0.0),
                nozzleTarget = serObj.optDouble("nozzle_target", 0.0),
                bedActual = serObj.optDouble("bed_actual", 0.0),
                bedTarget = serObj.optDouble("bed_target", 0.0),
                fanPercent = serObj.optInt("fan_percent", 0),
                x = serObj.optDouble("x", 0.0),
                y = serObj.optDouble("y", 0.0),
                z = serObj.optDouble("z", 0.0),
                e = serObj.optDouble("e", 0.0),
                lastError = serObj.optString("last_error"),
                printJob = printJob,
                console = consoleLines
            ),
            history = history
        )
    }

    suspend fun jog(baseUrl: String, axis: String, distance: Double, feedrate: Double) {
        val body = JSONObject()
            .put("axis", axis)
            .put("distance", distance)
            .put("feedrate", feedrate)
        requestJson(baseUrl, "/api/ivi/serial/jog", "POST", body, 20_000)
    }

    suspend fun home(baseUrl: String) {
        requestJson(baseUrl, "/api/ivi/serial/home", "POST", JSONObject(), 65_000)
    }

    suspend fun setTemp(baseUrl: String, heater: String, target: Int) {
        val body = JSONObject().put("heater", heater).put("target", target)
        requestJson(baseUrl, "/api/ivi/serial/temp", "POST", body, 15_000)
    }

    suspend fun extrude(baseUrl: String, amount: Double, speed: Double) {
        val body = JSONObject().put("amount", amount).put("speed", speed)
        requestJson(baseUrl, "/api/ivi/serial/extrude", "POST", body, 35_000)
    }

    suspend fun setFan(baseUrl: String, percent: Int) {
        val body = JSONObject().put("percent", percent)
        requestJson(baseUrl, "/api/ivi/serial/fan", "POST", body, 10_000)
    }

    suspend fun babystep(baseUrl: String, delta: Double) {
        val body = JSONObject().put("delta", delta)
        requestJson(baseUrl, "/api/ivi/serial/babystep", "POST", body, 10_000)
    }

    suspend fun motorsOff(baseUrl: String) {
        requestJson(baseUrl, "/api/ivi/serial/motors_off", "POST", JSONObject(), 10_000)
    }

    suspend fun eStop(baseUrl: String) {
        requestJson(baseUrl, "/api/ivi/serial/estop", "POST", JSONObject(), 5_000)
    }

    suspend fun sendGCode(baseUrl: String, gcode: String) {
        val body = JSONObject().put("gcode", gcode)
        requestJson(baseUrl, "/api/ivi/serial/gcode", "POST", body, 60_000)
    }

    suspend fun clearConsole(baseUrl: String) {
        requestJson(baseUrl, "/api/ivi/serial/console/clear", "POST", JSONObject(), 5_000)
    }

    suspend fun sendJobCommand(baseUrl: String, command: String) {
        val body = if (command == "resume") {
            JSONObject().put("command", "pause").put("action", "resume")
        } else {
            JSONObject().put("command", command)
        }
        requestJson(baseUrl, "/api/job", "POST", body, 15_000)
    }

    suspend fun updateSettings(baseUrl: String, autoTransform: Boolean? = null, uniqueNames: Boolean? = null) {
        val body = JSONObject()
        if (autoTransform != null) body.put("auto_transform", autoTransform)
        if (uniqueNames != null) body.put("unique_names", uniqueNames)
        requestJson(baseUrl, "/api/ivi/settings", "POST", body, 5_000)
    }

    suspend fun clearHistory(baseUrl: String) {
        requestJson(baseUrl, "/api/ivi/history/clear", "POST", JSONObject(), 5_000)
    }
}
