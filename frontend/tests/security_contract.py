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


def test_overview_uses_authenticated_api_and_history_without_browser_storage():
    assert "fetch('/api/overview'" in SOURCE
    assert '/resource/history' in SOURCE
    assert 'localStorage' not in SOURCE
    assert 'sessionStorage' not in SOURCE
    assert 'token' not in SOURCE.lower()


def test_overview_has_no_fake_sparkline_defaults_or_claims():
    assert 'points = [28, 36, 31, 44, 39, 52, 47, 62]' not in SOURCE
    assert '暂无历史数据' in SOURCE
    assert '暂无 API 数据' in SOURCE
