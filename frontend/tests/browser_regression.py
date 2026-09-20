from playwright.sync_api import sync_playwright


BASE_URL = "http://127.0.0.1:4173/"


def run_regressions():
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        tests = []

        page = browser.new_page()
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".side-nav .nav-item").nth(3).click()
        assert page.locator(".route-table").count() == 1
        tests.append("navigation")
        page.close()

        page = browser.new_page()
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        assert page.locator(".node-drawer").get_attribute("role") == "dialog"
        page.keyboard.press("Escape")
        assert page.locator(".node-drawer").count() == 0
        tests.append("dialog")
        page.close()

        page = browser.new_page()
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".top-actions .icon-button").click()
        assert page.locator(".placeholder-panel").count() == 1
        page.close()

        page = browser.new_page()
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        page.locator(".drawer-button").click()
        assert page.locator(".node-detail-page").count() == 1
        assert page.locator(".detail-chart").count() == 1
        tests.append("node-detail")
        page.close()
        browser.close()
    print(f"{len(tests)} browser regression tests passed")


if __name__ == "__main__":
    run_regressions()
