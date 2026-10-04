import os

from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
from playwright.sync_api import sync_playwright


BASE_URL = os.environ.get("PROBEWATCH_REAL_BASE_URL", "").strip()
USERNAME = os.environ.get("PROBEWATCH_TEST_USERNAME", "").strip()
PASSWORD = os.environ.get("PROBEWATCH_TEST_PASSWORD", "")
NODE_UUID = os.environ.get("PROBEWATCH_TEST_NODE_UUID", "").strip()
EXPECTED_ROLE = os.environ.get("PROBEWATCH_EXPECT_ROLE", "").strip()
CHROMIUM_PATH = os.environ.get("PROBEWATCH_CHROMIUM")


def required_environment():
    missing = [
        name
        for name, value in (
            ("PROBEWATCH_REAL_BASE_URL", BASE_URL),
            ("PROBEWATCH_TEST_USERNAME", USERNAME),
            ("PROBEWATCH_TEST_PASSWORD", PASSWORD),
        )
        if not value
    ]
    if missing:
        raise SystemExit("real browser regression requires: " + ", ".join(missing))


def click_nav(page, label):
    locator = page.locator(".nav-item", has_text=label).first
    locator.wait_for(timeout=15000)
    locator.click()


def login(page):
    page.goto(BASE_URL, wait_until="domcontentloaded", timeout=30000)
    page.wait_for_timeout(800)
    if page.locator(".app-shell").count() > 0:
        return
    login_button = page.get_by_role("button", name="管理员登录")
    if login_button.count() == 0:
        login_button = page.get_by_text("管理员登录", exact=True)
    login_button.first.click()
    page.locator("#login-username").fill(USERNAME)
    page.locator("#admin-pwd").fill(PASSWORD)
    page.get_by_role("button", name="口令登录进入控制台").click()
    page.locator(".app-shell").wait_for(timeout=20000)


def run():
    required_environment()
    with sync_playwright() as playwright:
        options = {"headless": True}
        if CHROMIUM_PATH:
            options["executable_path"] = CHROMIUM_PATH
        browser = playwright.chromium.launch(**options)
        context = browser.new_context()
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        login(page)

        page.locator(".app-shell").wait_for()
        page.locator(".sync-state").wait_for()
        if EXPECTED_ROLE:
            role_text = page.locator(".profile-avatar-top").get_attribute("title") or ""
            if EXPECTED_ROLE.lower() not in role_text.lower():
                raise AssertionError(f"expected role {EXPECTED_ROLE!r} in profile title, got {role_text!r}")

        if NODE_UUID:
            page.goto(f"{BASE_URL.rstrip('/')}/#/node-detail?uuid={NODE_UUID}", wait_until="domcontentloaded", timeout=30000)
            page.locator(".komari-detail-page").wait_for(timeout=20000)

        click_nav(page, "系统日志")
        page.locator(".log-streaming-view").wait_for(timeout=15000)

        click_nav(page, "远程终端")
        page.locator(".terminal-page").wait_for(timeout=15000)
        page.locator(".terminal-connection-pill").wait_for(timeout=15000)

        click_nav(page, "测速")
        page.locator(".speedtest-benchmark-view").wait_for(timeout=15000)

        if errors:
            raise AssertionError("real browser page errors: " + "; ".join(errors))
        print("real browser regression passed")
        browser.close()


if __name__ == "__main__":
    run()
