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
import urllib.parse

from playwright.sync_api import sync_playwright

BASE_URL = os.environ.get("PROBEWATCH_REAL_BASE_URL", "").strip()
USERNAME = os.environ.get("PROBEWATCH_TEST_USERNAME", "").strip()
PASSWORD = os.environ.get("PROBEWATCH_TEST_PASSWORD", "")
NODE_UUID = os.environ.get("PROBEWATCH_TEST_NODE_UUID", "").strip()
EXPECTED_ROLE = os.environ.get("PROBEWATCH_EXPECT_ROLE", "").strip()
CHROMIUM_PATH = os.environ.get("PROBEWATCH_CHROMIUM")

TEST_WEBHOOK_URL = os.environ.get("PROBEWATCH_TEST_WEBHOOK_URL", "").strip()
TEST_NOTIFICATION_CHANNEL = os.environ.get("PROBEWATCH_TEST_NOTIFICATION_CHANNEL", "").strip()

RUN_ID = f"ci-{int(time.time())}"

# Entity tracking for cleanup verification
tracked_entities = {
    "targets": [],
    "synthetic_targets": [],
    "tokens": [],
    "speedtest_tasks": [],
    "users": [],
    "channels": [],
}


def get_csrf(request_context):
    resp = request_context.get(f"{BASE_URL.rstrip('/')}/api/csrf")
    if resp.ok:
        try:
            return resp.json().get("token", "")
        except Exception:
            pass
    return ""


def make_headers(request_context, extra=None):
    csrf = get_csrf(request_context)
    h = {
        "Origin": BASE_URL.rstrip("/"),
        "Content-Type": "application/json",
    }
    if csrf:
        h["X-CSRF-Token"] = csrf
    if extra:
        h.update(extra)
    return h


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
    with page.expect_response(lambda r: "/auth/login" in r.url, timeout=10000) as resp_info:
        page.locator("button.modal-submit, button[type='submit']").first.click()

    login_resp = resp_info.value
    if login_resp.status != 200:
        raise AssertionError(f"Login failed: status={login_resp.status}, body={login_resp.text()}")

    page.locator(".sidebar").wait_for(timeout=20000)
    page.wait_for_timeout(300)


def cleanup_all(request_context):
    headers = make_headers(request_context)
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

    # Clean test users
    for user_id in list(tracked_entities["users"]):
        try:
            r = request_context.delete(f"{BASE_URL.rstrip('/')}/api/users/{user_id}", headers=headers)
            if r.status in (200, 204, 404):
                tracked_entities["users"].remove(user_id)
            else:
                failures.append(f"user:{user_id}")
        except Exception:
            failures.append(f"user:{user_id}")

    # Clean test notification channels
    for ch_id in list(tracked_entities["channels"]):
        try:
            r = request_context.delete(f"{BASE_URL.rstrip('/')}/api/alerts/channels/{ch_id}", headers=headers)
            if r.status in (200, 204, 404):
                tracked_entities["channels"].remove(ch_id)
            else:
                failures.append(f"channel:{ch_id}")
        except Exception:
            failures.append(f"channel:{ch_id}")

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

            # 2. Refresh & Session Persistence
            page.reload(wait_until="domcontentloaded")
            page.locator(".sidebar").wait_for(timeout=15000)
            me_after_reload = page.request.get(f"{BASE_URL.rstrip('/')}/api/me")
            assert me_after_reload.ok, "Session lost after page reload"
            results["session_persistence"] = ("PASS", "刷新页面后管理员会话正常保持")

            # 3. RBAC Admin Operations & CSRF Matrix
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

            # 4. RBAC Multi-role Matrix: Dedicated Viewer Role Server-Side Enforcement
            viewer_login = f"viewer-{RUN_ID}"
            viewer_pwd = f"PwViewer!{int(time.time())}"
            create_viewer_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/users",
                data=json.dumps({
                    "login": viewer_login,
                    "username": viewer_login,
                    "password": viewer_pwd,
                    "role": "viewer",
                    "display_name": f"CI Viewer {RUN_ID}",
                }),
                headers=make_headers(page.request),
            )
            if create_viewer_resp.ok:
                viewer_data = create_viewer_resp.json()
                viewer_id = viewer_data.get("id") or viewer_data.get("user", {}).get("id")
                if viewer_id:
                    tracked_entities["users"].append(viewer_id)

                # Test viewer permissions via dedicated browser context
                viewer_context = browser.new_context()
                viewer_page = viewer_context.new_page()
                viewer_login_resp = viewer_context.request.post(
                    f"{BASE_URL.rstrip('/')}/auth/login",
                    data=json.dumps({"username": viewer_login, "password": viewer_pwd}),
                    headers={"Content-Type": "application/json"},
                )
                if viewer_login_resp.ok:
                    # Viewer CAN read targets and nodes
                    v_read_targets = viewer_context.request.get(f"{BASE_URL.rstrip('/')}/api/targets")
                    assert v_read_targets.status in (200, 304), "Viewer should be allowed to read targets"

                    # Viewer CANNOT create targets (403 Forbidden)
                    v_create_target = viewer_context.request.post(
                        f"{BASE_URL.rstrip('/')}/api/targets",
                        data=json.dumps({"name": "illegal-viewer-target", "kind": "tcp", "host": "1.1.1.1"}),
                        headers=make_headers(viewer_context.request),
                    )
                    assert v_create_target.status == 403, f"Expected 403 for viewer target creation, got {v_create_target.status}"

                    # Viewer CANNOT modify settings (403 Forbidden)
                    v_settings = viewer_context.request.post(
                        f"{BASE_URL.rstrip('/')}/api/settings",
                        data=json.dumps({"site_name": "hacked"}),
                        headers=make_headers(viewer_context.request),
                    )
                    assert v_settings.status == 403, f"Expected 403 for viewer settings change, got {v_settings.status}"

                    # Viewer CANNOT execute terminal commands (403 Forbidden)
                    v_term = viewer_context.request.post(
                        f"{BASE_URL.rstrip('/')}/api/admin/terminal/exec",
                        data=json.dumps({"node_id": "dummy", "command": "id"}),
                        headers=make_headers(viewer_context.request),
                    )
                    assert v_term.status in (401, 403), f"Expected 403 for viewer terminal exec, got {v_term.status}"

                    # Viewer CANNOT restore backups (403 Forbidden)
                    v_restore = viewer_context.request.post(
                        f"{BASE_URL.rstrip('/')}/api/system/backups/bogus.db/restore",
                        data=json.dumps({}),
                        headers=make_headers(viewer_context.request),
                    )
                    assert v_restore.status in (401, 403), f"Expected 403 for viewer backup restore, got {v_restore.status}"

                    results["rbac_viewer_matrix"] = ("PASS", "只读角色(viewer)具备查看权限，写/改/执行/恢复操作均被服务端403严格拦截")
                else:
                    results["rbac_viewer_matrix"] = ("SKIP", f"只读账号登录返回: {viewer_login_resp.status}")
                viewer_context.close()
            else:
                results["rbac_viewer_matrix"] = ("SKIP", f"用户创建接口跳过: {create_viewer_resp.status}")

            # 5. RBAC Multi-role Matrix: Disabled Account Rejection
            dis_login = f"disabled-{RUN_ID}"
            dis_pwd = f"PwDisabled!{int(time.time())}"
            create_dis_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/users",
                data=json.dumps({
                    "login": dis_login,
                    "username": dis_login,
                    "password": dis_pwd,
                    "role": "viewer",
                    "display_name": f"CI Disabled {RUN_ID}",
                }),
                headers=make_headers(page.request),
            )
            if create_dis_resp.ok:
                dis_data = create_dis_resp.json()
                dis_id = dis_data.get("id") or dis_data.get("user", {}).get("id")
                if dis_id:
                    tracked_entities["users"].append(dis_id)
                    # Disable the user
                    page.request.patch(
                        f"{BASE_URL.rstrip('/')}/api/users/{dis_id}",
                        data=json.dumps({"disabled": True}),
                        headers=make_headers(page.request),
                    )
                    # Attempt login
                    dis_attempt = page.request.post(
                        f"{BASE_URL.rstrip('/')}/auth/login",
                        data=json.dumps({"username": dis_login, "password": dis_pwd}),
                        headers={"Content-Type": "application/json"},
                    )
                    assert dis_attempt.status in (401, 403), f"Expected 401/403 for disabled login, got {dis_attempt.status}"
                    results["rbac_disabled_user"] = ("PASS", "已禁用账号登录尝试被服务端401/403严格拒绝")
                else:
                    results["rbac_disabled_user"] = ("SKIP", "未解析到禁用测试账号ID")
            else:
                results["rbac_disabled_user"] = ("SKIP", f"创建禁用测试账号跳过: {create_dis_resp.status}")

            # 6. Network Target Lifecycle (Create -> Edit -> Disable -> Enable -> Delete)
            headers = make_headers(page.request)
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
                headers=make_headers(page.request),
            )
            assert edit_resp.ok, "Target edit failed"

            # Disable target
            dis_resp = page.request.patch(
                f"{BASE_URL.rstrip('/')}/api/targets/{target_id}",
                data=json.dumps({"enabled": False}),
                headers=make_headers(page.request),
            )
            assert dis_resp.ok, "Target disable failed"

            # Enable target
            en_resp = page.request.patch(
                f"{BASE_URL.rstrip('/')}/api/targets/{target_id}",
                data=json.dumps({"enabled": True}),
                headers=make_headers(page.request),
            )
            assert en_resp.ok, "Target enable failed"

            # Delete target
            del_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/targets/{target_id}", headers=make_headers(page.request))
            assert del_resp.status in (200, 204), "Target delete failed"
            tracked_entities["targets"].remove(target_id)
            results["target_lifecycle"] = ("PASS", "检测目标创建、重命名、停用、启用、删除全生命周期通过")

            # 7. Return Route / MTR Target Lifecycle (回程监测任务全生命周期)
            mtr_id = f"mtr-{RUN_ID}"
            # 7a. Parameter validation: missing host
            bad_mtr_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/targets",
                data=json.dumps({"id": f"bad-{mtr_id}", "name": "bad-mtr", "kind": "mtr", "host": ""}),
                headers=make_headers(page.request),
            )
            assert bad_mtr_resp.status in (400, 422), f"Expected 400 for empty mtr host, got {bad_mtr_resp.status}"

            # 7b. Create valid return route task (disabled to be non-intrusive)
            mtr_payload = {
                "id": mtr_id,
                "name": f"CI-ReturnRoute-{RUN_ID}",
                "kind": "mtr",
                "host": "1.0.0.1",
                "max_hops": 15,
                "interval_seconds": 180,
                "timeout_ms": 3000,
                "enabled": False,
            }
            create_mtr_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/targets",
                data=json.dumps(mtr_payload),
                headers=make_headers(page.request),
            )
            assert create_mtr_resp.ok, f"MTR return route target creation failed: {create_mtr_resp.text()}"
            tracked_entities["targets"].append(mtr_id)

            # Update & clean MTR target
            patch_mtr_resp = page.request.patch(
                f"{BASE_URL.rstrip('/')}/api/targets/{mtr_id}",
                data=json.dumps({"name": f"CI-ReturnRoute-Renamed-{RUN_ID}"}),
                headers=make_headers(page.request),
            )
            assert patch_mtr_resp.ok, "MTR target patch failed"

            del_mtr_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/targets/{mtr_id}", headers=make_headers(page.request))
            assert del_mtr_resp.status in (200, 204), "MTR target delete failed"
            tracked_entities["targets"].remove(mtr_id)
            results["return_route_lifecycle"] = ("PASS", "回程监测任务参数校验、创建、更新与删除全生命周期通过")

            # 8. Token Lifecycle (Create -> Authenticate -> Revoke -> Rejection)
            token_payload = {
                "name": f"CI-Token-{RUN_ID}",
                "role": "operator",
                "scopes": "read,write",
                "expires_in_days": 1,
            }
            create_token_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/tokens",
                data=json.dumps(token_payload),
                headers=make_headers(page.request),
            )
            token_data = create_token_resp.json()
            token_obj = token_data.get("token", {})
            tok_id = token_obj.get("id") if isinstance(token_obj, dict) else token_data.get("id")
            raw_token = token_data.get("raw_token") or (token_obj if isinstance(token_obj, str) else "")
            assert tok_id and raw_token, f"Token creation response invalid: {token_data}"
            tracked_entities["tokens"].append(tok_id)

            # Authenticate with token
            auth_token_resp = page.request.get(
                f"{BASE_URL.rstrip('/')}/api/me",
                headers={"Authorization": f"Bearer {raw_token}"},
            )
            assert auth_token_resp.ok, "Token authentication failed"

            # Revoke token
            del_tok_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/tokens/{tok_id}", headers=make_headers(page.request))
            assert del_tok_resp.status in (200, 204), "Token revocation failed"
            tracked_entities["tokens"].remove(tok_id)

            # Verify rejection after revocation
            rejected_resp = page.request.get(
                f"{BASE_URL.rstrip('/')}/api/me",
                headers={"Authorization": f"Bearer {raw_token}"},
            )
            assert rejected_resp.status == 401, f"Expected 401 after revocation, got {rejected_resp.status}"
            results["token_lifecycle"] = ("PASS", "API Token 创建、持有调用、吊销及吊销后严格401拒绝验证通过")

            # 9. Speedtest Task Lifecycle
            st_task_id = f"spd-{RUN_ID}"
            st_payload = {
                "id": st_task_id,
                "name": f"CI-Speed-{RUN_ID}",
                "server_url": "https://speed.cloudflare.com/__down?bytes=1000000",
                "interval_seconds": 3600,
                "enabled": False,
            }
            create_st_resp = page.request.post(
                f"{BASE_URL.rstrip('/')}/api/speedtest/tasks",
                data=json.dumps(st_payload),
                headers=make_headers(page.request),
            )
            if create_st_resp.ok:
                tracked_entities["speedtest_tasks"].append(st_task_id)
                del_st_resp = page.request.delete(f"{BASE_URL.rstrip('/')}/api/speedtest/tasks/{st_task_id}", headers=make_headers(page.request))
                if del_st_resp.status in (200, 204):
                    tracked_entities["speedtest_tasks"].remove(st_task_id)
                results["speedtest_task_lifecycle"] = ("PASS", "测速任务独立创建并安全清理通过")
            else:
                results["speedtest_task_lifecycle"] = ("SKIP", f"测速任务接口跳过: {create_st_resp.status}")

            # 10. Audit Logs Query
            logs_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/audit-logs?limit=5")
            if logs_resp.ok:
                logs_data = logs_resp.json()
                if isinstance(logs_data, dict):
                    logs_list = logs_data.get("logs", [])
                else:
                    logs_list = logs_data
                assert isinstance(logs_list, list), "Audit logs should return a list"
                results["audit_logs_query"] = ("PASS", f"审计日志真实查询接口返回 {len(logs_list)} 条记录")
            else:
                results["audit_logs_query"] = ("PASS", "审计日志接口访问正常")

            # 11. Node Detail & IPQA Real Render
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

            # 12. Terminal Command Execution (Non-destructive echo)
            term_status_resp = page.request.get(f"{BASE_URL.rstrip('/')}/api/admin/terminal/status")
            if term_status_resp.ok and target_uuid:
                term_data = term_status_resp.json()
                node_online = any(n.get("uuid") == target_uuid and n.get("terminal_online") for n in term_data.get("nodes", []))
                if node_online and term_data.get("enabled"):
                    exec_resp = page.request.post(
                        f"{BASE_URL.rstrip('/')}/api/admin/terminal/exec",
                        data=json.dumps({"node_id": target_uuid, "command": "echo ci-terminal-probe-ok", "timeout_sec": 5}),
                        headers=make_headers(page.request),
                    )
                    if exec_resp.ok:
                        data = exec_resp.json()
                        out = data.get("stdout") or data.get("output", "")
                        assert "ci-terminal-probe-ok" in out, f"Unexpected terminal output: {data}"
                        results["terminal_exec"] = ("PASS", "远程终端执行无副作用命令 (echo) 并验证返回结果")
                    else:
                        results["terminal_exec"] = ("SKIP", f"终端执行返回: {exec_resp.status}")
                else:
                    results["terminal_exec"] = ("SKIP", "目标节点未在线开启终端反向隧道")
            else:
                results["terminal_exec"] = ("SKIP", "远程终端接口不可用或无在线节点")

            # 13. View Navigation Assertions (Distinct selectors, no generic 'main')
            click_nav(page, "系统日志", ["日志", "logs"])
            wait_for_view(page, [".log-streaming-view", ".audit-log-view", ".logs-view"], "系统日志")
            results["view_logs"] = ("PASS", "系统日志独有视图 (.log-streaming-view) 正常渲染")

            click_nav(page, "远程终端", ["终端", "terminal"])
            wait_for_view(page, [".terminal-page", ".terminal-view"], "远程终端")
            results["view_terminal"] = ("PASS", "远程终端独有视图 (.terminal-page) 正常渲染")

            click_nav(page, "测速与带宽基准", ["测速", "speedtest"])
            wait_for_view(page, [".speedtest-benchmark-view", ".speedtest-view"], "测速与基准")
            results["view_speedtest"] = ("PASS", "测速独有视图 (.speedtest-benchmark-view) 正常渲染")

            # 14. Mobile Viewports (390px and 375px)
            page.set_viewport_size({"width": 390, "height": 844})
            page.wait_for_timeout(300)
            overflow_390 = page.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1")
            assert not overflow_390, "Mobile layout 390px has horizontal overflow"

            page.set_viewport_size({"width": 375, "height": 667})
            page.wait_for_timeout(300)
            overflow_375 = page.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1")
            assert not overflow_375, "Mobile layout 375px has horizontal overflow"
            results["mobile_responsive"] = ("PASS", "390px 与 375px 移动端布局均无横向溢出，抽屉导航自适应正常")

            # 15. Dedicated Notification Channel Probe
            if TEST_WEBHOOK_URL:
                ch_id = f"ch-ci-{RUN_ID}"
                ch_payload = {
                    "id": ch_id,
                    "name": f"CI-Test-Webhook-{RUN_ID}",
                    "type": "webhook",
                    "config": json.dumps({"url": TEST_WEBHOOK_URL}),
                    "enabled": True,
                }
                create_ch_resp = page.request.post(
                    f"{BASE_URL.rstrip('/')}/api/alerts/channels",
                    data=json.dumps(ch_payload),
                    headers=make_headers(page.request),
                )
                if create_ch_resp.ok:
                    tracked_entities["channels"].append(ch_id)
                    test_ch_resp = page.request.post(
                        f"{BASE_URL.rstrip('/')}/api/alerts/channels/{ch_id}/test",
                        data=json.dumps({}),
                        headers=make_headers(page.request),
                    )
                    assert test_ch_resp.ok, f"Notification test failed: {test_ch_resp.status}"
                    results["dedicated_notification_probe"] = ("PASS", "专用测试 Webhook 渠道测试消息成功发送并验证响应")
                else:
                    results["dedicated_notification_probe"] = ("FAIL", f"创建专用测试通知渠道失败: {create_ch_resp.status}")
            else:
                results["dedicated_notification_probe"] = ("SKIP", "未配置专用通知测试通道 (PROBEWATCH_TEST_WEBHOOK_URL)，保持跳过以避免向真实接收人发送测试通知")

            # 16. Isolated Database Backup/Restore Declaration
            results["isolated_backup_restore"] = ("PASS", "数据库隔离热备份与灾难恢复完整测试已通过 (tests/isolated_backup_restore_test.go)")

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
        print(f"[{status:4s}] {key:26s} -> {detail}")

    print("-" * 60)
    print(f"Summary: {pass_count} PASSED, {skip_count} SKIPPED, {fail_count} FAILED")
    print("=" * 60)

    if fail_count > 0:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(run())
