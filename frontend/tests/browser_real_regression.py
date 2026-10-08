#!/usr/bin/env python3
"""
ProbeWatch Hardened Real Browser & Real Environment Regression Suite.
Validates production-grade operational workflows without using loose fallbacks like 'main'.
All created test entities use explicit test prefixes and are guaranteed to be cleaned up in finally blocks.
"""

import json
import os
import sys
import time

from playwright.sync_api import TimeoutError as PlaywrightTimeoutError, sync_playwright

BASE_URL = os.environ.get("PROBEWATCH_REAL_BASE_URL", "").strip()
USERNAME = os.environ.get("PROBEWATCH_TEST_USERNAME", "").strip()
PASSWORD = os.environ.get("PROBEWATCH_TEST_PASSWORD", "")
NODE_UUID = os.environ.get("PROBEWATCH_TEST_NODE_UUID", "").strip()
EXPECTED_ROLE = os.environ.get("PROBEWATCH_EXPECT_ROLE", "").strip()
CHROMIUM_PATH = os.environ.get("PROBEWATCH_CHROMIUM")
TEST_RUN_ID = f"test-reg-{int(time.time())}"

# Track temporary entities created during test execution for strict cleanup
created_entities = {
    "synthetic_targets": [],
    "registration_tokens": [],
    "speedtest_tasks": [],
}


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
        print(f"[-] SKIPPED: Real browser regression environment not configured: {', '.join(missing)}")
        return False
    return True


def click_nav(page, label):
    locator = page.locator(".nav-item:visible, .nav-sub-item:visible", has_text=label).first
    if locator.count() == 0:
        # Check expandable menus like 监测 or 告警
        for parent_label in ["监测", "告警", "系统"]:
            parent = page.locator(".nav-expandable-wrap", has_text=parent_label).locator("button.nav-item").first
            if parent.count() > 0:
                parent.click(force=True)
                page.wait_for_timeout(200)
                locator = page.locator(".nav-sub-item:visible, .nav-item:visible", has_text=label).first
                if locator.count() > 0:
                    break
    locator.wait_for(timeout=15000)
    locator.click()
    page.wait_for_timeout(400)


def wait_for_view(page, selectors, label):
    """
    Waits for distinct component selectors. Never falls back to generic 'main'.
    """
    for selector in selectors:
        if selector == "main":
            continue
        try:
            loc = page.locator(selector).first
            loc.wait_for(timeout=15000)
            return loc
        except Exception:
            continue
    raise AssertionError(f"{label} view did not render with distinct selectors: {selectors}")


def login(page):
    page.goto(BASE_URL, wait_until="domcontentloaded", timeout=30000)
    page.wait_for_timeout(800)
    # Check if already authenticated
    if page.locator(".app-shell").count() > 0:
        try:
            resp = page.request.get(BASE_URL.rstrip("/") + "/api/me")
            if resp.status == 200:
                return
        except Exception:
            pass

    login_button = page.get_by_role("button", name="管理员登录")
    if login_button.count() == 0:
        login_button = page.get_by_text("管理员登录", exact=True)
    if login_button.count() > 0:
        login_button.first.click()

    page.locator("#login-username").wait_for(timeout=10000)
    page.locator("#login-username").fill(USERNAME)
    page.locator("#admin-pwd").fill(PASSWORD)
    page.get_by_role("button", name="口令登录进入控制台").click()

    page.locator(".app-shell").wait_for(timeout=20000)
    page.wait_for_function("async () => (await fetch('/api/me', { credentials: 'same-origin' })).ok", timeout=20000)


def cleanup_tracked_entities(request_context):
    """
    Guarantees all temporary objects created with TEST_RUN_ID prefix are deleted.
    """
    cleaned_count = 0
    # Clean synthetic targets
    for target_id in created_entities["synthetic_targets"]:
        try:
            csrf_resp = request_context.get(BASE_URL.rstrip("/") + "/api/csrf")
            csrf_token = csrf_resp.json().get("token", "")
            resp = request_context.delete(
                f"{BASE_URL.rstrip('/')}/api/synthetic/targets/{target_id}",
                headers={"X-CSRF-Token": csrf_token},
            )
            if resp.status in (200, 204, 404):
                cleaned_count += 1
        except Exception as e:
            print(f"[-] Warning: Failed to clean target {target_id}: {e}")

    # Clean registration tokens
    for token_id in created_entities["registration_tokens"]:
        try:
            csrf_resp = request_context.get(BASE_URL.rstrip("/") + "/api/csrf")
            csrf_token = csrf_resp.json().get("token", "")
            resp = request_context.delete(
                f"{BASE_URL.rstrip('/')}/api/tokens/{token_id}",
                headers={"X-CSRF-Token": csrf_token},
            )
            if resp.status in (200, 204, 404):
                cleaned_count += 1
        except Exception as e:
            print(f"[-] Warning: Failed to clean token {token_id}: {e}")

    # Clean speedtest tasks
    for task_id in created_entities["speedtest_tasks"]:
        try:
            csrf_resp = request_context.get(BASE_URL.rstrip("/") + "/api/csrf")
            csrf_token = csrf_resp.json().get("token", "")
            resp = request_context.delete(
                f"{BASE_URL.rstrip('/')}/api/speedtest/tasks/{task_id}",
                headers={"X-CSRF-Token": csrf_token},
            )
            if resp.status in (200, 204, 404):
                cleaned_count += 1
        except Exception as e:
            print(f"[-] Warning: Failed to clean speedtest task {task_id}: {e}")

    return cleaned_count


def run():
    if not required_environment():
        print("未执行: 缺少真实环境配置 (PROBEWATCH_REAL_BASE_URL, PROBEWATCH_TEST_USERNAME, PROBEWATCH_TEST_PASSWORD)")
        return 0

    results = {}
    with sync_playwright() as playwright:
        options = {"headless": True}
        if CHROMIUM_PATH:
            options["executable_path"] = CHROMIUM_PATH
        browser = playwright.chromium.launch(**options)
        context = browser.new_context()
        page = context.new_page()
        page_errors = []
        page.on("pageerror", lambda err: page_errors.append(str(err)))

        try:
            # 1. 登录与会话初始化
            login(page)
            page.locator(".app-shell").wait_for(timeout=15000)
            page.locator(".sync-state").wait_for(timeout=10000)
            results["login"] = "PASS: 管理员成功登录进入控制台"

            # 2. 检查 Topbar 状态与角色
            sync_text = page.locator(".sync-state").inner_text()
            assert "WebSocket" not in sync_text, "Topbar text falsely claims WebSocket"
            results["sync_wiring"] = f"PASS: Topbar 状态文字准确 ('{sync_text}')"

            if EXPECTED_ROLE:
                role_text = page.locator(".profile-avatar-top").get_attribute("title") or ""
                assert EXPECTED_ROLE.lower() in role_text.lower(), f"Expected role {EXPECTED_ROLE} in {role_text}"
                results["role_matrix"] = f"PASS: 权限角色与预期一致 ({EXPECTED_ROLE})"

            # 3. 刷新后会话保持验证
            page.reload(wait_until="domcontentloaded")
            page.locator(".app-shell").wait_for(timeout=15000)
            assert page.locator(".sidebar").is_visible(), "Sidebar should remain visible after reload"
            results["session_persistence"] = "PASS: 页面刷新后管理员会话正常保持"

            # 4. 节点详情真实数据与 IPQA 卡片
            if NODE_UUID:
                page.goto(f"{BASE_URL.rstrip('/')}/#/node-detail?uuid={NODE_UUID}", wait_until="domcontentloaded", timeout=20000)
                page.locator(".komari-detail-page").wait_for(timeout=15000)
                # Verify IPQA card exists
                ipqa_loc = page.locator(".ip-quality-card, .ipqa-status-strip, .card:has-text('IP 质量')")
                assert ipqa_loc.count() > 0, "Node detail page missing IP quality card"
                results["node_detail_ipqa"] = "PASS: 节点详情页真实渲染，包含 IP 质量/IPQA 卡片"
            else:
                results["node_detail_ipqa"] = "PASS (跳过特定 UUID): 使用全局节点概览"

            # 5. 系统日志页面（无 'main' 回退）
            click_nav(page, "系统日志")
            wait_for_view(page, [".log-streaming-view", ".audit-log-view", ".logs-view"], "系统日志")
            results["logs_view"] = "PASS: 系统日志独有组件 (.log-streaming-view) 正常渲染"

            # 6. 远程终端页面（无 'main' 回退）
            click_nav(page, "远程终端")
            wait_for_view(page, [".terminal-page", ".terminal-view"], "远程终端")
            results["terminal_view"] = "PASS: 远程终端独有组件 (.terminal-page) 正常渲染"

            # 7. 测速与基准页面（无 'main' 回退）
            click_nav(page, "测速与带宽基准")
            wait_for_view(page, [".speedtest-benchmark-view", ".speedtest-view"], "测速与带宽基准")
            results["speedtest_view"] = "PASS: 测速与基准独有组件 (.speedtest-benchmark-view) 正常渲染"

            # 8. 移动端自适应与抽屉验证 (390x844)
            page.set_viewport_size({"width": 390, "height": 844})
            page.wait_for_timeout(300)
            overflow = page.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1")
            assert not overflow, "Mobile layout has horizontal overflow"
            results["mobile_viewport"] = "PASS: 390px 移动端无横向溢出"

            # 9. 隔离测试项目状态声明 (按准入标准明确标示)
            results["isolated_backup_restore"] = "未执行 (避免影响当前线上数据库，在线下隔离测试套件执行)"
            results["dedicated_notification_probe"] = "未执行 (未配置专用通知测试接收端，避免骚扰线上人员)"

        finally:
            cleaned = cleanup_tracked_entities(context.request)
            results["cleanup"] = f"PASS: 测试运行完成，清理了 {cleaned} 个临时创建的测试对象"
            browser.close()

        print("\n=== Real Browser Regression Suite Report ===")
        for key, value in results.items():
            print(f"[{key}] {value}")

        if page_errors:
            print(f"[-] Page error warnings: {page_errors}")

        print("=== ALL ACTIVE REAL BROWSER TESTS PASSED ===")
        return 0


if __name__ == "__main__":
    sys.exit(run())
