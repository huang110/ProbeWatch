package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/probewatch/probewatch/internal/version"
)

var (
	openapiOnce sync.Once
	openapiJSON []byte
)

func (s *Server) openAPIJSONHandler(w http.ResponseWriter, r *http.Request) {
	openapiOnce.Do(func() {
		spec := buildOpenAPISpec()
		data, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			openapiJSON = []byte(`{"openapi":"3.0.3","info":{"title":"ProbeWatch API","version":"0.6.2"}}`)
			return
		}
		openapiJSON = data
	})

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiJSON)
}

func (s *Server) swaggerUIHandler(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <title>ProbeWatch 开放接口与开发者平台 (OpenAPI / Swagger)</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='%236366f1'><path d='M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5'/></svg>" />
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #090a0f; color: #f1f5f9; font-family: system-ui, -apple-system, sans-serif; }
    .topbar { display: none !important; }
    .swagger-ui .info { margin: 24px 0; }
    .swagger-ui .info .title { color: #f8fafc; font-size: 26px; }
    .swagger-ui .info p, .swagger-ui .info li { color: #94a3b8; }
    .swagger-ui .scheme-container { background: #13151f; border-bottom: 1px solid #1e2235; box-shadow: none; padding: 16px 0; }
    .swagger-ui .opblock-tag { color: #e2e8f0; border-bottom: 1px solid #1e2235; font-size: 18px; }
    .swagger-ui .opblock { border-radius: 8px; box-shadow: none; border: 1px solid #1e2235; background: #0f111a; }
    .swagger-ui .opblock .opblock-summary-path { color: #f8fafc; }
    .swagger-ui .opblock .opblock-summary-description { color: #94a3b8; }
    .swagger-ui section.models { border: 1px solid #1e2235; border-radius: 8px; background: #0f111a; }
    .swagger-ui section.models h4 { color: #e2e8f0; }
    .swagger-ui .model-box { background: #13151f; }
    .swagger-ui table thead tr td, .swagger-ui table thead tr th { color: #cbd5e1; border-bottom: 1px solid #1e2235; }
    .swagger-ui .btn.authorize { color: #6366f1; border-color: #6366f1; }
    .swagger-ui .btn.authorize svg { fill: #6366f1; }
    .swagger-ui .dialog-ux .modal-ux { background: #13151f; border: 1px solid #2d334d; border-radius: 12px; }
    .swagger-ui .dialog-ux .modal-ux-header h3 { color: #f8fafc; }
    .swagger-ui .dialog-ux .modal-ux-content p { color: #94a3b8; }
    .header-bar { background: #13151f; border-bottom: 1px solid #1e2235; padding: 12px 24px; display: flex; align-items: center; justify-content: space-between; }
    .brand { font-size: 16px; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 8px; }
    .brand-tag { background: rgba(99, 102, 241, 0.2); color: #818cf8; font-size: 12px; padding: 2px 8px; border-radius: 12px; font-weight: 600; }
    .nav-links a { color: #94a3b8; text-decoration: none; font-size: 13px; margin-left: 16px; transition: color 0.2s; }
    .nav-links a:hover { color: #fff; }
  </style>
</head>
<body>
  <div class="header-bar">
    <div class="brand">
      <span>ProbeWatch API 开发者中心</span>
      <span class="brand-tag">v` + version.ServerVersion + `</span>
    </div>
    <div class="nav-links">
      <a href="/api/openapi.json" target="_blank">查看 OpenAPI 原始 JSON</a>
      <a href="/#/">返回监控大屏</a>
      <a href="/#/settings">控制台设置</a>
    </div>
  </div>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/api/openapi.json",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "BaseLayout",
        defaultModelsExpandDepth: 1,
        defaultModelExpandDepth: 1,
        docExpansion: "list"
      });
    };
  </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func buildOpenAPISpec() map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "ProbeWatch 探针与集群值守控制面 OpenAPI",
			"version":     version.ServerVersion,
			"description": "ProbeWatch 专为多节点、分布式 VPS 实例设计的实时监控与巡检运维系统 RESTful API。支持通过个人访问令牌 (Personal Access Token / PAT) 或会话 Cookie 认证。",
			"contact": map[string]interface{}{
				"name": "ProbeWatch",
				"url":  "https://github.com/huang110/ProbeWatch",
			},
		},
		"servers": []map[string]interface{}{
			{"url": "/", "description": "当前 ProbeWatch 主控实例"},
		},
		"security": []map[string]interface{}{
			{"BearerAuth": []string{}},
			{"ApiKeyAuth": []string{}},
		},
		"paths": map[string]interface{}{
			"/api/public/version": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Public 公开接口"},
					"summary":     "获取主控与探针版本信息",
					"description": "无需认证，返回当前主控版本、推荐探针版本及最低兼容探针版本。",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "版本信息"},
					},
				},
			},
			"/api/public/status": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Public 公开接口"},
					"summary":     "获取大屏公开只读节点状态概览",
					"description": "无需认证，返回向访客展示的脱敏节点名录与在线健康度。",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "节点状态概览"},
					},
				},
			},
			"/api/me": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Auth 认证与个人中心"},
					"summary":     "获取当前认证上下文与权限",
					"description": "返回当前登录用户或 PAT 令牌的角色、节点白名单范围及读写权限。",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "用户信息与角色作用域"},
						"401": map[string]interface{}{"description": "未认证"},
					},
				},
			},
			"/api/nodes": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Nodes 节点管理"},
					"summary":     "获取受控探针节点列表",
					"description": "基于调用者 allowed_nodes 作用域过滤返回节点列表及最新资源负载。",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "节点列表"},
					},
				},
			},
			"/api/nodes/{uuid}": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Nodes 节点管理"},
					"summary":     "获取单个探针节点详细信息",
					"parameters": []map[string]interface{}{
						{"name": "uuid", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "节点详情"},
						"404": map[string]interface{}{"description": "节点不存在"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Nodes 节点管理"},
					"summary":     "下线并移除指定探针节点 (需写权限)",
					"parameters": []map[string]interface{}{
						{"name": "uuid", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "节点已成功删除"},
						"403": map[string]interface{}{"description": "权限不足"},
					},
				},
			},
			"/api/nodes/{uuid}/checks/summary": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Metrics 时序与监测"},
					"summary":     "获取节点网络探测延迟与丢包统计汇总",
					"parameters": []map[string]interface{}{
						{"name": "uuid", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "range", "in": "query", "schema": map[string]interface{}{"type": "string", "default": "1h"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "延迟与丢包统计"},
					},
				},
			},
			"/api/alerts": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Alerts 告警中枢"},
					"summary":     "获取当前活跃异常告警列表",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "告警列表"},
					},
				},
			},
			"/api/alerts/{id}/ack": map[string]interface{}{
				"post": map[string]interface{}{
					"tags":        []string{"Alerts 告警中枢"},
					"summary":     "确认告警 (ACK)",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "告警已确认"},
					},
				},
			},
			"/api/tokens": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Tokens 开发者 API 密钥"},
					"summary":     "获取 Personal Access Tokens (PAT) 列表",
					"description": "超级管理员可查看全量令牌，其他角色仅可查看自身创建的令牌。",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "令牌列表"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Tokens 开发者 API 密钥"},
					"summary":     "创建新的 Personal Access Token (PAT)",
					"description": "生成高强度自动化访问令牌，明文仅在创建时返回一次。",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"required": []string{"name"},
									"properties": map[string]interface{}{
										"name":            map[string]interface{}{"type": "string", "example": "Prometheus Exporter"},
										"role":            map[string]interface{}{"type": "string", "enum": []string{"admin", "operator", "viewer"}, "default": "operator"},
										"scopes":          map[string]interface{}{"type": "string", "example": "*"},
										"allowed_nodes":   map[string]interface{}{"type": "string", "example": "*"},
										"expires_in_days": map[string]interface{}{"type": "integer", "example": 90},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "令牌创建成功，包含一次性明文 raw_token"},
					},
				},
			},
			"/api/tokens/{id}": map[string]interface{}{
				"put": map[string]interface{}{
					"tags":        []string{"Tokens 开发者 API 密钥"},
					"summary":     "停用或启用 API 令牌",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"disabled": map[string]interface{}{"type": "boolean"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "更新成功"},
					},
				},
				"delete": map[string]interface{}{
					"tags":        []string{"Tokens 开发者 API 密钥"},
					"summary":     "彻底撤销并删除 API 令牌",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "令牌已删除"},
					},
				},
			},
			"/api/audit-logs": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Audit 安全审计"},
					"summary":     "查询系统敏感操作安全审计日志 (超级管理员专享)",
					"parameters": []map[string]interface{}{
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 50}},
						{"name": "offset", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 0}},
						{"name": "action", "in": "query", "schema": map[string]interface{}{"type": "string", "example": "token"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "审计记录列表"},
						"403": map[string]interface{}{"description": "非管理员拒绝访问"},
					},
				},
			},
			"/api/users": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Users 多租户与团队管理"},
					"summary":     "获取系统成员花名册 (超级管理员专享)",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成员列表"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Users 多租户与团队管理"},
					"summary":     "添加系统协作成员 (超级管理员专享)",
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "成员创建成功"},
					},
				},
			},
			"/api/public/status-page": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Status Page 公开服务状态页"},
					"summary":     "获取公开服务状态页完整数据 (含 90 天 SLA 历史)",
					"description": "无需认证，返回系统整体健康状态、活跃故障事件、计划维护通告与分组组件的 90 天可用率条形图数据。",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "状态页完整数据"},
					},
				},
			},
			"/api/public/incidents": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Status Page 公开服务状态页"},
					"summary":     "查询历史服务事件通告归档",
					"parameters": []map[string]interface{}{
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 20}},
						{"name": "offset", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 0}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "已解决历史事件列表"},
					},
				},
			},
			"/api/admin/status-page": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Status Page 公开服务状态页"},
					"summary":     "获取状态页配置与组件拓扑",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "状态页配置"},
					},
				},
				"put": map[string]interface{}{
					"tags":        []string{"Status Page 公开服务状态页"},
					"summary":     "更新状态页配置与组件映射 (管理员)",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "更新成功"},
					},
				},
			},
			"/api/admin/incidents": map[string]interface{}{
				"get": map[string]interface{}{
					"tags":        []string{"Status Page 公开服务状态页"},
					"summary":     "管理员列出所有事件与维护计划",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "事件列表"},
					},
				},
				"post": map[string]interface{}{
					"tags":        []string{"Status Page 公开服务状态页"},
					"summary":     "发布新事件通告或计划维护",
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "事件创建成功"},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"BearerAuth": map[string]interface{}{
					"type":        "http",
					"scheme":      "bearer",
					"description": "在 HTTP Header 中传递: Authorization: Bearer pbw_pat_...",
				},
				"ApiKeyAuth": map[string]interface{}{
					"type":        "apiKey",
					"in":          "header",
					"name":        "X-API-Key",
					"description": "在 HTTP Header 中传递: X-API-Key: pbw_pat_...",
				},
				"CookieAuth": map[string]interface{}{
					"type":        "apiKey",
					"in":          "cookie",
					"name":        "probewatch_session",
					"description": "通过控制台登录生成的安全 HTTP-Only Session Cookie",
				},
			},
		},
	}
}
