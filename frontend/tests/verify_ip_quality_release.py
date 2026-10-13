import os
import sys
import time
from playwright.sync_api import sync_playwright

BASE_URL = "https://tz.115yu.us.ci"
NODE_UUID = "bdf9fac6-66ff-4585-9e1e-1cc20c4ac3c8"
ARTIFACT_DIR = r"C:\Users\黄\.gemini\antigravity\brain\8376b092-4bfc-49a6-b17c-29bb89f3558a"

VIEWPORTS = [
    {"name": "desktop_1920", "width": 1920, "height": 1080},
    {"name": "desktop_1440", "width": 1440, "height": 900},
    {"name": "desktop_1280", "width": 1280, "height": 900},
    {"name": "tablet_1024", "width": 1024, "height": 768},
    {"name": "tablet_768", "width": 768, "height": 1024},
    {"name": "mobile_375", "width": 375, "height": 812},
    {"name": "mobile_320", "width": 320, "height": 680},
]

def run():
    print(f"Starting IP Quality release verification against {BASE_URL}")
    os.makedirs(ARTIFACT_DIR, exist_ok=True)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        
        # Test 1: Verify Live Production across all viewports
        for vp in VIEWPORTS:
            tag = vp["name"]
            w = vp["width"]
            h = vp["height"]
            print(f"\n==========================================")
            print(f"Testing Viewport: {tag} ({w}x{h})")
            print(f"==========================================")

            context = browser.new_context(
                viewport={"width": w, "height": h},
                color_scheme="dark"
            )
            page = context.new_page()

            errors = []
            page.on("pageerror", lambda err: errors.append(str(err)))

            # Navigate to node detail page
            detail_url = f"{BASE_URL}/#/node-detail?uuid={NODE_UUID}"
            page.goto(detail_url)
            
            # Wait for IP quality card selector
            page.wait_for_selector(".komari-ip-quality-card", timeout=30000)
            card = page.locator(".komari-ip-quality-card")

            # Wait for summary grid to load
            page.wait_for_selector(".ip-quality-summary-grid", timeout=15000)

            # Check overflow
            scroll_w = page.evaluate("document.documentElement.scrollWidth")
            client_w = page.evaluate("document.documentElement.clientWidth")
            print(f"[{tag}] Overflow check: scrollWidth={scroll_w}, clientWidth={client_w}")
            assert scroll_w <= client_w + 1, f"Horizontal overflow: {scroll_w} > {client_w}"

            # Check tabs
            tabs_locator = card.locator(".ip-family-tabs")
            assert tabs_locator.count() > 0, "Must render .ip-family-tabs"
            v4_tab = card.locator(".ip-family-tab").filter(has_text="IPv4")
            v6_tab = card.locator(".ip-family-tab").filter(has_text="IPv6")
            assert v4_tab.count() > 0, "Must have IPv4 tab"
            assert v6_tab.count() > 0, "Must have IPv6 tab"

            # Check Summary Grid
            summary = card.locator(".ip-quality-summary-grid")
            assert summary.count() > 0, "Must render .ip-quality-summary-grid"
            summary_text = summary.inner_text().replace("\n", " | ")
            print(f"[{tag}] IPv4 Summary: {summary_text}")
            assert "综合风险" in summary_text
            assert "IP 类型" in summary_text
            assert "地区" in summary_text
            assert "ASN" in summary_text

            # Check 3 columns
            score_list = card.locator(".ip-quality-score-list")
            factors_grid = card.locator(".ip-quality-factors-grid")
            media_grid = card.locator(".ip-quality-media-grid")
            assert score_list.count() > 0, "Must render .ip-quality-score-list"
            assert factors_grid.count() > 0, "Must render .ip-quality-factors-grid"
            assert media_grid.count() > 0, "Must render .ip-quality-media-grid"

            # Check score list items
            score_items = score_list.locator(".ip-quality-score-row").all_inner_texts()
            print(f"[{tag}] Score items: {', '.join(score_items)}")
            assert any("IP2Location" in s for s in score_items)
            assert any("Scamalytics" in s for s in score_items)
            assert any("AbuseIPDB" in s for s in score_items)

            # Check media items
            media_items = media_grid.locator(".ip-quality-media-row").all_inner_texts()
            print(f"[{tag}] Media items count: {len(media_items)}")
            assert any("Netflix" in m for m in media_items)
            assert any("YouTube" in m for m in media_items)
            assert any("ChatGPT" in m for m in media_items)

            # Screenshot IPv4
            v4_shot = os.path.join(ARTIFACT_DIR, f"ipqa_live_{tag}_v4.png")
            card.screenshot(path=v4_shot)
            print(f"[{tag}] Saved IPv4 screenshot: {v4_shot}")

            # Click IPv6 tab
            v6_tab.click()
            page.wait_for_timeout(600)

            # Check IPv6 summary
            v6_summary_text = summary.inner_text().replace("\n", " | ")
            print(f"[{tag}] IPv6 Summary: {v6_summary_text}")
            # Hong Kong should be in IPv6 for ciallonet node
            assert "香港" in v6_summary_text or "HK" in v6_summary_text or "AS202355" in v6_summary_text

            # Screenshot IPv6
            v6_shot = os.path.join(ARTIFACT_DIR, f"ipqa_live_{tag}_v6.png")
            card.screenshot(path=v6_shot)
            print(f"[{tag}] Saved IPv6 screenshot: {v6_shot}")

            # Check light theme for 1280 and 375
            if tag in ["desktop_1280", "mobile_375"]:
                page.evaluate("document.documentElement.setAttribute('data-theme', 'light')")
                page.wait_for_timeout(300)
                light_shot = os.path.join(ARTIFACT_DIR, f"ipqa_live_{tag}_light.png")
                card.screenshot(path=light_shot)
                print(f"[{tag}] Saved light theme screenshot: {light_shot}")

            context.close()

        # Test 2: Songzhong State Verification
        # Inject Songzhong mock scenario into page route to verify the badges and styling
        print(f"\n==========================================")
        print("Testing Songzhong State Verification")
        print("==========================================")
        context = browser.new_context(viewport={"width": 1280, "height": 900}, color_scheme="dark")
        page = context.new_page()

        # Mock public IP quality endpoint with Songzhong data
        def handle_ipqa(route):
            mock_data = {
                "ip_quality": {
                    "has_ipv4": True,
                    "has_ipv6": True,
                    "last_checked_at": "2026-10-13 21:05:00",
                    "ipv4": {
                        "risk_score": 0,
                        "risk_level": "low",
                        "ip_type": "原生IP",
                        "country": "TW",
                        "region": "台湾",
                        "asn": "AS3462",
                        "organization": "Data Communication Business Group",
                        "proxy": False,
                        "vpn": False,
                        "tor": False,
                        "abuse": False,
                        "source_scores": {
                            "IP2Location": 0,
                            "Scamalytics": 0,
                            "ipapi": 0,
                            "AbuseIPDB": 0,
                            "IPQS": 0,
                            "DB-IP": 0
                        },
                        "media": [
                            {"name": "Google 定位", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "Netflix", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "Disney+", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "YouTube", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "Prime Video", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "TikTok", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "Reddit", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"},
                            {"name": "ChatGPT", "status": "available", "status_raw": "解锁", "region": "TW", "type": "原生"}
                        ]
                    },
                    "ipv6": {
                        "risk_score": 0.5,
                        "risk_level": "low",
                        "ip_type": "原生IP",
                        "country": "HK",
                        "region": "香港",
                        "asn": "AS202355",
                        "organization": "Ciallo Networks LTD",
                        "proxy": False,
                        "vpn": False,
                        "tor": False,
                        "abuse": False,
                        "source_scores": {
                            "IP2Location": 3,
                            "Scamalytics": 0,
                            "ipapi": 0,
                            "AbuseIPDB": 0,
                            "IPQS": 0,
                            "DB-IP": 0
                        },
                        "media": [
                            {"name": "Google 定位", "status": "songzhong", "status_raw": "送中 · CN", "region": "CN", "type": "送中"},
                            {"name": "Netflix", "status": "available", "status_raw": "仅自制", "region": "HK", "type": "原生"},
                            {"name": "Disney+", "status": "available", "status_raw": "解锁", "region": "HK", "type": "原生"},
                            {"name": "YouTube", "status": "songzhong", "status_raw": "送中 · CN", "region": "CN", "type": "送中"},
                            {"name": "Prime Video", "status": "blocked", "status_raw": "不可用"},
                            {"name": "TikTok", "status": "blocked", "status_raw": "不可用"},
                            {"name": "Reddit", "status": "available", "status_raw": "解锁", "region": "HK", "type": "原生"},
                            {"name": "ChatGPT", "status": "available", "status_raw": "仅APP", "region": "HK", "type": "原生"}
                        ]
                    }
                }
            }
            route.fulfill(json=mock_data)

        page.route(f"**/api/public/nodes/{NODE_UUID}/ip-quality*", handle_ipqa)
        page.goto(f"{BASE_URL}/#/node-detail?uuid={NODE_UUID}")
        page.wait_for_selector(".komari-ip-quality-card", timeout=30000)
        card = page.locator(".komari-ip-quality-card")

        # Tab should have "IPv6 送中" badge
        v6_badge = card.locator(".tab-songzhong-badge")
        assert v6_badge.count() > 0, "Must render .tab-songzhong-badge"
        print("PASS: Found .tab-songzhong-badge on IPv6 tab")

        # Click IPv6 to view Songzhong status
        card.locator(".ip-family-tab").filter(has_text="IPv6").click()
        page.wait_for_timeout(600)

        # Summary should have risk songzhong badge
        risk_sz = card.locator(".risk-songzhong-badge")
        assert risk_sz.count() > 0, "Must render .risk-songzhong-badge in summary"
        print(f"PASS: Found .risk-songzhong-badge: {risk_sz.inner_text()}")

        # Media grid should have songzhong status badges on Google and YouTube
        songzhong_badges = card.locator(".status-songzhong").all_inner_texts()
        print(f"PASS: Found .status-songzhong badges: {songzhong_badges}")
        assert any("送中" in b for b in songzhong_badges)

        sz_shot = os.path.join(ARTIFACT_DIR, "ipqa_verified_songzhong_1280.png")
        card.screenshot(path=sz_shot)
        print(f"Saved Songzhong verification screenshot: {sz_shot}")

        # Also mobile 375 for songzhong
        page.set_viewport_size({"width": 375, "height": 812})
        page.wait_for_timeout(500)
        sz_mob_shot = os.path.join(ARTIFACT_DIR, "ipqa_verified_songzhong_375.png")
        card.screenshot(path=sz_mob_shot)
        print(f"Saved Songzhong mobile screenshot: {sz_mob_shot}")

        context.close()
        browser.close()

    print("\nALL VERIFICATIONS PASSED SUCCESSFULLY!")

if __name__ == "__main__":
    run()
