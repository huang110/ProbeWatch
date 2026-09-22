from pathlib import Path
import re


ROOT = Path(__file__).parents[1]
FRONTEND_SRC = ROOT / "src"
# The console is split across src/main.jsx, src/App.jsx, src/components/ and
# src/lib/; the security contract runs against every source file joined
# together so the guarantees hold no matter how the UI is organised.
SOURCE_FILES = sorted(
    [path for path in FRONTEND_SRC.rglob("*.jsx") if path.is_file()]
    + [path for path in FRONTEND_SRC.rglob("*.js") if path.is_file()]
)
assert SOURCE_FILES, "frontend src must contain jsx/js source files"
SOURCE = "\n".join(path.read_text(encoding="utf-8") for path in SOURCE_FILES)
STYLES = (FRONTEND_SRC / "styles.css").read_text(encoding="utf-8")
GUEST_VIEW = (FRONTEND_SRC / "components" / "GuestView.jsx").read_text(encoding="utf-8")
INDEX = (ROOT / "index.html").read_text(encoding="utf-8")
VITE_CONFIG = (ROOT / "vite.config.js").read_text(encoding="utf-8")
SECURITY_DOC = (ROOT / "SECURITY.md").read_text(encoding="utf-8")
OPENRESTY_CONFIG = ROOT / "deploy" / "openresty-security.conf"
GITIGNORE = (ROOT.parent / ".gitignore").read_text(encoding="utf-8")
API_DIR = ROOT.parent / "internal" / "api"
PUBLIC_HANDLERS = API_DIR / "public_handlers.go"

# IPv4 literals that are safe to appear in demo data: loopback, private,
# link-local, multicast, and the RFC 5737 documentation ranges.
RESERVED_IPV4 = re.compile(
    r"^(0\.|127\.|10\.|192\.168\.|192\.0\.2\.|198\.51\.100\.|203\.0\.113\.|"
    r"169\.254\.|224\.|240\.|255\.|172\.(1[6-9]|2[0-9]|3[01])\.)"
)


def test_demo_frontend_does_not_ship_real_node_ips():
    for literal in re.findall(r"\b\d{1,3}(?:\.\d{1,3}){3}\b", SOURCE):
        assert RESERVED_IPV4.match(literal), (
            f"frontend demo data contains a non-reserved IPv4 literal: {literal}; "
            "use RFC 5737 documentation addresses (192.0.2.x / 198.51.100.x / 203.0.113.x) instead"
        )


def test_frontend_has_no_external_font_import():
    assert "fonts.googleapis.com" not in STYLES


def test_frontend_does_not_claim_live_sync_without_an_api_client():
    assert "数据同步正常" not in SOURCE


def test_static_entry_has_a_content_security_policy_fallback():
    assert "Content-Security-Policy" in INDEX


def test_vite_preview_configures_security_headers():
    for header in ("Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"):
        assert header in VITE_CONFIG


def test_pytest_cache_is_ignored():
    assert ".pytest_cache/" in GITIGNORE


def test_security_document_references_frontend_openresty_config():
    assert OPENRESTY_CONFIG.exists()
    assert "frontend/deploy/openresty-security.conf" in SECURITY_DOC
    assert "deploy/openresty-security.conf" not in SECURITY_DOC.replace("frontend/deploy/openresty-security.conf", "")


def test_https_homepage_security_headers_cover_browser_baseline():
    required_headers = {
        "Content-Security-Policy": "default-src 'self'",
        "X-Frame-Options": "DENY",
        "X-Content-Type-Options": "nosniff",
        "Referrer-Policy": "strict-origin-when-cross-origin",
        "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
        "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
    }
    config = OPENRESTY_CONFIG.read_text(encoding="utf-8")
    assert 'add_header Content-Security-Policy "' in config
    assert "default-src 'self'" in config
    for header, value in {
        "X-Frame-Options": "DENY",
        "X-Content-Type-Options": "nosniff",
        "Referrer-Policy": "strict-origin-when-cross-origin",
        "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
        "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
    }.items():
        assert f"add_header {header} \"{value}\" always;" in config


def test_openresty_security_snippet_keeps_api_proxy_guidance():
    config = OPENRESTY_CONFIG.read_text(encoding="utf-8")
    assert "API authorization" in config
    assert "HTTPS server block" in config


def test_overview_uses_authenticated_api_and_history_without_browser_storage():
    assert "fetch('/api/overview'" in SOURCE
    assert '/resource/history' in SOURCE
    assert 'localStorage' not in SOURCE
    assert 'sessionStorage' not in SOURCE


def test_overview_has_no_fake_sparkline_defaults_or_claims():
    assert 'points = [28, 36, 31, 44, 39, 52, 47, 62]' not in SOURCE
    assert '暂无历史数据' in SOURCE
    assert '暂无 API 数据' in SOURCE


def test_alerts_use_authenticated_api_and_ephemeral_csrf_header():
    assert "fetch('/api/alerts?status=open,acked'" in SOURCE
    assert "fetch('/api/csrf'" in SOURCE
    assert "credentials: 'same-origin'" in SOURCE
    assert "X-CSRF-Token" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_alerts_handle_security_and_conflict_failures():
    for status in ('401', '403', '409'):
        assert status in SOURCE
    assert '确认告警失败' in SOURCE


def test_public_status_endpoint_is_registered_without_authentication():
    server = (API_DIR / "server.go").read_text(encoding="utf-8")
    registrations = [line.strip() for line in server.splitlines() if "/api/public/status" in line]
    assert registrations, "server.go must register /api/public/status"
    assert all("RequireAuth" not in line for line in registrations), "public status must stay unauthenticated"


def test_public_status_response_is_a_sanitized_allow_list():
    handlers = PUBLIC_HANDLERS.read_text(encoding="utf-8")
    assert "publicStatusResponse" in handlers
    tags = set(re.findall(r'json:"([^"]+)"', handlers))
    assert {"nodes", "online", "total", "names", "checks", "success_rate", "avg_latency_ms", "last_updated_at", "generated_at"} <= tags
    forbidden = ("uuid", "node_id", "target_id", "detector_id", "token", "resource", "ip", "alert", "reason", "severity", "hostname")
    for tag in tags:
        for field in forbidden:
            assert field not in tag, f"public status json tag {tag!r} leaks field {field!r}"
    assert "ListAlerts" not in handlers
    assert "NodeToken" not in handlers


def test_public_status_sanitizes_node_names_before_publishing():
    handlers = PUBLIC_HANDLERS.read_text(encoding="utf-8")
    assert "sanitizeNodeName" in handlers
    assert "ReplaceAllString" in handlers
    assert "[已脱敏]" in handlers


def test_frontend_never_injects_raw_html():
    assert "dangerouslySetInnerHTML" not in SOURCE
    assert "innerHTML" not in SOURCE
    assert "insertAdjacentHTML" not in SOURCE
    assert "document.write" not in SOURCE


def test_node_detail_fetches_checks_summary_and_traffic_analytics():
    assert "`/api/nodes/${encodeURIComponent(analyticsUuid)}/checks/summary`" in SOURCE
    assert "`/api/nodes/${encodeURIComponent(analyticsUuid)}/traffic?period=${encodeURIComponent(trafficPeriod)}`" in SOURCE
    assert SOURCE.count("credentials: 'same-origin'") >= 3
    assert "new AbortController" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_node_analytics_failures_render_inline_empty_state_without_popups():
    assert "暂无数据" in SOURCE
    assert "alert(" not in SOURCE
    assert "window.alert" not in SOURCE


def test_media_matrix_uses_authenticated_node_media_api():
    assert "`/api/nodes/${encodeURIComponent(target.uuid)}/media`" in SOURCE
    assert "credentials: 'same-origin'" in SOURCE
    assert "new AbortController" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_media_matrix_renders_statuses_and_empty_states_defensively():
    assert "暂无流媒体上报" in SOURCE
    assert "暂无上报" in SOURCE
    assert "mediaStatusTone" in SOURCE
    for tone in ("available", "unavailable", "warning", "muted"):
        assert f"media-cell-{tone}" in STYLES, f"styles.css must style media-cell-{tone}"
    assert "dangerouslySetInnerHTML" not in SOURCE
    assert "innerHTML" not in SOURCE
    assert "alert(" not in SOURCE


def test_guest_view_uses_public_status_without_storage():
    assert "fetch('/api/public/status'" in SOURCE
    assert "GuestView" in SOURCE
    assert "服务状态" in SOURCE
    assert "'/auth/github'" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_guest_view_does_not_call_management_apis():
    for management_call in ("/api/nodes", "/api/alerts", "/api/overview", "/api/targets", "/api/csrf", "/checks/summary", "/traffic?period="):
        assert management_call not in GUEST_VIEW, f"guest view must not call {management_call}"


def test_security_document_describes_public_status_boundary():
    assert "/api/public/status" in SECURITY_DOC
    assert "脱敏" in SECURITY_DOC
    assert "游客" in SECURITY_DOC


def test_settings_card_uses_authenticated_totp_apis_with_csrf():
    assert "fetch('/api/totp/setup'" in SOURCE
    assert "`/api/totp/${action}`" in SOURCE
    assert "fetch('/api/csrf'" in SOURCE
    assert "'X-CSRF-Token': csrfToken" in SOURCE
    assert "one-time-code" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_totp_login_page_posts_pending_credential_to_verify():
    assert "'/auth/totp/verify'" in SOURCE
    assert "window.location.pathname === '/login/2fa'" in SOURCE
    assert "TOTPVerifyPage" in SOURCE
    # The verification POST must not carry any browser-stored token.
    assert "method: 'POST'" in SOURCE
    assert "credentials: 'same-origin'" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_totp_setup_secret_is_not_rendered_as_html_or_copied_to_clipboard():
    assert "dangerouslySetInnerHTML" not in SOURCE
    assert "clipboard" not in SOURCE.lower()
    assert "totp-secret" in SOURCE


# ---- komari/Lite 风格重构（分栏面板）后的结构约束 ----


def test_styles_define_design_tokens_and_reduced_motion():
    assert ":root" in STYLES
    for token in ("--bg-base", "--bg-panel", "--bg-raised", "--border", "--mint", "--amber", "--rose", "--text-1", "--text-2", "--text-3"):
        assert token in STYLES, f"styles.css must define design token {token}"
    assert "prefers-reduced-motion" in STYLES


def test_topbar_has_clock_auto_refresh_interval_and_last_sync():
    for marker in ("topbar-clock", "自动刷新", "refresh-interval", "最后同步", "REFRESH_OPTIONS = [10, 30, 60]"):
        assert marker in SOURCE, f"topbar must expose {marker}"
    assert "aria-pressed" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_resource_and_history_polling_are_independent_and_pause_when_hidden():
    assert "OVERVIEW_INTERVAL_MS = 300000" in SOURCE
    assert "refreshInterval * 1000" in SOURCE
    assert "visibilitychange" in SOURCE
    assert "document.visibilityState" in SOURCE


def test_overview_renders_six_stat_cards_with_sparklines():
    assert "StatCard" in SOURCE
    for label in ("节点在线", "平均延迟", "检测通过率", "今日流量", "CPU 平均", "内存平均"):
        assert label in SOURCE, f"overview stat card {label} missing"
    assert "stat-grid" in STYLES
    assert "stat-card" in STYLES
    assert "sparkline" in STYLES


def test_overview_splits_node_table_and_side_rankings():
    assert "overview-columns" in STYLES
    assert "NodeTable" in SOURCE
    assert "RankCard" in SOURCE
    for rank in ("CPU 占用 Top5", "内存占用 Top5", "丢包 Top5", "最近告警"):
        assert rank in SOURCE, f"side column ranking {rank} missing"


def test_node_table_columns_cover_resources_network_loss_and_heartbeat():
    for column in ("状态", "节点", "区域", "CPU", "内存", "磁盘", "网络 ↓/↑", "丢包率", "最后心跳"):
        assert column in SOURCE, f"node table column {column} missing"
    assert "node-table" in STYLES
    assert "formatRate" in SOURCE


def test_node_table_degrades_to_cards_on_mobile():
    assert "node-card" in STYLES
    assert "768px" in STYLES
    assert ".node-table-wrap { display: none; }" in STYLES
    assert ".node-card-list { display: none; }" in STYLES


def test_sidebar_supports_collapse_and_mobile_drawer():
    assert "sidebar-collapsed" in SOURCE
    assert "sidebar-collapsed" in STYLES
    assert "sidebar-open" in STYLES
    assert "sidebar-backdrop" in STYLES
    assert "折叠侧边栏" in SOURCE
    assert "打开导航菜单" in SOURCE


def test_guest_view_renders_public_big_screen_status():
    assert "全部正常" in GUEST_VIEW
    assert "部分异常" in GUEST_VIEW
    assert "guest-stats-bar" in STYLES
    assert "guest-node-grid" in STYLES
    assert "guest-badge" in STYLES
    # 公开页不展示单节点状态/在线时长等明细
    assert "状态未公开" in GUEST_VIEW


def test_node_detail_renders_gauges_history_chart_and_identity():
    assert "RingGauge" in SOURCE
    assert "DualLineChart" in SOURCE
    assert "detail-identity" in STYLES
    assert "ring-gauge" in STYLES
    assert "dual-chart" in STYLES
    for label in ("操作系统", "内核", "架构", "Agent 版本", "开始时间"):
        assert label in SOURCE, f"node info row {label} missing"


def test_drawer_stays_a_summary_dialog_without_full_analytics():
    drawer = (FRONTEND_SRC / "components" / "NodeDrawer.jsx").read_text(encoding="utf-8")
    assert 'role="dialog"' in drawer
    assert "打开完整详情" in drawer
    for management_call in ("/api/nodes", "/checks/summary", "/traffic?period=", "/resource/history"):
        assert management_call not in drawer, f"summary drawer must not fetch {management_call}"
