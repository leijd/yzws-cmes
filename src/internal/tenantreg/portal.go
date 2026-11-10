package tenantreg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/**
 * 制衣厂门户（体验用户与注册用户共用同一个页面，仅按钮文案与默认内容不同）：
 * - 公开端口：/public/tenant-portal?code=XXX 返回厂名、logo（各厂自己的，未设置用平台默认 logo）、
 *   版本类型（trial=免费体验版 / paid=正式版）、欢迎标题与欢迎语（各厂管理员可改，未设置用平台默认）、
 *   以及今日工单/在制订单/员工总数/累计工单等概览数据。
 * - 管理端口（需 settings:read / settings:write）：各厂管理员维护自己的 logo 与欢迎内容。
 * - logo 上传：PNG/GIF，≤500KB，直接存库并经 /public/tenant-logo 输出，不依赖静态文件目录。
 */

const (
	portalDefaultPaidTitle = "欢迎使用{tenant}生产管理系统"
	portalDefaultPaidText  = "这里是一智算制衣 MES 的工作入口：订单、排产、扫菲报工、计件工资一条线走完，随时掌握车间实时进度。"
	portalDefaultTrialText = "免费体验版已开通：这是一个真实的制衣厂数字化车间，订单、扫码、计件工资一网即成。您可以亲手把一个订单跑起来。"
	/* logo 文件大小上限：500KB */
	tenantLogoMaxBytes = 500 * 1024
)

/* logo 对各厂 logo 的公开访问地址；带版本号便于更新后立即刷新缓存 */
func tenantLogoURL(code string, version int64) string {
	return "/api/v1/public/tenant-logo?code=" + url.QueryEscape(code) + "&v=" + strconv.FormatInt(version, 10)
}

/* 按文件头识别 logo 格式：仅允许 PNG 与 GIF */
func tenantLogoContentType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	}
	return ""
}

func registerTenantPortal(api *httpapi.API) {
	/* 公开：本厂上传的 logo 图片（按厂编码输出，浏览器 <img> 直接引用） */
	api.Public("GET /api/v1/public/tenant-logo", func(w http.ResponseWriter, r *http.Request) error {
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if code == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "缺少制衣厂编码")
		}
		var data []byte
		var contentType string
		err := api.DB.QueryRow(r.Context(), `SELECT p.logo_data,COALESCE(p.logo_content_type,'') FROM tenant_portal p
			JOIN tenant t ON t.id=p.tenant_id
			WHERE t.code=$1 AND p.logo_data IS NOT NULL AND t.status='active'`, code).Scan(&data, &contentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "该制衣厂未上传 logo")
		}
		if err != nil {
			return err
		}
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return nil
	})

	/* 公开：门户信息（按厂编码，未登录也可访问，用于体验用户与登录后的门户页） */
	api.Public("GET /api/v1/public/tenant-portal", func(w http.ResponseWriter, r *http.Request) error {
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if code == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "缺少制衣厂编码")
		}
		var tenantID, name, shortCode, customerType string
		err := api.DB.QueryRow(r.Context(), `SELECT id,name,COALESCE(short_code,''),customer_type FROM tenant
			WHERE code=$1 AND status='active' AND registration_status='approved'`, code).
			Scan(&tenantID, &name, &shortCode, &customerType)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "制衣厂不存在或尚未启用")
		}
		if err != nil {
			return err
		}
		edition := "paid"
		if customerType == "trial" {
			edition = "trial"
		}
		var logoURL, welcomeTitle, welcomeText, logoType *string
		var portalVersion int64
		_ = api.DB.QueryRow(r.Context(), `SELECT logo_url,welcome_title,welcome_text,logo_content_type,version FROM tenant_portal WHERE tenant_id=$1`, tenantID).
			Scan(&logoURL, &welcomeTitle, &welcomeText, &logoType, &portalVersion)
		var trialEnds *int64
		_ = api.DB.QueryRow(r.Context(), `SELECT trial_ends_at FROM tenant_subscription WHERE tenant_id=$1 AND customer_type='trial'`, tenantID).
			Scan(&trialEnds)

		/* 概览数据：今日工单（今天创建的工单）、在制订单（未完成的工单）、员工总数、累计工单 */
		now := time.Now()
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMicro()
		var todayOrders, wipOrders, employees, totalOrders int64
		_ = api.DB.QueryRow(r.Context(), `SELECT
			(SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND created_at>=$2),
			(SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND status IN ('pending_release','in_production','paused')),
			(SELECT COUNT(*) FROM employee WHERE tenant_id=$1 AND status='active'),
			(SELECT COUNT(*) FROM work_order WHERE tenant_id=$1)`, tenantID, dayStart).
			Scan(&todayOrders, &wipOrders, &employees, &totalOrders)

		effTitle := strings.ReplaceAll(portalDefaultPaidTitle, "{tenant}", name)
		effText := portalDefaultPaidText
		if edition == "trial" {
			effTitle = "欢迎使用" + name + " · 一智算制衣"
			effText = portalDefaultTrialText
		}
		content := map[string]any{
			"welcome_title": effTitle,
			"welcome_text":  effText,
			"logo_url":      "",
			"customized":    false,
		}
		if welcomeTitle != nil && strings.TrimSpace(*welcomeTitle) != "" {
			content["welcome_title"] = strings.TrimSpace(*welcomeTitle)
			content["customized"] = true
		}
		if welcomeText != nil && strings.TrimSpace(*welcomeText) != "" {
			content["welcome_text"] = strings.TrimSpace(*welcomeText)
			content["customized"] = true
		}
		/* 上传的企业 logo 优先；未上传时使用图片网址；都没有则前端显示平台默认 logo */
		if logoType != nil && strings.TrimSpace(*logoType) != "" {
			content["logo_url"] = tenantLogoURL(code, portalVersion)
		} else if logoURL != nil {
			content["logo_url"] = strings.TrimSpace(*logoURL)
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"code": code, "name": name, "short_code": shortCode, "edition": edition,
			"trial_ends_at": trialEnds,
			"content":       content,
			"stats": map[string]any{"today_orders": todayOrders, "wip_orders": wipOrders,
				"employees": employees, "total_orders": totalOrders},
		})
	})

	/* 本厂管理员：查看门户内容（含平台默认值，便于“恢复默认”）；厂管理员或具备 settings:read 的用户可看 */
	api.Handle("GET /api/v1/tenant-portal/content", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		isAdmin, err := isTenantAdmin(r, api, p)
		if err != nil {
			return err
		}
		if !isAdmin && !p.Permissions["settings:read"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "仅本厂管理员可查看门户内容")
		}
		var code, name, customerType string
		if err := api.DB.QueryRow(r.Context(), `SELECT code,name,customer_type FROM tenant WHERE id=$1`, p.TenantID).
			Scan(&code, &name, &customerType); err != nil {
			return err
		}
		edition := "paid"
		defaultTitle := strings.ReplaceAll(portalDefaultPaidTitle, "{tenant}", name)
		defaultText := portalDefaultPaidText
		if customerType == "trial" {
			edition = "trial"
			defaultTitle = "欢迎使用" + name + " · 一智算制衣"
			defaultText = portalDefaultTrialText
		}
		var logoURL, systemName, welcomeTitle, welcomeText, logoType, styleCode *string
		var sections []byte
		var version int64
		err = api.DB.QueryRow(r.Context(), `SELECT logo_url,system_name,welcome_title,welcome_text,logo_content_type,style_code,sections,version FROM tenant_portal WHERE tenant_id=$1`, p.TenantID).
			Scan(&logoURL, &systemName, &welcomeTitle, &welcomeText, &logoType, &styleCode, &sections, &version)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hasUploadedLogo := logoType != nil && strings.TrimSpace(*logoType) != ""
		logoPreviewURL := deref(logoURL)
		if hasUploadedLogo {
			logoPreviewURL = tenantLogoURL(code, version)
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"tenant_name": name, "edition": edition,
			"logo_url": deref(logoURL), "logo_preview_url": logoPreviewURL, "has_uploaded_logo": hasUploadedLogo,
			"system_name": deref(systemName), "default_system_name": "一智算",
			"welcome_title": deref(welcomeTitle), "welcome_text": deref(welcomeText),
			"default_title": defaultTitle, "default_text": defaultText, "version": version,
			"style_code": deref(styleCode), "sections": parsePortalSections(sections),
		})
	})

	/* 门户视图（设计 5.8）：audience=manager 返回厂长自定义（风格/板块/文字）；audience=employee 固定模板 */
	api.Handle("GET /api/v1/tenant-portal/view", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		audience := strings.TrimSpace(r.URL.Query().Get("audience"))
		if audience == "" {
			audience = "employee"
		}
		if audience != "manager" && audience != "employee" {
			return httpapi.Error(http.StatusBadRequest, "INVALID_REQUEST", "audience 必须为 manager 或 employee")
		}
		var code, name string
		if err := api.DB.QueryRow(r.Context(), `SELECT code,name FROM tenant WHERE id=$1`, p.TenantID).Scan(&code, &name); err != nil {
			return err
		}
		if audience == "employee" {
			/* 员工门户：服务端固定模板，忽略厂长自定义（设计 6.5） */
			return httpapi.JSON(w, http.StatusOK, map[string]any{
				"audience": "employee", "style_code": "classic", "tenant_name": name,
				"sections": defaultPortalSections(),
			})
		}
		var styleCode *string
		var sections []byte
		_ = api.DB.QueryRow(r.Context(), `SELECT style_code,sections FROM tenant_portal WHERE tenant_id=$1`, p.TenantID).Scan(&styleCode, &sections)
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"audience": "manager", "style_code": deref(styleCode), "tenant_name": name,
			"sections": parsePortalSections(sections),
		})
	})

	/* 工作页面品牌：本厂 logo + 制衣厂系统名称（未配置时前端显示平台默认） */
	api.Handle("GET /api/v1/tenant/branding", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var code, tenantName string
		if err := api.DB.QueryRow(r.Context(), `SELECT code,name FROM tenant WHERE id=$1`, p.TenantID).Scan(&code, &tenantName); err != nil {
			return err
		}
		var logoURL, systemName, logoType *string
		var version int64
		_ = api.DB.QueryRow(r.Context(), `SELECT logo_url,system_name,logo_content_type,version FROM tenant_portal WHERE tenant_id=$1`, p.TenantID).
			Scan(&logoURL, &systemName, &logoType, &version)
		logo := deref(logoURL)
		if logoType != nil && strings.TrimSpace(*logoType) != "" {
			logo = tenantLogoURL(code, version)
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"tenant_name": tenantName, "system_name": deref(systemName), "logo_url": logo,
			"default_system_name": "一智算",
		})
	})

	/* 本厂管理员：保存门户内容（logo 网址 + 欢迎标题/欢迎语；留空表示恢复平台默认） */
	api.Handle("PUT /api/v1/tenant-portal/content", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		isAdmin, err := isTenantAdmin(r, api, p)
		if err != nil {
			return err
		}
		if !isAdmin && !p.Permissions["settings:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "仅本厂管理员可修改门户内容")
		}
		var in struct {
			LogoURL      string `json:"logo_url"`
			SystemName   string `json:"system_name"`
			WelcomeTitle string `json:"welcome_title"`
			WelcomeText  string `json:"welcome_text"`
			StyleCode    string `json:"style_code"`
			Sections     []struct {
				Key         string `json:"key"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Enabled     bool   `json:"enabled"`
			} `json:"sections"`
			ResetSections bool `json:"reset_sections"`
			/* 恢复平台默认时一并清除已上传的企业 logo */
			ResetLogo bool   `json:"reset_logo"`
			Reason    string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		logoURL := strings.TrimSpace(in.LogoURL)
		systemName := strings.TrimSpace(in.SystemName)
		title := strings.TrimSpace(in.WelcomeTitle)
		text := strings.TrimSpace(in.WelcomeText)
		if len(logoURL) > 500 || len(systemName) > 30 || len(title) > 60 || len(text) > 400 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "内容过长（系统名称 ≤30、logo 网址 ≤500、标题 ≤60、欢迎语 ≤400）")
		}
		if logoURL != "" && !strings.HasPrefix(logoURL, "http://") && !strings.HasPrefix(logoURL, "https://") && !strings.HasPrefix(logoURL, "/") {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "logo 地址需以 http(s):// 或 / 开头")
		}
		styleCode := strings.TrimSpace(in.StyleCode)
		if styleCode != "" && styleCode != "classic" && styleCode != "dark" && styleCode != "fresh" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "风格必须为 classic / dark / fresh")
		}
		/* 板块校验：键必须在允许集合内，文案限长（设计 6.5） */
		allowed := map[string]bool{"welcome": true, "overview": true, "entries": true, "progress": true}
		var sectionsJSON []byte
		if len(in.Sections) > 0 && !in.ResetSections {
			normalized := make([]map[string]any, 0, len(in.Sections))
			for _, s := range in.Sections {
				if !allowed[s.Key] {
					return httpapi.Error(http.StatusBadRequest, "INVALID_SECTION", "板块键必须是 welcome/overview/entries/progress 之一")
				}
				if len(s.Title) > 20 || len(s.Description) > 60 {
					return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "板块标题 ≤20 字、说明 ≤60 字")
				}
				normalized = append(normalized, map[string]any{
					"key": s.Key, "title": s.Title, "description": s.Description, "enabled": s.Enabled,
				})
			}
			sectionsJSON, _ = json.Marshal(normalized)
		}
		/* 填写图片网址即改用以网址为准；留空且未要求恢复默认时，保留已上传的 logo 文件 */
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO tenant_portal(tenant_id,logo_url,system_name,welcome_title,welcome_text,style_code,sections,updated_at,updated_by,version)
			VALUES($1,NULLIF($2,''),NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($9,''),$10,$6,$7,1)
			ON CONFLICT(tenant_id) DO UPDATE SET logo_url=EXCLUDED.logo_url,
			logo_data=CASE WHEN $8 OR NULLIF($2,'') IS NOT NULL THEN NULL ELSE tenant_portal.logo_data END,
			logo_content_type=CASE WHEN $8 OR NULLIF($2,'') IS NOT NULL THEN NULL ELSE tenant_portal.logo_content_type END,
			system_name=EXCLUDED.system_name,
			welcome_title=EXCLUDED.welcome_title,
			welcome_text=EXCLUDED.welcome_text,
			style_code=COALESCE(EXCLUDED.style_code, tenant_portal.style_code),
			sections=CASE WHEN $11 THEN NULL ELSE COALESCE($10::JSONB, tenant_portal.sections) END,
			updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,version=tenant_portal.version+1`,
			p.TenantID, logoURL, systemName, title, text, platform.Now(), p.UserID, in.ResetLogo,
			styleCode, sectionsJSON, in.ResetSections); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"message": "门户内容已保存，门户页下次打开即生效"})
	})

	/* 本厂管理员：上传企业 logo（PNG/GIF，≤500KB，直接存库） */
	api.Handle("POST /api/v1/tenant-portal/logo", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		isAdmin, err := isTenantAdmin(r, api, p)
		if err != nil {
			return err
		}
		if !isAdmin && !p.Permissions["settings:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "仅本厂管理员可上传企业 logo")
		}
		r.Body = http.MaxBytesReader(w, r.Body, tenantLogoMaxBytes)
		data, readErr := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if readErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(readErr, &maxErr) {
				return httpapi.Error(http.StatusRequestEntityTooLarge, "LOGO_TOO_LARGE", "logo 文件不能超过 500KB")
			}
			return httpapi.Error(http.StatusBadRequest, "INVALID_LOGO", "logo 文件读取失败，请重新选择")
		}
		if len(data) == 0 {
			return httpapi.Error(http.StatusBadRequest, "INVALID_LOGO", "请选择要上传的 logo 文件")
		}
		if len(data) > tenantLogoMaxBytes {
			return httpapi.Error(http.StatusRequestEntityTooLarge, "LOGO_TOO_LARGE", "logo 文件不能超过 500KB")
		}
		contentType := tenantLogoContentType(data)
		if contentType == "" {
			return httpapi.Error(http.StatusBadRequest, "UNSUPPORTED_LOGO", "logo 仅支持 PNG 或 GIF 格式")
		}
		var code string
		if err := api.DB.QueryRow(r.Context(), `SELECT code FROM tenant WHERE id=$1`, p.TenantID).Scan(&code); err != nil {
			return err
		}
		var version int64
		err = api.DB.QueryRow(r.Context(), `INSERT INTO tenant_portal(tenant_id,logo_url,logo_data,logo_content_type,updated_at,updated_by,version)
			VALUES($1,NULL,$2,$3,$4,$5,1)
			ON CONFLICT(tenant_id) DO UPDATE SET logo_url=NULL,logo_data=EXCLUDED.logo_data,
			logo_content_type=EXCLUDED.logo_content_type,updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,
			version=tenant_portal.version+1
			RETURNING version`, p.TenantID, data, contentType, platform.Now(), p.UserID).Scan(&version)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"logo_url": tenantLogoURL(code, version), "content_type": contentType, "size": len(data),
			"version": version, "message": "企业 logo 已上传，门户页下次打开即生效",
		})
	})

	/* 本厂管理员：移除已上传/已填写的企业 logo（门户恢复平台默认 logo） */
	api.Handle("DELETE /api/v1/tenant-portal/logo", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		isAdmin, err := isTenantAdmin(r, api, p)
		if err != nil {
			return err
		}
		if !isAdmin && !p.Permissions["settings:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "仅本厂管理员可移除企业 logo")
		}
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO tenant_portal(tenant_id,logo_url,logo_data,logo_content_type,updated_at,updated_by,version)
			VALUES($1,NULL,NULL,NULL,$2,$3,1)
			ON CONFLICT(tenant_id) DO UPDATE SET logo_url=NULL,logo_data=NULL,logo_content_type=NULL,
			updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,version=tenant_portal.version+1`,
			p.TenantID, platform.Now(), p.UserID); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"message": "企业 logo 已移除，门户将显示平台默认 logo"})
	})
}

/* 是否为该厂管理员（tenant_admin 角色） */
func isTenantAdmin(r *http.Request, api *httpapi.API, p auth.Principal) (bool, error) {
	var ok bool
	err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_role ur JOIN role ro ON ro.tenant_id=ur.tenant_id AND ro.id=ur.role_id
		WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND ro.code='tenant_admin')`, p.TenantID, p.UserID).Scan(&ok)
	return ok, err
}

/* defaultPortalSections 平台默认门户板块（设计 6.5）。 */
func defaultPortalSections() []map[string]any {
	return []map[string]any{
		{"key": "welcome", "title": "欢迎区", "description": "厂名、欢迎标题与欢迎语", "enabled": true},
		{"key": "overview", "title": "生产概览", "description": "今日工单、在制、人数等概览", "enabled": true},
		{"key": "entries", "title": "工作入口", "description": "按角色显示的工作入口", "enabled": true},
		{"key": "progress", "title": "体验进度", "description": "体验用户的功能点亮进度", "enabled": true},
	}
}

/* parsePortalSections 解析存储的板块 JSONB；为空返回默认板块。 */
func parsePortalSections(raw []byte) []map[string]any {
	if len(raw) == 0 {
		return defaultPortalSections()
	}
	var sections []map[string]any
	if err := json.Unmarshal(raw, &sections); err != nil || len(sections) == 0 {
		return defaultPortalSections()
	}
	return sections
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
