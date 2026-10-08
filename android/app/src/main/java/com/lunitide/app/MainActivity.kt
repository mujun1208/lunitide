package com.lunitide.app

import android.annotation.SuppressLint
import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import android.net.http.SslError
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.SslErrorHandler
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import com.google.zxing.integration.android.IntentIntegrator
import com.google.zxing.integration.android.IntentResult
import org.json.JSONArray
import org.json.JSONObject
import java.net.URL
import java.security.MessageDigest
import java.security.SecureRandom
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager

/**
 * Lunitide 移动伴侣壳：单 Activity WebView 容器。
 *
 * 零业务逻辑——界面始终从电脑端网关现拉（壳内启动即 clearCache，
 * 配合网关 sw.js 的在线优先策略，电脑升级后 APP 自动访问最新版）。
 *
 * 关键职责：
 * 1. 深链接接管配对：lunitide://open?origin=..&fp=.. 与 https://<ip>:<port>/pair#c=..&fp=..
 * 2. 自签证书信任：SHA-256 指纹比对（配对 URL 内 fp 锚定 / TOFU 持久化）
 * 3. 多候选地址轮换：配对时网关下发 addresses（IPv4 + 公网 IPv6），
 *    蜂窝网络下局域网 IPv4 不可达自动切换 IPv6 直连家里电脑。
 */
class MainActivity : Activity() {

    companion object {
        private const val PREFS = "gateway"
        private const val KEY_ORIGIN = "origin"
        private const val KEY_FP = "fp"
        private const val KEY_CANDIDATES = "candidates"
        private const val LOAD_TIMEOUT_MS = 8000L
        private const val PROBE_TIMEOUT_MS = 3500L
        private const val EXIT_WINDOW_MS = 1500L
    }

    private lateinit var web: WebView
    private lateinit var prefs: SharedPreferences
    private val handler = Handler(Looper.getMainLooper())
    private var pendingTimeout: Runnable? = null
    private var candidates: List<String> = emptyList()
    private var candidateIdx = 0
    /** 候选可达性并行探测：origin -> 可达；缺键 = 探测仍在途（仅主线程读写）。 */
    private val probeResults = mutableMapOf<String, Boolean>()
    private var probeGen = 0
    private var lastProbedOrigin: String? = null
    /** 探测专用信任所有证书的 TLS 工厂：探测只测链路通不通，真实性仍由 WebView 指纹校验把关。 */
    private val trustAllFactory: SSLSocketFactory by lazy {
        val tm = arrayOf<TrustManager>(object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<java.security.cert.X509Certificate>, authType: String) {}
            override fun checkServerTrusted(chain: Array<java.security.cert.X509Certificate>, authType: String) {}
            override fun getAcceptedIssuers(): Array<java.security.cert.X509Certificate> = arrayOf()
        })
        SSLContext.getInstance("TLS").apply { init(null, tm, SecureRandom()) }.socketFactory
    }
    private var lastUrl: String? = null
    private var backPressedAt = 0L
    /** 浏览器配对后经 lunitide://open 传入的设备令牌：首次加载网关页时注入 localStorage。 */
    private var pendingToken: String? = null

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = getSharedPreferences(PREFS, MODE_PRIVATE)
        web = WebView(this)
        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            cacheMode = android.webkit.WebSettings.LOAD_DEFAULT
            userAgentString = "$userAgentString LunitideApp/$versionName"
            mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
        }
        web.webViewClient = gatewayClient()
        web.webChromeClient = WebChromeClient()
        web.addJavascriptInterface(ShellBridge(), "LunitideShell")
        setContentView(web)
        // 启动清缓存：确保不吃旧版本前端（网页资产每次从网关现拉）。
        web.clearCache(true)
        handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleIntent(intent)
    }

    private fun handleIntent(intent: Intent?) {
        val data = intent?.data
        when (data?.scheme) {
            "lunitide" -> {
                val origin = data.getQueryParameter("origin")
                val fp = data.getQueryParameter("fp")
                val token = data.getQueryParameter("token")
                // 浏览器配对页「已在 APP 中打开」传入：addresses 逗号分隔。
                val alt = data.getQueryParameter("addresses")
                    ?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }
                    ?: data.getQueryParameters("alt").orEmpty()
                if (origin != null) {
                    saveGateway(origin, fp, alt)
                    if (!token.isNullOrEmpty()) pendingToken = token
                }
                loadSavedOrEmpty()
            }
            "https" -> {
                // 系统相机扫码后 chooser 选择 Lunitide：配对 URL 原样进壳完成配对。
                // 保留旧候选（家里扫的公网 IPv6 在蜂窝下仍有效），新地址提前。
                val origin = originOf(data)
                val fp = fragmentParam(data, "fp")
                val host = data.host ?: ""
                val merged = (listOf(host) + decodeCandidates().filter { it != host }).filter { it.isNotEmpty() }
                saveGateway(origin, fp, merged)
                loadWithFailover(data.toString())
            }
            else -> loadSavedOrEmpty()
        }
    }

    private fun originOf(uri: Uri): String {
        val port = if (uri.port > 0) uri.port else 443
        // Uri.host 对 IPv6 返回裸地址（无方括号），拼 origin 必须补上方括号，
        // 否则 https://2409:...:47651 是非法 URL，重启后无法回连。
        val host = uri.host ?: return ""
        val bracketed = if (host.contains(':')) "[$host]" else host
        return "https://$bracketed:$port"
    }

    private fun fragmentParam(uri: Uri, key: String): String? {
        val fragment = uri.fragment ?: return null
        for (pair in fragment.split('&')) {
            val idx = pair.indexOf('=')
            if (idx > 0 && pair.substring(0, idx) == key) return pair.substring(idx + 1)
        }
        return null
    }

    private fun saveGateway(origin: String, fp: String?, alts: List<String>?) {
        prefs.edit().apply {
            putString(KEY_ORIGIN, origin)
            if (!fp.isNullOrEmpty()) putString(KEY_FP, fp)
            if (alts != null) putString(KEY_CANDIDATES, JSONArray(alts).toString())
            apply()
        }
        rebuildCandidates(origin, alts)
    }

    private fun rebuildCandidates(origin: String, alts: List<String>?) {
        val list = mutableListOf(origin)
        alts.orEmpty().forEach { alt ->
            val candidate = normalizeOrigin(alt)
            if (candidate != null && candidate != origin) list.add(candidate)
        }
        candidates = list
        candidateIdx = 0
        lastProbedOrigin = null
    }

    /** 裸地址（网关 hostAddresses 格式：IPv4 或 IPv6，不带 scheme/端口）→ origin。 */
    private fun normalizeOrigin(address: String): String? {
        val trimmed = address.trim()
        if (trimmed.isEmpty()) return null
        val host = if (trimmed.contains(':')) "[$trimmed]" else trimmed
        return "https://$host:47651"
    }

    private fun loadSavedOrEmpty() {
        val origin = prefs.getString(KEY_ORIGIN, null)
        if (origin == null) {
            renderWelcome()
            return
        }
        rebuildCandidates(origin, decodeCandidates())
        loadWithFailover("$origin/")
    }

    private fun decodeCandidates(): List<String> {
        val raw = prefs.getString(KEY_CANDIDATES, null) ?: return emptyList()
        return runCatching {
            val arr = JSONArray(raw)
            (0 until arr.length()).mapNotNull { arr.optString(it) }
        }.getOrDefault(emptyList())
    }

    private fun renderWelcome() {
        val html = """
            <!doctype html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width,initial-scale=1">
            <style>
              body{font-family:system-ui,sans-serif;background:#10141c;color:#e8ecf4;
                   display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
              .card{max-width:320px;padding:32px 24px;text-align:center;line-height:1.7}
              h1{font-size:20px;margin:0 0 12px}
              p{font-size:14px;color:#9aa7bd;margin:0 0 8px}
              .scan-btn{display:inline-block;margin:16px 0 12px;padding:12px 40px;font-size:16px;
                   color:#10141c;background:#7aa2ff;border:none;border-radius:8px}
            </style></head><body><div class="card">
              <h1>Lunitide</h1>
              <p>扫描电脑端「设置 → 远程访问」里的二维码，即可连接你的电脑。</p>
              <button class="scan-btn" onclick="location.href='lunitide-shell://scan'">扫码配对</button>
              <p>配对完成后在家走 Wi-Fi、在外走蜂窝流量，自动切换直连。</p>
              </div></body></html>
        """.trimIndent()
        web.loadDataWithBaseURL(null, html, "text/html", "utf-8", null)
    }

    /** 所有候选地址都连不上：渲染原生错误页（重扫码/重试出口），
     *  不再停留在 WebView 系统错误页或白屏上。 */
    private fun renderConnectError() {
        val tried = candidates.size.coerceAtLeast(1)
        val html = """
            <!doctype html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width,initial-scale=1">
            <style>
              body{font-family:system-ui,sans-serif;background:#10141c;color:#e8ecf4;
                   display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
              .card{max-width:320px;padding:32px 24px;text-align:center;line-height:1.7}
              h1{font-size:20px;margin:0 0 12px}
              p{font-size:14px;color:#9aa7bd;margin:0 0 8px}
              .btn{display:block;margin:10px auto;padding:12px 0;width:220px;font-size:16px;
                   color:#10141c;background:#7aa2ff;border:none;border-radius:8px}
              .btn.ghost{background:transparent;color:#7aa2ff;border:1px solid #7aa2ff}
            </style></head><body><div class="card">
              <h1>连不上电脑</h1>
              <p>已尝试全部 $tried 个已知地址，均无法到达。</p>
              <p>· 确认电脑端「设置 → 远程访问」已开启<br>
                 · 手机与电脑连同一 Wi-Fi 后重试<br>
                 · 电脑网络支持公网 IPv6 时，手机流量也可直连</p>
              <button class="btn" onclick="location.href='lunitide-shell://scan'">重新扫码配对</button>
              <button class="btn ghost" onclick="location.href='lunitide-shell://retry'">重试连接</button>
              </div></body></html>
        """.trimIndent()
        web.loadDataWithBaseURL(null, html, "text/html", "utf-8", null)
    }

    /** APP 内扫码配对：扫码结果即配对 URL，复用 https 深链接分支在壳内完成配对。 */
    private fun startScan() {
        IntentIntegrator(this)
            .setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
            .setPrompt("对准电脑端「设置 → 远程访问」里的二维码")
            .setBeepEnabled(false)
            .initiateScan()
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        val result: IntentResult? =
            IntentIntegrator.parseActivityResult(requestCode, resultCode, data)
        val contents = result?.contents
        if (!contents.isNullOrEmpty()) {
            handleIntent(Intent(Intent.ACTION_VIEW, Uri.parse(contents)))
            return
        }
        super.onActivityResult(requestCode, resultCode, data)
    }

    private fun loadWithFailover(url: String) {
        lastUrl = url
        web.loadUrl(url)
        scheduleTimeout()
    }

    private fun scheduleTimeout() {
        cancelTimeout()
        val task = Runnable { failover() }
        pendingTimeout = task
        handler.postDelayed(task, LOAD_TIMEOUT_MS)
    }

    private fun cancelTimeout() {
        pendingTimeout?.let { handler.removeCallbacks(it) }
        pendingTimeout = null
    }

    /** 当前 origin 连不上：优先切到已探明可达的候选，其次按原顺序轮换；全灭进错误页。 */
    private fun failover() {
        val rest = (candidateIdx + 1) until candidates.size
        val next = rest.firstOrNull { probeResults[candidates[it]] == true }
            ?: rest.firstOrNull { !probeResults.containsKey(candidates[it]) }
        if (next != null) {
            loadCandidate(next)
        } else {
            runOnUiThread { renderConnectError() }
        }
    }

    private fun originOfUrl(url: String?): String? =
        url?.let { Regex("^https://[^/]+").find(it)?.value }

    private fun loadCandidate(idx: Int) {
        candidateIdx = idx
        val origin = candidates[idx]
        val target = lastUrl?.replace(Regex("^https://[^/]+"), origin) ?: "$origin/"
        loadWithFailover(target)
    }

    /** 主页面开始加载时，对其余候选并行做可达性探测（蜂窝下不被失效地址串行拖慢 8s/个）。
     *  探测只测链路通不通：信任所有证书，真实性仍由 WebView 的指纹校验把关。 */
    private fun startProbes(loadedUrl: String) {
        val loaded = originOfUrl(loadedUrl) ?: return
        if (loaded == lastProbedOrigin) return
        lastProbedOrigin = loaded
        probeGen++
        val gen = probeGen
        probeResults.clear()
        candidates.forEach { origin ->
            if (origin == loaded) return@forEach
            Thread {
                val ok = runCatching {
                    val conn = URL("$origin/").openConnection() as HttpsURLConnection
                    conn.connectTimeout = PROBE_TIMEOUT_MS.toInt()
                    conn.readTimeout = PROBE_TIMEOUT_MS.toInt()
                    conn.instanceFollowRedirects = false
                    conn.sslSocketFactory = trustAllFactory
                    conn.hostnameVerifier = HostnameVerifier { _, _ -> true }
                    conn.connect()
                    conn.responseCode >= 0
                }.getOrDefault(false)
                if (gen == probeGen && !isDestroyed) {
                    runOnUiThread {
                        if (gen != probeGen) return@runOnUiThread
                        probeResults[origin] = ok
                        // 当前页仍卡着（8s 未到）而某候选已探明可达：立刻切换，不等超时。
                        if (ok && pendingTimeout != null) {
                            val idx = candidates.indexOf(origin)
                            if (idx > candidateIdx) {
                                probeGen++
                                cancelTimeout()
                                loadCandidate(idx)
                            }
                        }
                    }
                }
            }.start()
        }
    }

    private fun gatewayClient() = object : WebViewClient() {
        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
            val uri = request.url
            return when (uri.scheme) {
                "http", "https" -> {
                    // 网关内的地址在壳内打开；站外链接交给系统浏览器。
                    val current = Uri.parse(lastUrl ?: return false)
                    if (uri.host == current.host && uri.port == current.port) false
                    else {
                        startActivity(Intent(Intent.ACTION_VIEW, uri))
                        true
                    }
                }
                "lunitide" -> true // 壳内不需要唤起自己
                "lunitide-shell" -> {
                    // 欢迎页/连接错误页按钮：scan 进原生扫码页；retry 从头轮换候选重连。
                    when (uri.host) {
                        "scan" -> startScan()
                        "retry" -> loadSavedOrEmpty()
                    }
                    true
                }
                else -> false
            }
        }

        override fun onPageStarted(view: WebView, url: String, favicon: android.graphics.Bitmap?) {
            if (url.startsWith("https://")) {
                scheduleTimeout()
                startProbes(url)
            }
        }

        override fun onPageFinished(view: WebView, url: String) {
            if (url.startsWith("https://")) cancelTimeout()
            maybeInjectToken(view, url)
        }

        /** 浏览器侧配对完成的凭据转移：注入前端约定的 localStorage 键后重载一次。 */
        private fun maybeInjectToken(view: WebView, url: String) {
            val token = pendingToken ?: return
            val origin = prefs.getString(KEY_ORIGIN, null) ?: return
            if (!url.startsWith("$origin/")) return
            pendingToken = null
            // IPv6 host 在 URL 里必须带方括号（Uri.host 返回裸地址）。
            val parsed = Uri.parse(origin)
            val host = parsed.host ?: return
            val bracketed = if (host.contains(':')) "[$host]" else host
            val port = if (parsed.port > 0) parsed.port else 443
            val credentials = JSONObject()
                .put("wsUrl", "wss://$bracketed:$port/bridge")
                .put("token", token)
                .toString()
            view.evaluateJavascript(
                "try{localStorage.setItem('lunitide:remote-bridge', $credentials)}catch(e){}"
            ) { view.reload() }
        }

        override fun onReceivedError(
            view: WebView,
            request: WebResourceRequest,
            error: WebResourceError
        ) {
            if (request.isForMainFrame && request.url.toString() == lastUrl) {
                cancelTimeout()
                failover()
            }
        }

        override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
            val presented = error.certificate.x509Certificate?.let { fingerprintOf(it) }
            val expected = expectedFingerprint()
            when {
                presented != null && expected != null && presented == expected -> {
                    handler.proceed()
                    if (prefs.getString(KEY_FP, null) == null) persistFingerprint(presented)
                }
                expected == null && presented != null -> confirmFingerprint(presented, handler)
                else -> {
                    handler.cancel()
                    runOnUiThread {
                        Toast.makeText(
                            this@MainActivity,
                            "安全证书校验失败：连接已拒绝，请重新扫码配对",
                            Toast.LENGTH_LONG
                        ).show()
                    }
                }
            }
        }
    }

    private fun expectedFingerprint(): String? {
        lastUrl?.let { fragmentParam(Uri.parse(it), "fp") }?.let { return it.lowercase() }
        return prefs.getString(KEY_FP, null)?.lowercase()
    }

    private fun persistFingerprint(fp: String) {
        prefs.edit().putString(KEY_FP, fp).apply()
    }

    /** 无任何锚点（TOFU 首次）：原生对话框展示指纹，用户与电脑端比对后决定。 */
    private fun confirmFingerprint(presented: String, handler: SslErrorHandler) {
        runOnUiThread {
            AlertDialog.Builder(this)
                .setTitle("验证电脑身份")
                .setMessage(
                    "连接的电脑证书指纹为：\n\n$presented\n\n" +
                        "请与电脑端「设置 → 远程访问 → 身份指纹」核对一致后继续。"
                )
                .setPositiveButton("一致，继续") { _, _ ->
                    persistFingerprint(presented)
                    handler.proceed()
                }
                .setNegativeButton("取消") { _, _ -> handler.cancel() }
                .setOnCancelListener { handler.cancel() }
                .show()
        }
    }

    private fun fingerprintOf(cert: java.security.cert.X509Certificate): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(cert.encoded)
        return digest.joinToString("") { "%02x".format(it) }.take(16)
    }

    /** 网页（壳内）配对成功后回传网关信息：origin/fingerprint/addresses 全量候选。 */
    inner class ShellBridge {
        @JavascriptInterface
        fun saveGateway(json: String) {
            runCatching {
                val obj = JSONObject(json)
                val origin = obj.optString("origin")
                val fp = obj.optString("fingerprint")
                val addresses = obj.optJSONArray("addresses")?.let { arr ->
                    (0 until arr.length()).mapNotNull { arr.optString(it).ifEmpty { null } }
                }
                if (origin.isNotEmpty()) {
                    this@MainActivity.saveGateway(origin, fp.ifEmpty { null }, addresses)
                }
            }
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        if (web.canGoBack()) web.goBack()
        else {
            val now = System.currentTimeMillis()
            if (now - backPressedAt < EXIT_WINDOW_MS) finish()
            else {
                backPressedAt = now
                Toast.makeText(this, "再按一次退出 Lunitide", Toast.LENGTH_SHORT).show()
            }
        }
    }

    override fun onDestroy() {
        cancelTimeout()
        web.destroy()
        super.onDestroy()
    }

    private val versionName: String
        get() = packageManager.getPackageInfo(packageName, 0).versionName ?: "?"
}
