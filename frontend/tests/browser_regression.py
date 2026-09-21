from playwright.sync_api import sync_playwright


BASE_URL = __import__("os").environ.get("PROBEWATCH_BASE_URL", "http://127.0.0.1:4173/")


def install_api(page, *, me_status=200, nodes_status=200, nodes=None, me=None, alerts=None, alerts_status=200, overview=None, overview_status=200):
    json = __import__("json")
    page.route("**/api/me", lambda route: route.fulfill(status=me_status, content_type="application/json", body=("{}" if me is None else json.dumps(me))))
    page.route("**/api/nodes", lambda route: route.fulfill(status=nodes_status, content_type="application/json", body=json.dumps([] if nodes is None else nodes)))
    page.route("**/api/alerts**", lambda route: route.fulfill(status=alerts_status, content_type="application/json", body=json.dumps([] if alerts is None else alerts)))
    page.route("**/api/overview", lambda route: route.fulfill(status=overview_status, content_type="application/json", body=json.dumps({} if overview is None else overview)))


def run_regressions():
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        tests = []
        node = {"id": "node-1", "uuid": "uuid-1", "name": "测试节点", "status": "online", "last_reported_at": "2026-09-20T00:00:00Z", "resource": {"cpu_percent": 12.5, "memory_total_bytes": 100, "memory_used_bytes": 50}}

        page = browser.new_page()
        install_api(
            page,
            nodes=[node],
            alerts=[{"id": "alert-1", "severity": "warning", "title": "测试告警", "message": "测试告警详情", "status": "open", "last_seen": "2026-09-20T00:00:00Z", "occurrence_count": 1}],
            overview={"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100}, "resources": {"network_rx_bytes": 10, "network_tx_bytes": 20, "network_history": [10, 20]}},
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item").nth(3).click(force=True)
        page.locator(".subpage").wait_for()
        assert page.locator(".subpage").count() == 1
        tests.append("navigation")
        page.close()

        page = browser.new_page()
        install_api(
            page,
            nodes=[node],
            alerts=[{"id": "alert-1", "severity": "warning", "title": "测试告警", "message": "测试告警详情", "status": "open", "last_seen": "2026-09-20T00:00:00Z", "occurrence_count": 1}],
            overview={"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100}, "resources": {"network_rx_bytes": 10, "network_tx_bytes": 20, "network_history": [10, 20]}},
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        assert page.locator(".node-drawer").get_attribute("role") == "dialog"
        page.keyboard.press("Escape")
        assert page.locator(".node-drawer").count() == 0
        tests.append("dialog")
        page.close()

        page = browser.new_page()
        install_api(page, me_status=401)
        page.goto(BASE_URL, wait_until="networkidle")
        auth_state = page.locator('.api-state.api-state-auth')
        auth_state.wait_for()
        assert auth_state.get_by_text("需要登录才能查看节点数据。", exact=True).count() == 1
        assert auth_state.get_by_role("button", name="使用 GitHub 登录", exact=True).count() == 1
        tests.append("auth")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[])
        page.goto(BASE_URL, wait_until="networkidle")
        assert page.get_by_text("API 返回空节点数组，暂无节点数据。").count() == 1
        tests.append("empty-api")
        page.close()

        page = browser.new_page()
        install_api(
            page,
            nodes=[node],
            alerts=[{"id": "alert-1", "severity": "warning", "title": "测试告警", "message": "测试告警详情", "status": "open", "last_seen": "2026-09-20T00:00:00Z", "occurrence_count": 1}],
            overview={"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100}, "resources": {"network_rx_bytes": 10, "network_tx_bytes": 20, "network_history": [10, 20]}},
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        page.locator(".drawer-button").click()
        assert page.locator(".node-detail-page").count() == 1
        assert page.get_by_text("12.5%").count() >= 1
        tests.append("node-detail")
        page.close()
        browser.close()
    print(f"{len(tests)} browser regression tests passed")


if __name__ == "__main__":
    run_regressions()
