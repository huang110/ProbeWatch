from pathlib import Path
import re


ROOT = Path(__file__).parents[1]
SOURCE = (ROOT / "src" / "main.jsx").read_text(encoding="utf-8")
STYLES = (ROOT / "src" / "styles.css").read_text(encoding="utf-8")
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


def test_guest_view_uses_public_status_without_storage():
    assert "fetch('/api/public/status'" in SOURCE
    assert "GuestView" in SOURCE
    assert "服务状态" in SOURCE
    assert "'/auth/github'" in SOURCE
    assert "localStorage" not in SOURCE
    assert "sessionStorage" not in SOURCE


def test_guest_view_does_not_call_management_apis():
    guest_section = SOURCE[SOURCE.index("function GuestView"):SOURCE.index("function App")]
    for management_call in ("/api/nodes", "/api/alerts", "/api/overview", "/api/targets", "/api/csrf", "/checks/summary", "/traffic?period="):
        assert management_call not in guest_section, f"guest view must not call {management_call}"


def test_security_document_describes_public_status_boundary():
    assert "/api/public/status" in SECURITY_DOC
    assert "脱敏" in SECURITY_DOC
    assert "游客" in SECURITY_DOC
