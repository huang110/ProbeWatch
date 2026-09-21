from playwright.sync_api import sync_playwright


BASE_URL = "http://127.0.0.1:4173/"


def install_api(page, *, me_status=200, nodes_status=200, nodes=None, me=None, alerts=None, alerts_status=200):
    page.route("**/api/me", lambda route: route.fulfill(status=me_status, content_type="application/json", body=("{}" if me is None else __import__("json").dumps(me))))
    page.route("**/api/nodes", lambda route: route.fulfill(status=nodes_status, content_type="application/json", body=__import__("json").dumps([] if nodes is None else nodes)))
    page.route("**/api/alerts**", lambda route: route.fulfill(status=alerts_status, content_type="application/json", body=__import__("json").dumps([] if alerts is None else alerts)))
    page.route("**/api/overview", lambda route: route.fulfill(status=200, content_type="application/json", body=__import__("json").dumps({})))


def run_regressions():
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        tests = []
        node = {"id": "node-1", "uuid": "uuid-1", "name": "测试节点", "status": "online", "last_reported_at": "2026-09-20T00:00:00Z", "resource": {"cpu_percent": 12.5, "memory_total_bytes": 100, "memory_used_bytes": 50}}

        page = browser.new_page()
        install_api(page, nodes=[node])
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".side-nav .nav-item").nth(3).click()
        page.locator(".subpage").wait_for()
        assert page.locator(".subpage").count() == 1
        tests.append("navigation")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node])
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
        page.get_by_text("需要登录才能查看节点数据。").wait_for()
        assert page.get_by_role("button", name="使用 GitHub 登录").count() == 1
        tests.append("auth")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[])
        page.goto(BASE_URL, wait_until="networkidle")
        assert page.get_by_text("API 返回空节点数组，暂无节点数据。").count() == 1
        tests.append("empty-api")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node])
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
