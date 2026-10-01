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


def install_api(page, *, guest=False):
    public = {
        "nodes": {"online": 1, "total": 1, "names": ["测试节点"]},
        "checks": {"success_rate": 100, "avg_latency_ms": 12.5},
        "last_updated_at": "2026-09-20T00:00:00Z",
        "generated_at": "2026-09-20T00:00:01Z",
    }

    def route(request):
        url = request.url
        if "/api/me" in url:
            return {"status": 401 if guest else 200, "body": "{}" if guest else json.dumps({"login": "admin", "role": "admin"})}
        if "/api/public/status" in url:
            return {"status": 200, "body": json.dumps(public)}
        if url.endswith("/api/nodes"):
            return {"status": 200, "body": json.dumps([] if guest else [NODE])}
        if "/api/alerts" in url:
            return {"status": 200, "body": "[]"}
        if "/api/overview" in url:
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
            return {"status": 200, "body": json.dumps({"nodes": []})}
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

        page.locator(".nav-item", has_text="系统日志").click()
        page.locator("text=系统日志").first.wait_for()
        assert page.locator(".content-wrap").count() == 1

        page.locator(".nav-item", has_text="远程终端").click()
        page.locator("text=远程终端").first.wait_for()
        assert page.locator(".content-wrap").count() == 1

        page.locator(".nav-item", has_text="监测").click()
        page.locator("text=延迟监测").first.wait_for()
        assert page.locator(".content-wrap").count() == 1

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
