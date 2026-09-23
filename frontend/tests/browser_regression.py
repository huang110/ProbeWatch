import os
import re
from datetime import datetime, timedelta, timezone

from playwright.sync_api import sync_playwright


BASE_URL = os.environ.get("PROBEWATCH_BASE_URL", "http://127.0.0.1:4173/")


def install_api(page, *, me_status=200, nodes_status=200, nodes=None, me=None, alerts=None, alerts_status=200, overview=None, overview_status=200, public_status=None, checks=None, checks_status=200, traffic=None, traffic_status=200, media=None, media_status=200, targets=None, targets_status=200, registration=None, csrf_token="contract-csrf-token"):
    import json
    empty_public_status = {"nodes": {"online": 0, "total": 0, "names": []}, "checks": {"success_rate": None, "avg_latency_ms": None}, "last_updated_at": None, "generated_at": "2026-09-22T00:00:00Z"}
    empty_traffic = {"period": "day", "window": {"from": "2026-09-21T00:00:00Z", "to": "2026-09-22T00:00:00Z"}, "rx_bytes": None, "tx_bytes": None, "rx_resets": 0, "tx_resets": 0, "interval_seconds": 3600, "series": []}
    page.route("**/api/me", lambda route: route.fulfill(status=me_status, content_type="application/json", body=("{}" if me is None else json.dumps(me))))
    page.route("**/api/nodes", lambda route: route.fulfill(status=nodes_status, content_type="application/json", body=json.dumps([] if nodes is None else nodes)))
    page.route("**/api/alerts**", lambda route: route.fulfill(status=alerts_status, content_type="application/json", body=json.dumps([] if alerts is None else alerts)))
    page.route("**/api/overview", lambda route: route.fulfill(status=overview_status, content_type="application/json", body=json.dumps({} if overview is None else overview)))
    page.route("**/api/public/status*", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(empty_public_status if public_status is None else public_status)))
    page.route("**/api/nodes/*/checks/summary", lambda route: route.fulfill(status=checks_status, content_type="application/json", body=json.dumps([] if checks is None else checks)))

    def media_route(route):
        if media is None:
            route.fulfill(status=media_status, content_type="application/json", body="[]")
            return
        if isinstance(media, dict):
            uuid = route.request.url.rstrip("/").split("/")[-2]
            payload = media.get(uuid)
            if payload is None:
                route.fulfill(status=500, content_type="application/json", body="{}")
                return
            route.fulfill(status=media_status, content_type="application/json", body=json.dumps(payload))
            return
        route.fulfill(status=media_status, content_type="application/json", body=json.dumps(media))

    page.route("**/api/nodes/*/media", media_route)

    def traffic_route(route):
        url = route.request.url
        period = "month" if "period=month" in url else "week" if "period=week" in url else "day"
        if traffic is None:
            payload = empty_traffic
        elif isinstance(traffic, dict) and "series" in traffic:
            payload = traffic
        else:
            payload = traffic.get(period, empty_traffic)
        route.fulfill(status=traffic_status, content_type="application/json", body=json.dumps(payload))

    page.route("**/api/nodes/*/traffic*", traffic_route)

    # 深拷贝：PATCH 场景会原地改写 enabled，不能影响其他 page 的基线数据
    targets_store = json.loads(json.dumps(targets)) if targets is not None else []

    def csrf_route(route):
        route.fulfill(status=200, content_type="application/json", headers={"X-CSRF-Token": csrf_token}, body="{}")

    def targets_collection_route(route):
        request = route.request
        if request.method == "GET":
            if targets_status != 200:
                route.fulfill(status=targets_status, content_type="application/json", body='{"error": "targets unavailable"}')
                return
            route.fulfill(status=200, content_type="application/json", body=json.dumps(list(targets_store)))
            return
        if request.method == "POST":
            body = json.loads(request.post_data or "{}")
            for item in targets_store:
                if item.get("id") == body.get("id"):
                    route.fulfill(status=409, content_type="application/json", body=json.dumps({"error": "target already exists"}))
                    return
            created = dict(body)
            created.setdefault("enabled", True)
            targets_store.append(created)
            route.fulfill(status=201, content_type="application/json", body=json.dumps(created))
            return
        route.fulfill(status=405, content_type="application/json", body=json.dumps({"error": "method not allowed"}))

    def targets_item_route(route):
        request = route.request
        target_id = request.url.rstrip("/").split("/")[-1]
        if request.method == "PATCH":
            body = json.loads(request.post_data or "{}")
            for item in targets_store:
                if item.get("id") == target_id:
                    item["enabled"] = bool(body.get("enabled", not item.get("enabled", True)))
                    route.fulfill(status=200, content_type="application/json", body=json.dumps(item))
                    return
            route.fulfill(status=404, content_type="application/json", body=json.dumps({"error": "target not found"}))
            return
        if request.method == "DELETE":
            targets_store[:] = [item for item in targets_store if item.get("id") != target_id]
            route.fulfill(status=204, body="")
            return
        route.fulfill(status=405, content_type="application/json", body=json.dumps({"error": "method not allowed"}))

    def registration_route(route):
        if registration is None:
            route.fulfill(status=404, content_type="application/json", body=json.dumps({"error": "not found"}))
            return
        route.fulfill(status=200, content_type="application/json", body=json.dumps(registration))

    page.route("**/api/csrf", csrf_route)
    page.route("**/api/targets/*", targets_item_route)
    page.route("**/api/targets", targets_collection_route)
    page.route("**/api/registration-tokens", registration_route)


def run_regressions():
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        tests = []
        node = {"id": "node-1", "uuid": "uuid-1", "name": "测试节点", "status": "online", "last_reported_at": "2026-09-20T00:00:00Z", "resource": {"cpu_percent": 12.5, "memory_total_bytes": 100, "memory_used_bytes": 50}}
        base_overview = {"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100, "avg_latency_ms": 42.7}, "resources": {"network_rx_bytes": 10, "network_tx_bytes": 20, "network_rx_bytes_delta": 300, "network_tx_bytes_delta": 100, "network_history": [10, 20], "cpu_percent": 33.5, "memory_used_bytes": 4, "memory_total_bytes": 10}}
        base_alerts = [{"id": "alert-1", "severity": "warning", "title": "测试告警", "message": "测试告警详情", "status": "open", "last_seen": "2026-09-20T00:00:00Z", "occurrence_count": 1}]

        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item").nth(3).click(force=True)
        page.locator(".subpage").wait_for()
        assert page.locator(".subpage").count() == 1
        assert page.locator(".side-nav").count() == 1
        tests.append("admin-console")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview)
        page.goto(BASE_URL, wait_until="networkidle")
        # 顶栏：当前时间、自动刷新开关、间隔选择、最后同步
        clock_text = page.locator(".topbar-clock b").inner_text()
        assert re.match(r"^\d{1,2}:\d{2}:\d{2}$", clock_text), f"unexpected clock text: {clock_text}"
        toggle = page.locator(".refresh-toggle")
        assert toggle.get_attribute("aria-pressed") == "true"
        interval = page.locator(".refresh-interval")
        assert interval.locator("option").count() == 3
        assert [interval.locator("option").nth(i).get_attribute("value") for i in range(3)] == ["10", "30", "60"]
        assert len(page.locator(".topbar .last-sync b").inner_text()) >= 1
        toggle.click()
        assert toggle.get_attribute("aria-pressed") == "false"
        interval.select_option("10")
        # 总览：6 个统计卡 + 右侧排行卡
        assert page.locator(".stat-card").count() == 6
        assert page.locator(".rank-card").count() == 3
        assert page.locator(".recent-alerts-panel").count() == 1
        headers = page.locator(".node-table thead th")
        assert headers.count() == 10
        assert headers.nth(6).inner_text() == "网络 ↓/↑"
        assert headers.nth(7).inner_text() == "丢包率"
        assert headers.nth(8).inner_text() == "最后心跳"
        assert page.locator(".node-table tbody tr").count() == 1
        tests.append("topbar-refresh-overview")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        assert page.locator(".node-drawer").get_attribute("role") == "dialog"
        page.keyboard.press("Escape")
        assert page.locator(".node-drawer").count() == 0
        tests.append("dialog")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview)
        page.goto(BASE_URL, wait_until="networkidle")
        # 桌面端折叠成纯图标窄栏
        assert "sidebar-collapsed" not in (page.locator(".sidebar").get_attribute("class") or "")
        page.locator(".collapse-toggle").click()
        assert "sidebar-collapsed" in (page.locator(".sidebar").get_attribute("class") or "")
        page.locator(".collapse-toggle").click()
        assert "sidebar-collapsed" not in (page.locator(".sidebar").get_attribute("class") or "")
        tests.append("sidebar-collapse")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview)
        page.goto(BASE_URL, wait_until="networkidle")
        # 移动端（<768px）：表格隐藏，卡片展示且可打开抽屉
        page.set_viewport_size({"width": 390, "height": 844})
        assert not page.locator(".node-table-wrap").is_visible()
        card = page.locator(".node-card").first
        card.wait_for()
        assert card.is_visible()
        assert "测试节点" in card.inner_text()
        assert card.locator(".progress-track").count() == 3
        card.click()
        assert page.locator(".node-drawer").get_attribute("role") == "dialog"
        page.keyboard.press("Escape")
        tests.append("node-table-mobile-cards")
        page.close()

        page = browser.new_page()
        install_api(
            page,
            me_status=401,
            public_status={"nodes": {"online": 1, "total": 3, "names": ["edge-01", "edge-02 [已脱敏]"]}, "checks": {"success_rate": 98.5, "avg_latency_ms": 42.7}, "last_updated_at": "2026-09-22T00:00:00Z", "generated_at": "2026-09-22T00:01:00Z"},
        )
        page.goto(BASE_URL, wait_until="networkidle")
        guest = page.locator(".guest-shell")
        guest.wait_for()
        assert guest.get_by_text("1 / 3", exact=True).count() == 1
        assert guest.get_by_text("42.7 ms", exact=True).count() == 1
        assert guest.get_by_text("98.5%", exact=True).count() == 1
        assert guest.get_by_text("edge-01", exact=True).count() == 1
        assert guest.get_by_text("全部正常", exact=True).count() == 1
        assert guest.locator(".guest-stats-bar").count() == 1
        assert guest.locator(".guest-node-card").count() == 2
        assert page.locator(".side-nav").count() == 0
        assert page.locator(".node-row").count() == 0
        assert page.locator(".alert-item").count() == 0
        assert page.locator(".node-drawer").count() == 0
        assert guest.get_by_role("button", name="使用 GitHub 登录", exact=True).count() == 1
        tests.append("guest-view")
        page.close()

        page = browser.new_page()
        install_api(page, me_status=401, public_status={"nodes": {"online": 1, "total": 2, "names": ["edge-01"]}, "checks": {"success_rate": None, "avg_latency_ms": None}, "last_updated_at": None, "generated_at": "2026-09-22T00:01:00Z"})
        page.goto(BASE_URL, wait_until="networkidle")
        guest = page.locator(".guest-shell")
        guest.wait_for()
        assert guest.get_by_text("部分异常", exact=True).count() == 1
        tests.append("guest-view-degraded")
        page.close()

        page = browser.new_page()
        install_api(page, me_status=401, public_status=None)
        page.goto(BASE_URL, wait_until="networkidle")
        guest = page.locator(".guest-shell")
        guest.wait_for()
        assert guest.get_by_text("—", exact=True).count() >= 1
        assert page.locator(".side-nav").count() == 0
        tests.append("guest-view-empty")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[])
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".api-state").get_by_text("API 返回空节点数组，暂无节点数据。", exact=True).first.wait_for()
        assert page.locator(".api-state").get_by_text("API 返回空节点数组，暂无节点数据。", exact=True).count() == 1
        tests.append("empty-api")
        page.close()

        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        page.locator(".drawer-button").click()
        assert page.locator(".node-detail-page").count() == 1
        assert page.locator(".ring-gauge").count() == 3
        assert page.get_by_text("12.5%").count() >= 1
        tests.append("node-detail")
        page.close()

        page = browser.new_page()
        checks = [
            {"target_id": "t-1", "name": "edge-http", "kind": "http", "host": "192.0.2.10", "total": 100, "success": 98, "failure": 2, "loss_rate": 0.02, "latency_avg_ms": 12.5, "jitter_ms": 1.2, "last_checked_at": "2026-09-21T12:00:00Z"},
            {"target_id": "t-2", "name": "edge-icmp", "kind": "icmp", "host": "192.0.2.11", "total": 50, "success": 50, "failure": 0, "loss_rate": 0.0, "latency_avg_ms": 8.0, "jitter_ms": None, "last_checked_at": "2026-09-21T12:05:00Z"},
            {"target_id": "t-3", "name": "edge-idle", "kind": "tcp", "host": "192.0.2.12"},
        ]
        day_series = [{"time": f"2026-09-21T{hour:02d}:00:00Z", "rx_bytes": None if hour % 3 == 0 else 1000000 * (hour + 1), "tx_bytes": None if hour % 4 == 0 else 500000 * (hour + 1)} for hour in range(24)]
        traffic_by_period = {
            "day": {"period": "day", "window": {"from": "2026-09-21T00:00:00Z", "to": "2026-09-22T00:00:00Z"}, "rx_bytes": 1500000000, "tx_bytes": 250000000, "rx_resets": 1, "tx_resets": 0, "interval_seconds": 3600, "series": day_series},
            "week": {"period": "week", "window": {"from": "2026-09-15T00:00:00Z", "to": "2026-09-22T00:00:00Z"}, "rx_bytes": 3000000000, "tx_bytes": 500000000, "rx_resets": 2, "tx_resets": 0, "interval_seconds": 86400, "series": [{"time": f"2026-09-{15 + day_offset:02d}T00:00:00Z", "rx_bytes": 400000000 * (day_offset + 1), "tx_bytes": 70000000 * (day_offset + 1)} for day_offset in range(7)]},
            "month": {"period": "month", "window": {"from": "2026-08-23T00:00:00Z", "to": "2026-09-22T00:00:00Z"}, "rx_bytes": None, "tx_bytes": None, "rx_resets": 0, "tx_resets": 0, "interval_seconds": 86400, "series": []},
        }
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview, checks=checks, traffic=traffic_by_period)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        drawer = page.locator(".node-drawer")
        drawer.get_by_text("资源摘要", exact=True).wait_for()
        assert drawer.get_by_text("打开完整详情", exact=True).count() == 1
        assert drawer.locator(".checks-table").count() == 0
        drawer.locator(".drawer-button").click()
        detail = page.locator(".node-detail-page")
        assert detail.count() == 1
        table = detail.locator(".checks-table")
        table.wait_for()
        rows = table.locator("tbody tr")
        assert rows.count() == 3
        first_row = rows.nth(0)
        assert first_row.locator("td").nth(0).inner_text() == "edge-http"
        assert first_row.locator("td").nth(1).inner_text() == "http"
        assert first_row.locator("td").nth(2).inner_text() == "100"
        assert first_row.locator("td").nth(3).inner_text() == "98"
        assert first_row.locator("td").nth(4).inner_text() == "2"
        assert first_row.locator("td").nth(5).inner_text() == "2%"
        assert first_row.locator("td").nth(6).inner_text() == "12.5"
        assert "checks-row-warning" in (first_row.get_attribute("class") or "")
        second_row = rows.nth(1)
        assert second_row.locator("td").nth(0).inner_text() == "edge-icmp"
        assert second_row.locator("td").nth(5).inner_text() == "0%"
        assert second_row.locator("td").nth(7).inner_text() == "—"
        assert "checks-row-warning" not in (second_row.get_attribute("class") or "")
        third_row = rows.nth(2)
        assert third_row.locator("td").nth(0).inner_text() == "edge-idle"
        assert third_row.locator("td").nth(3).inner_text() == "—"
        assert third_row.locator("td").nth(5).inner_text() == "—"
        assert "checks-row-warning" not in (third_row.get_attribute("class") or "")
        assert detail.get_by_text("1.4 GB", exact=True).count() >= 1
        assert detail.get_by_text("238 MB", exact=True).count() >= 1
        assert detail.get_by_text("计数器重置 1 次", exact=True).count() == 1
        assert detail.locator(".traffic-bar-rx").count() >= 1
        assert detail.locator(".traffic-bar-tx").count() >= 1
        week_button = detail.get_by_role("button", name="周", exact=True)
        week_button.click()
        detail.get_by_text("2.8 GB", exact=True).first.wait_for()
        assert "filter-active" in (week_button.get_attribute("class") or "")
        assert detail.get_by_text("计数器重置 2 次", exact=True).count() == 1
        # 节点信息区（OS/内核/架构/Agent 版本/开始时间）
        for label in ("操作系统", "内核", "架构", "Agent 版本", "开始时间"):
            assert detail.get_by_text(label, exact=True).count() == 1
        tests.append("node-analytics")
        page.close()

        page = browser.new_page()
        install_api(
            page,
            nodes=[node],
            alerts=base_alerts,
            overview=base_overview,
            checks_status=503,
            traffic_status=503,
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        page.locator(".drawer-button").click()
        detail = page.locator(".node-detail-page")
        detail.get_by_text("暂无数据", exact=True).first.wait_for()
        assert detail.get_by_text("暂无数据", exact=True).count() >= 2
        assert page.locator(".api-state").count() == 0
        tests.append("node-analytics-failure")
        page.close()

        page = browser.new_page()
        media_payload = [
            {"detector_id": "det-1", "checked_at": "2026-09-21T12:00:00Z", "result": {"detector": "netflix", "status": "available", "region": "US", "latency_ms": 120, "reason": "region matched"}},
            {"detector_id": "det-2", "checked_at": "2026-09-21T12:00:00Z", "result": {"detector": "disney", "status": "unavailable", "reason": "403 forbidden"}},
            {"detector_id": "det-3", "checked_at": "2026-09-21T12:00:00Z", "result": {"detector": "youtube", "status": "timeout", "reason": "context deadline exceeded"}},
        ]
        install_api(
            page,
            nodes=[node, {"id": "node-2", "uuid": "uuid-2", "name": "备用节点", "status": "online", "resource": {}}],
            alerts=[],
            overview={"nodes": {"online": 2, "total": 2, "resource_reporting": 2}, "checks": {"success_rate": 100}, "resources": {}},
            media={"uuid-1": media_payload},
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item").nth(4).click(force=True)
        table = page.locator(".media-table")
        table.wait_for()
        header = table.locator("thead th")
        assert header.count() == 5
        assert header.nth(1).inner_text() == "netflix"
        assert header.nth(3).inner_text() == "youtube"
        rows = table.locator("tbody tr")
        assert rows.count() == 2
        first_row = rows.nth(0)
        cells = first_row.locator("td")
        assert cells.nth(0).inner_text() == "测试节点"
        assert "media-cell-available" in (cells.nth(1).get_attribute("class") or "")
        assert "US" in cells.nth(1).inner_text()
        assert cells.nth(1).get_attribute("title") == "region matched"
        assert "media-cell-unavailable" in (cells.nth(2).get_attribute("class") or "")
        assert cells.nth(2).get_attribute("title") == "403 forbidden"
        assert "media-cell-warning" in (cells.nth(3).get_attribute("class") or "")
        assert cells.nth(3).get_attribute("title") == "context deadline exceeded"
        assert cells.nth(4).inner_text() != "—"
        second_row = rows.nth(1)
        assert second_row.locator("td").nth(0).inner_text() == "备用节点"
        assert second_row.get_by_text("暂无上报", exact=True).count() == 1
        tests.append("media-matrix")
        page.close()

        page = browser.new_page()
        install_api(
            page,
            nodes=[node],
            alerts=[],
            overview={"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100}, "resources": {}},
            media=[],
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item").nth(4).click(force=True)
        media_empty = page.locator(".subpage .empty-state")
        media_empty.get_by_text("暂无流媒体上报", exact=True).wait_for()
        assert page.locator(".media-table").count() == 0
        tests.append("media-empty")
        page.close()

        base_targets = [
            {"id": "tcp-edge-01", "name": "边缘 TCP", "kind": "tcp", "host": "192.0.2.10", "port": 80, "interval_seconds": 60, "timeout_ms": 3000, "enabled": True},
            {"id": "http-edge-01", "name": "边缘 HTTP", "kind": "http", "host": "192.0.2.11", "port": 80, "path": "/health", "interval_seconds": 30, "timeout_ms": 3000, "enabled": False},
            {"id": "mtr-edge-01", "name": "边缘 MTR", "kind": "mtr", "host": "192.0.2.12", "port": 80, "max_hops": 20, "interval_seconds": 300, "timeout_ms": 3000, "enabled": True},
        ]

        # 检测目标管理：列表 / 筛选 / 创建（成功 + 服务端 409 文案）/ 启停 / 删除
        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview, targets=base_targets)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item", has_text="检测目标").click(force=True)
        table = page.locator(".target-table")
        table.wait_for()
        headers = table.locator("thead th")
        assert headers.count() == 9
        assert [headers.nth(i).inner_text() for i in range(8)] == ["ID", "名称", "类型", "主机", "端口", "路径", "间隔", "启用"]
        rows = table.locator("tbody tr")
        assert rows.count() == 3
        first_row = rows.nth(0)
        assert first_row.locator("td").nth(0).inner_text() == "tcp-edge-01"
        assert first_row.locator("td").nth(2).inner_text() == "TCP"
        assert first_row.locator("td").nth(4).inner_text() == "80"
        assert first_row.locator("td").nth(5).inner_text() == "—"
        assert first_row.locator("td").nth(6).inner_text() == "60s"
        assert first_row.locator(".target-toggle").inner_text() == "已启用"
        # 按类型筛选
        page.get_by_role("button", name="HTTP", exact=True).click()
        assert table.locator("tbody tr").count() == 1
        assert table.locator("tbody tr").nth(0).locator("td").nth(0).inner_text() == "http-edge-01"
        page.get_by_role("button", name="全部", exact=True).click()
        assert table.locator("tbody tr").count() == 3
        # 创建目标（http 类型，路径必填字段出现）
        page.get_by_role("button", name="新建目标", exact=True).click()
        form = page.locator(".target-form-panel")
        form.wait_for()
        page.locator(".target-form-grid select").first.select_option("http")
        page.get_by_placeholder("例如：上海 HTTP 探测").fill("新建 HTTP")
        page.get_by_placeholder("例如：http-sh-01").fill("http-new-01")
        page.get_by_placeholder("域名或 IP，不含协议").fill("192.0.2.30")
        page.get_by_placeholder("/health").fill("/status")
        page.get_by_role("button", name="创建目标", exact=True).click()
        page.locator(".api-state", has_text="已创建").wait_for()
        assert table.locator("tbody tr").count() == 4
        # 重复 ID → 服务端 error 文案原样展示
        page.locator(".target-form-grid select").first.select_option("http")
        page.get_by_placeholder("例如：上海 HTTP 探测").fill("重复目标")
        page.get_by_placeholder("例如：http-sh-01").fill("http-new-01")
        page.get_by_placeholder("域名或 IP，不含协议").fill("192.0.2.30")
        page.get_by_placeholder("/health").fill("/status")
        page.get_by_role("button", name="创建目标", exact=True).click()
        form_error = page.locator(".target-form-panel .api-state-error")
        form_error.wait_for()
        assert "target already exists" in form_error.inner_text()
        # 启停切换（PATCH enabled）
        rows.nth(0).locator(".target-toggle").click()
        rows.nth(0).locator(".target-toggle").get_by_text("已停用", exact=True).wait_for()
        # 删除目标
        target_row = table.locator("tbody tr", has_text="http-new-01")
        target_row.locator(".target-delete").click()
        page.locator(".api-state", has_text="已删除").wait_for()
        assert table.locator("tbody tr").count() == 3
        tests.append("targets-manage")
        page.close()

        # network / mtr 只读表格视图（复用 TargetTable，按 kind 过滤，无操作按钮）
        page = browser.new_page()
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview, targets=base_targets)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item").nth(2).click(force=True)
        readonly_table = page.locator(".target-table")
        readonly_table.wait_for()
        assert readonly_table.locator("thead th").count() == 8
        readonly_rows = readonly_table.locator("tbody tr")
        assert readonly_rows.count() == 2
        assert readonly_rows.nth(0).locator("td").nth(0).inner_text() == "tcp-edge-01"
        assert readonly_rows.nth(1).locator("td").nth(5).inner_text() == "/health"
        assert readonly_rows.nth(0).get_by_text("已启用", exact=True).count() == 1
        assert page.locator(".target-delete").count() == 0
        assert page.locator(".target-toggle").count() == 0
        page.locator(".nav-item").nth(3).click(force=True)
        page.get_by_text("MTR 路由目标", exact=True).wait_for()
        mtr_rows = page.locator(".target-table tbody tr")
        assert mtr_rows.count() == 1
        assert mtr_rows.nth(0).locator("td").nth(0).inner_text() == "mtr-edge-01"
        assert mtr_rows.nth(0).locator("td").nth(2).inner_text() == "MTR"
        tests.append("targets-readonly")
        page.close()

        # 节点自助接入：生成注册命令、倒计时、复制反馈
        registration = {
            "registration_token": "reg-token-123",
            "endpoint": "https://probewatch.example.com/api/agent/v1",
            "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=900)).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        page = browser.new_page()
        page.context.grant_permissions(["clipboard-read", "clipboard-write"], origin=BASE_URL.rstrip("/"))
        install_api(page, nodes=[node], alerts=base_alerts, overview=base_overview, registration=registration)
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".nav-item").nth(1).click(force=True)
        enroll_panel = page.locator(".enroll-panel")
        assert enroll_panel.get_by_text("15 分钟", exact=False).count() == 1
        page.get_by_role("button", name="生成接入命令", exact=True).click()
        enroll_script = page.locator(".enroll-script")
        enroll_script.wait_for()
        script_text = enroll_script.inner_text()
        assert 'export PROBEWATCH_AGENT_ENDPOINT="https://probewatch.example.com/api/agent/v1"' in script_text
        assert 'export PROBEWATCH_AGENT_REGISTRATION_TOKEN="reg-token-123"' in script_text
        assert "PROBEWATCH_AGENT_NODE_UUID=" in script_text
        assert re.search(r'PROBEWATCH_AGENT_NODE_UUID="[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"', script_text)
        assert "./probewatch-agent" in script_text
        assert re.match(r"剩余 \d{2}:\d{2}", page.locator(".enroll-countdown").inner_text())
        warning = page.locator(".enroll-warning").inner_text()
        assert "15 分钟" in warning and "仅可注册一个节点" in warning
        page.get_by_role("button", name="复制安装命令", exact=True).click()
        page.get_by_role("button", name="已复制", exact=True).wait_for()
        clipboard_text = page.evaluate("() => navigator.clipboard.readText()")
        assert 'export PROBEWATCH_AGENT_REGISTRATION_TOKEN="reg-token-123"' in clipboard_text
        tests.append("node-enroll")
        page.close()
        browser.close()
    print(f"{len(tests)} browser regression tests passed")


if __name__ == "__main__":
    run_regressions()
