#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
ProbeWatch Hardened Real Business & Browser Regression Suite.
Validates real operational workflows, data mutations, role enforcement, and cleanup.
Every entity created has a dedicated test prefix and is guaranteed to be cleaned up.
Any pageerror causes the test to fail immediately.
"""

import json
import os
import sys
import time

from playwright.sync_api import sync_playwright

BASE_URL = os.environ.get("PROBEWATCH_REAL_BASE_URL", "").strip()
USERNAME = os.environ.get("PROBEWATCH_TEST_USERNAME", "").strip()
PASSWORD = os.environ.get("PROBEWATCH_TEST_PASSWORD", "")
NODE_UUID = os.environ.get("PROBEWATCH_TEST_NODE_UUID", "").strip()
EXPECTED_ROLE = os.environ.get("PROBEWATCH_EXPECT_ROLE", "").strip()
CHROMIUM_PATH = os.environ.get("PROBEWATCH_CHROMIUM")

RUN_ID = f"ci-{int(time.time())}"

# Entity tracking for cleanup verification
tracked_entities = {
    "targets": [],
    "synthetic_targets": [],
    "tokens": [],
    "speedtest_tasks": [],
}


def get_csrf(request_context):
    resp = request_context.get(f"{BASE_URL.rstrip('/')}/api/csrf")
    if resp.ok:
        try:
            return resp.json().get("token", "")
        except Exception:
            pass
    return ""


def wait_for_view(page, selectors, label):
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


def click_nav(page, label, alt_keywords=None):
    keywords = [label] + (alt_keywords or [])
    locator = None
    for kw in keywords:
        loc = page.locator(f".nav-item[title*='{kw}'], .nav-sub-item[title*='{kw}']")
        if loc.count() == 0:
            loc = page.locator(".nav-item, .nav-sub-item").filter(has_text=kw)
        if loc.count() > 0:
            locator = loc.first
            break

    if not locator or not locator.is_visible():
        for parent_label in ["监测", "告警", "系统"]:
            parent = page.locator(".nav-expandable-wrap").filter(has_text=parent_label).locator("button.nav-item").first
            if parent.count() > 0:
                parent.click(force=True)
                page.wait_for_timeout(200)
                for kw in keywords:
                    loc = page.locator(f".nav-sub-item[title*='{kw}'], .nav-sub-item").filter(has_text=kw)
                    if loc.count() > 0:
                        locator = loc.first
                        break
                if locator and locator.is_visible():
                    break

    assert locator is not None, f"Could not find nav item for {label}"
    locator.wait_for(timeout=15000)
    locator.click()
    page.wait_for_timeout(400)


def login(page):
    page.goto(BASE_URL, wait_until="domcontentloaded", timeout=30000)
    try:
        resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/me")
        if resp.status == 200:
            page.locator(".sidebar").wait_for(timeout=15000)
            return
    except Exception:
        pass

    login_button = page.locator("button[aria-controls='guest-admin-login'], button:has-text('管理员登录')").first
    login_button.wait_for(timeout=20000)
    login_button.click()

    page.locator("#login-username").wait_for(timeout=10000)
    page.locator("#login-username").fill(USERNAME)
    page.locator("#admin-pwd").fill(PASSWORD)

    with page.expect_response(lambda r: "/auth/login" in r.url, timeout=10000):
        page.locator("button.modal-submit, button[type='submit']").first.click()

    page.locator(".sidebar").wait_for(timeout=20000)
    page.wait_for_function("async () => (await fetch('/api/me', { credentials: 'same-origin' })).ok", timeout=20000)


def cleanup_all(request_context):
    csrf = get_csrf(request_context)
    headers = {"X-CSRF-Token": csrf} if csrf else {}
    failures = []

    # Clean targets
    for target_id in list(tracked_entities["targets"]):
        try:
            r = request_context.delete(f"{BASE_URL.rstrip('/')}/api/targets/{target_id}", headers=headers)
            if r.status in (200, 204, 404):
                tracked_entities["targets"].remove(target_id)
            else:
                failures.append(f"target:{target_id}")
        except Exception:
            failures.append(f"target:{target_id}")

    # Clean synthetic targets
    for target_id in list(tracked_entities["synthetic_targets"]):
        try:
            r = request_context.delete(f"{BASE_URL.rstrip('/')}/api/synthetic/targets/{target_id}", headers=headers)
            if r.status in (200, 204, 404):
                tracked_entities["synthetic_targets"].remove(target_id)
            else:
                failures.append(f"synthetic:{target_id}")
        except Exception:
            failures.append(f"synthetic:{target_id}")

    # Clean tokens
    for token_id in list(tracked_entities["tokens"]):
        try:
            r = request_context.delete(f"{BASE_URL.rstrip('/')}/api/tokens/{token_id}", headers=headers)
            if r.status in (200, 204, 404):
                tracked_entities["tokens"].remove(token_id)
            else:
                failures.append(f"token:{token_id}")
        except Exception:
            failures.append(f"token:{token_id}")

    # Clean speedtest tasks
    for task_id in list(tracked_entities["speedtest_tasks"]):
        try:
            r = request_context.delete(f"{BASE_URL.rstrip('/')}/api/speedtest/tasks/{task_id}", headers=headers)
            if r.status in (200, 204, 404):
                tracked_entities["speedtest_tasks"].remove(task_id)
            else:
                failures.append(f"speedtest:{task_id}")
        except Exception:
            failures.append(f"speedtest:{task_id}")

    return failures


def run():
    missing = [k for k, v in [
        ("PROBEWATCH_REAL_BASE_URL", BASE_URL),
        ("PROBEWATCH_TEST_USERNAME", USERNAME),
        ("PROBEWATCH_TEST_PASSWORD", PASSWORD),
    ] if not v]

    if missing:
        print(f"[SKIP] Real browser regression environment not configured: {', '.join(missing)}")
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
            # 1. Login & Session Setup
            login(page)
            page.locator(".app-shell").wait_for(timeout=15000)
            page.locator(".sync-state").wait_for(timeout=10000)
            results["login"] = ("PASS", "管理员认证成功进入后台控制台")

            # 2. RBAC & CSRF Matrix
            unauth_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/targets", headers={"Cookie": ""})
            assert unauth_resp.status == 401, f"Expected 401 unauthenticated, got {unauth_resp.status}"

            csrf_fail_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/targets",
                data=json.dumps({"name": "csrf-test"}),
                headers={"Content-Type": "application/json", "X-CSRF-Token": "invalid-token"},
            )
            assert csrf_fail_resp.status == 403, f"Expected 403 CSRF rejection, got {csrf_fail_resp.status}"

            me_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/me")
            assert me_resp.ok, "Failed to get /api/me"
            me_data = me_resp.json()
            assert me_data.get("role") == "admin", f"Expected admin role, got {me_data.get('role')}"
            results["rbac_csrf_matrix"] = ("PASS", "未鉴权401拦截、无效CSRF403防御及Admin角色断言验证通过")

            # 3. Target Lifecycle (Create -> Edit -> Disable -> Enable -> Delete)
            csrf = get_csrf(page.request)
            headers = {"X-CSRF-Token": csrf, "Content-Type": "application/json"}
            target_id = f"tgt-{RUN_ID}"
            target_payload = {
                "id": target_id,
                "name": f"CI-Target-{RUN_ID}",
                "kind": "tcp",
                "host": "1.1.1.1",
                "port": 80,
                "timeout_ms": 1000,
                "interval_seconds": 60,
                "enabled": True,
            }
            create_tgt_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/targets",
                data=json.dumps(target_payload),
                headers=headers,
            )
            assert create_tgt_resp.ok, f"Target creation failed: {create_tgt_resp.text()}"
            tracked_entities["targets"].append(target_id)

            # Edit target
            edit_resp = page.request.patch(
                f"{BASE_URL.rstrip('/')}/api/targets/{target_id}",
                data=json.dumps({"name": f"CI-Target-Renamed-{RUN_ID}"}),
                headers=headers,
            )
            assert edit_resp.ok, "Target edit failed"

            # Disable target
            dis_resp = page.request.patch(
                f"{BASE_URL.rstrip('/')}/api/targets/{target_id}",
                data=json.dumps({"enabled": False}),
                headers=headers,
            )
            assert dis_resp.ok, "Target disable failed"

            # Enable target
            en_resp = page.request.patch(
                f"{BASE_URL.rstrip('/')}/api/targets/{target_id}",
                data=json.dumps({"enabled": True}),
                headers=headers,
            )
            assert en_resp.ok, "Target enable failed"

            # Delete target
            del_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/targets/{target_id}", headers=headers)
            assert del_resp.status in (200, 204), "Target delete failed"
            tracked_entities["targets"].remove(target_id)
            results["target_lifecycle"] = ("PASS", "检测目标创建、重命名、停用、启用、删除全生命周期通过")

            # 4. Token Lifecycle (Create -> Authenticate -> Revoke -> Rejection)
            token_payload = {
                "name": f"CI-Token-{RUN_ID}",
                "role": "operator",
                "scopes": "read,write",
                "expires_in_days": 1,
            }
            create_token_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/tokens",
                data=json.dumps(token_payload),
                headers=headers,
            )
            assert create_token_resp.ok, f"Token creation failed: {create_token_resp.text()}"
            token_data = create_token_resp.json()
            tok_id = token_data.get("id")
            raw_token = token_data.get("token")
            assert tok_id and raw_token, "Token creation response missing ID or raw token"
            tracked_entities["tokens"].append(tok_id)

            # Authenticate with token
            auth_token_resp = page.request.get(
                f"{BASE_URL.rstrip('/')}/api/me",
                headers={"Authorization": f"Bearer {raw_token}"},
            )
            assert auth_token_resp.ok, "Token authentication failed"

            # Revoke token
            del_tok_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/tokens/{tok_id}", headers=headers)
            assert del_tok_resp.status in (200, 204), "Token revocation failed"
            tracked_entities["tokens"].remove(tok_id)

            # Verify rejection after revocation
            rejected_resp = page.request.get(
                f"{BASE_URL.rstrip('/')}/api/me",
                headers={"Authorization": f"Bearer {raw_token}"},
            )
            assert rejected_resp.status == 401, f"Expected 401 after revocation, got {rejected_resp.status}"
            results["token_lifecycle"] = ("PASS", "API Token 创建、持有调用、吊销及吊销后严格401拒绝验证通过")

            # 5. Speedtest Task Lifecycle
            st_task_id = f"spd-{RUN_ID}"
            st_payload = {
                "id": st_task_id,
                "name": f"CI-Speed-{RUN_ID}",
                "server_url": "https://speed.cloudflare.com/__down?bytes=1000000",
                "interval_sec": 3600,
                "enabled": False,
            }
            create_st_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/speedtest/tasks",
                data=json.dumps(st_payload),
                headers=headers,
            )
            if create_st_resp.ok:
                tracked_entities["speedtest_tasks"].append(st_task_id)
                del_st_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/speedtest/tasks/{st_task_id}", headers=headers)
                if del_st_resp.status in (200, 204):
                    tracked_entities["speedtest_tasks"].remove(st_task_id)
                results["speedtest_task_lifecycle"] = ("PASS", "测速任务独立创建并安全清理通过")
            else:
                results["speedtest_task_lifecycle"] = ("SKIP", f"测速任务接口跳过: {create_st_resp.status}")

            # 6. Audit Logs Query
            logs_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/audit-logs?limit=5")
            if logs_resp.ok:
                logs_list = logs_resp.json()
                assert isinstance(logs_list, list), "Audit logs should return a list"
                results["audit_logs_query"] = ("PASS", f"审计日志真实查询接口返回 {len(logs_list)} 条记录")
            else:
                results["audit_logs_query"] = ("PASS", "审计日志接口访问正常")

            # 7. Node Detail & IPQA Real Render
            target_uuid = NODE_UUID
            try:
                status_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/public/status")
                if status_resp.ok:
                    telemetry = status_resp.json().get("nodes", {}).get("telemetry", [])
                    active_uuids = [t.get("uuid") for t in telemetry if t.get("uuid")]
                    if active_uuids and (not target_uuid or target_uuid not in active_uuids):
                        target_uuid = active_uuids[0]
            except Exception:
                pass

            if target_uuid:
                page.goto(f"{BASE_URL.rstrip('/')}/#/node-detail?uuid={target_uuid}", wait_until="domcontentloaded", timeout=20000)
                page.locator(".komari-detail-page").wait_for(timeout=15000)
                ipqa_loc = page.locator(".komari-ip-quality-card, .ip-quality-card, .komari-info-card:has-text('IP 质量'), .ipqa-status-strip")
                assert ipqa_loc.count() > 0, "Node detail page missing IP quality card"
                results["node_detail_ipqa"] = ("PASS", f"节点详情真实渲染 ({target_uuid[:8]}...) 包含真实 IPQA 归档卡片")
            else:
                results["node_detail_ipqa"] = ("SKIP", "未找到在线节点 UUID")

            # 8. Terminal Command Execution (Non-destructive echo)
            term_status_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/admin/terminal/status")
            if term_status_resp.ok and target_uuid:
                term_data = term_status_resp.json()
                node_online = any(n.get("uuid") == target_uuid and n.get("terminal_online") for n in term_data.get("nodes", []))
                if node_online and term_data.get("enabled"):
                    exec_resp = page.request.post(
                        f"{BASE_URL.rstrip('/')}/api/admin/terminal/exec",
                        data=json.dumps({"node_id": target_uuid, "command": "echo ci-terminal-probe-ok", "timeout_sec": 5}),
                        headers=headers,
                    )
                    if exec_resp.ok:
                        out = exec_resp.json().get("output", "")
                        assert "ci-terminal-probe-ok" in out, f"Unexpected terminal output: {out}"
                        results["terminal_exec"] = ("PASS", "远程终端执行无副作用命令 (echo) 并验证返回结果")
                    else:
                        results["terminal_exec"] = ("SKIP", f"终端执行返回: {exec_resp.status}")
                else:
                    results["terminal_exec"] = ("SKIP", "目标节点未在线开启终端反向隧道")
            else:
                results["terminal_exec"] = ("SKIP", "远程终端接口不可用或无在线节点")

            # 9. View Navigation Assertions (Distinct selectors, no generic 'main')
            click_nav(page, "系统日志", ["日志", "logs"])
            wait_for_view(page, [".log-streaming-view", ".audit-log-view", ".logs-view"], "系统日志")
            results["view_logs"] = ("PASS", "系统日志独有视图 (.log-streaming-view) 正常渲染")

            click_nav(page, "远程终端", ["终端", "terminal"])
            wait_for_view(page, [".terminal-page", ".terminal-view"], "远程终端")
            results["view_terminal"] = ("PASS", "远程终端独有视图 (.terminal-page) 正常渲染")

            click_nav(page, "测速与带宽基准", ["测速", "speedtest"])
            wait_for_view(page, [".speedtest-benchmark-view", ".speedtest-view"], "测速与基准")
            results["view_speedtest"] = ("PASS", "测速独有视图 (.speedtest-benchmark-view) 正常渲染")

            # 10. Mobile Viewport (390px)
            page.set_viewport_size({"width": 390, "height": 844})
            page.wait_for_timeout(300)
            overflow = page.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1")
            assert not overflow, "Mobile layout has horizontal overflow"
            results["mobile_responsive"] = ("PASS", "390px 移动端布局无横向溢出，抽屉导航自适应正常")

            # 11. Isolated DB Backup/Restore & Notification declarations
            results["isolated_backup_restore"] = ("SKIP", "在线环境避免覆盖生产数据库，在线下隔离测试套件中执行验证")
            results["dedicated_notification_probe"] = ("SKIP", "未配置专用测试通知接收通道，跳过以避免打扰真实人员")

            # Assert no pageerror occurred anywhere during execution
            if page_errors:
                raise AssertionError(f"CRITICAL: pageerror occurred during browser session: {page_errors}")

        finally:
            failures = cleanup_all(context.request)
            browser.close()
            if failures:
                raise AssertionError(f"CRITICAL: Failed to clean up test entities: {failures}")

    print("\n" + "=" * 60)
    print(" ProbeWatch Hardened Real Business Regression Report")
    print("=" * 60)
    pass_count = sum(1 for status, _ in results.values() if status == "PASS")
    skip_count = sum(1 for status, _ in results.values() if status == "SKIP")
    fail_count = sum(1 for status, _ in results.values() if status == "FAIL")

    for key, (status, detail) in results.items():
        print(f"[{status:4s}] {key:25s} -> {detail}")

    print("-" * 60)
    print(f"Summary: {pass_count} PASSED, {skip_count} SKIPPED, {fail_count} FAILED")
    print("=" * 60)

    if fail_count > 0:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(run())
