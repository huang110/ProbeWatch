import json
import os

from playwright.sync_api import sync_playwright


BASE_URL = os.environ.get("PROBEWATCH_BASE_URL", "http://127.0.0.1:4173/")
CHROMIUM_PATH = os.environ.get("PROBEWATCH_CHROMIUM")


NODE = {
    "id": "node-1",
    "uuid": "uuid-1",
    "name": "测试节点",
    "status": "online",
    "last_reported_at": "2026-09-20T00:00:00Z",
    "resource": {"cpu_percent": 12.5, "memory_total_bytes": 100, "memory_used_bytes": 50},
}
NODE_TWO = {**NODE, "id": "node-2", "uuid": "uuid-2", "name": "备用节点"}


def install_api(page, *, guest=False, fail_overview=False):
    public = {
        "nodes": {"online": 1, "total": 1, "names": ["测试节点"]},
        "checks": {"success_rate": 100, "avg_latency_ms": 12.5},
        "last_updated_at": "2026-09-20T00:00:00Z",
        "generated_at": "2026-09-20T00:00:01Z",
    }

    request_urls = []
    page._probewatch_request_urls = request_urls

    def route(request):
        url = request.url
        request_urls.append(url)
        if "/api/me" in url:
            return {"status": 401 if guest else 200, "body": "{}" if guest else json.dumps({"login": "admin", "role": "admin"})}
        if "/api/public/status" in url:
            return {"status": 200, "body": json.dumps(public)}
        if url.endswith("/api/nodes"):
            return {"status": 200, "body": json.dumps([] if guest else [NODE, NODE_TWO])}
        if "/api/alerts" in url:
            return {"status": 200, "body": "[]"}
        if "/api/overview" in url:
            if fail_overview:
                return {"status": 503, "body": json.dumps({"error": "temporary unavailable"})}
            return {"status": 200, "body": json.dumps({"nodes": {"online": 1, "total": 1}, "checks": {"success_rate": 100, "avg_latency_ms": 12.5}, "resources": {}})}
        if "/api/version" in url:
            return {"status": 200, "body": json.dumps({"version": "0.8.64"})}
        if "/api/csrf" in url:
            return {"status": 200, "headers": {"X-CSRF-Token": "test-csrf"}, "body": "{}"}
        if "/api/targets" in url:
            return {"status": 200, "body": "[]"}
        if "/api/registration-tokens" in url:
            return {"status": 200, "body": "[]"}
        if "/api/nodes/" in url:
            if "/checks/summary" in url or "/network" in url or "/traffic" in url or "/media" in url or "/mtr" in url:
                return {"status": 200, "body": "[]"}
            return {"status": 200, "body": "{}"}
        if "/api/admin/terminal" in url:
            return {"status": 200, "body": json.dumps({"enabled": True, "nodes": [{"id": NODE["id"], "uuid": NODE["uuid"], "name": NODE["name"], "terminal_online": False}]})}
        return {"status": 200, "body": "{}"}

    def fulfill(route_obj):
        result = route(route_obj.request)
        route_obj.fulfill(content_type="application/json", **result)

    page.route("**/api/**", fulfill)


def launch(playwright):
    options = {"headless": True}
    if CHROMIUM_PATH:
        options["executable_path"] = CHROMIUM_PATH
    return playwright.chromium.launch(**options)


def run():
    with sync_playwright() as playwright:
        browser = launch(playwright)
        page = browser.new_page()
        install_api(page)
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".dashboard-lite-container").wait_for()
        assert page.locator(".sync-state").count() == 1
        assert page.locator(".last-sync").count() == 1
        assert not errors, errors

        degraded_page = browser.new_page()
        install_api(degraded_page, fail_overview=True)
        degraded_page.goto(BASE_URL, wait_until="networkidle")
        degraded_page.locator(".dashboard-lite-container").wait_for()
        assert degraded_page.locator(".dashboard-lite-container").count() == 1
        assert degraded_page.locator(".sync-state").count() == 1
        degraded_page.close()

        page.locator(".nav-item", has_text="系统日志").click()
        page.locator(".log-streaming-view").wait_for()
        assert page.locator(".log-streaming-view").count() == 1

        page.locator(".nav-item", has_text="远程终端").click()
        page.locator(".terminal-page").wait_for()
        assert page.locator(".terminal-connection-pill").count() == 1
        assert page.locator(".terminal-offline-banner").count() == 1

        page.locator(".nav-item", has_text="监测").click()
        page.locator(".monitor-view-container").wait_for()
        assert page.locator(".monitor-view-container").count() == 1

        page.goto(f"{BASE_URL}#/node-detail?uuid=uuid-1", wait_until="networkidle")
        page.locator(".komari-detail-page").wait_for()
        assert page.locator(".komari-detail-page").count() == 1
        page.goto(f"{BASE_URL}#/node-detail?uuid=uuid-2", wait_until="networkidle")
        page.locator(".komari-detail-page").wait_for()
        assert any("/api/nodes/uuid-2" in url for url in page._probewatch_request_urls)

        page.set_viewport_size({"width": 390, "height": 844})
        page.locator(".nav-item", has_text="仪表盘").first.click(force=True)
        page.locator(".dashboard-lite-container").wait_for()
        assert page.locator(".mobile-menu").count() == 1
        page.close()

        guest_page = browser.new_page()
        install_api(guest_page, guest=True)
        guest_page.goto(BASE_URL, wait_until="networkidle")
        guest_page.locator(".guest-shell").wait_for()
        assert guest_page.locator(".side-nav").count() == 0
        assert guest_page.locator(".guest-live-indicator").count() == 1
        guest_page.close()
        browser.close()
        print("browser live regression passed")


if __name__ == "__main__":
    run()
