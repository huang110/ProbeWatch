from playwright.sync_api import sync_playwright


BASE_URL = __import__("os").environ.get("PROBEWATCH_BASE_URL", "http://127.0.0.1:4173/")


def install_api(page, *, me_status=200, nodes_status=200, nodes=None, me=None, alerts=None, alerts_status=200, overview=None, overview_status=200, public_status=None, checks=None, checks_status=200, traffic=None, traffic_status=200, media=None, media_status=200):
    json = __import__("json")
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
        assert page.locator(".side-nav").count() == 1
        tests.append("admin-console")
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
        assert page.locator(".side-nav").count() == 0
        assert page.locator(".node-row").count() == 0
        assert page.locator(".alert-item").count() == 0
        assert page.locator(".node-drawer").count() == 0
        assert guest.get_by_role("button", name="使用 GitHub 登录", exact=True).count() == 1
        tests.append("guest-view")
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
        install_api(
            page,
            nodes=[node],
            alerts=[{"id": "alert-1", "severity": "warning", "title": "测试告警", "message": "测试告警详情", "status": "open", "last_seen": "2026-09-20T00:00:00Z", "occurrence_count": 1}],
            overview={"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100}, "resources": {"network_rx_bytes": 10, "network_tx_bytes": 20, "network_history": [10, 20]}},
            checks=checks,
            traffic=traffic_by_period,
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        drawer = page.locator(".node-drawer")
        table = drawer.locator(".checks-table")
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
        assert drawer.get_by_text("1.4 GB", exact=True).count() >= 1
        assert drawer.get_by_text("238 MB", exact=True).count() >= 1
        assert drawer.get_by_text("计数器重置 1 次", exact=True).count() == 1
        assert drawer.locator(".traffic-bar-rx").count() >= 1
        assert drawer.locator(".traffic-bar-tx").count() >= 1
        week_button = drawer.get_by_role("button", name="周", exact=True)
        week_button.click()
        drawer.get_by_text("2.8 GB", exact=True).first.wait_for()
        assert "filter-active" in (week_button.get_attribute("class") or "")
        assert drawer.get_by_text("计数器重置 2 次", exact=True).count() == 1
        drawer.locator(".drawer-button").click()
        assert page.locator(".node-detail-page").count() == 1
        assert page.locator(".node-detail-page .checks-table").count() == 1
        assert page.get_by_text("2.8 GB", exact=True).count() >= 1
        tests.append("node-analytics")
        page.close()

        page = browser.new_page()
        install_api(
            page,
            nodes=[node],
            alerts=[{"id": "alert-1", "severity": "warning", "title": "测试告警", "message": "测试告警详情", "status": "open", "last_seen": "2026-09-20T00:00:00Z", "occurrence_count": 1}],
            overview={"nodes": {"online": 1, "total": 1, "resource_reporting": 1}, "checks": {"success_rate": 100}, "resources": {"network_rx_bytes": 10, "network_tx_bytes": 20, "network_history": [10, 20]}},
            checks_status=503,
            traffic_status=503,
        )
        page.goto(BASE_URL, wait_until="networkidle")
        page.locator(".node-row").first.click()
        drawer = page.locator(".node-drawer")
        drawer.get_by_text("暂无数据", exact=True).first.wait_for()
        assert drawer.get_by_text("暂无数据", exact=True).count() >= 2
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
        browser.close()
    print(f"{len(tests)} browser regression tests passed")


if __name__ == "__main__":
    run_regressions()
