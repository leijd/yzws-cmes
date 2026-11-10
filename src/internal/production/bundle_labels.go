package production

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"
)

type bundleLabelPart struct {
	PartCode   string `json:"part_code"`
	PartName   string `json:"part_name"`
	Qty        int64  `json:"qty"`
	TicketID   string `json:"ticket_id"`
	PublicCode string `json:"public_code"`
}

// bundleLabel 是一扎（bundle）的标签数据。一个扎可能拆成多个部位，
// 标签以扎为主，部位信息放在 parts 中，二维码指向该扎的主菲票。
type bundleLabel struct {
	BundleID      string            `json:"bundle_id"`
	TicketID      string            `json:"ticket_id"`
	PublicCode    string            `json:"public_code"`
	TenantCode    string            `json:"tenant_code"`
	TenantName    string            `json:"tenant_name"`
	ProjectCode   string            `json:"project_code"`
	Brand         string            `json:"brand"`
	CustomerCode  string            `json:"customer_code"`
	CustomerName  string            `json:"customer_name"`
	WorkOrderCode string            `json:"work_order_code"`
	ProductCode   string            `json:"product_code"`
	ProductName   string            `json:"product_name"`
	Spec          string            `json:"spec"`
	Style         string            `json:"style"`
	Color         string            `json:"color"`
	Size          string            `json:"size"`
	Qty           int64             `json:"qty"`
	BundleNo      string            `json:"bundle_no"`
	BedNo         string            `json:"bed_no"`
	MarkerNo      string            `json:"marker_no"`
	FabricLayers  int64             `json:"fabric_layers"`
	CuttingNo     string            `json:"cutting_no"`
	CuttingDate   int64             `json:"cutting_date"`
	CutterName    string            `json:"cutter_name"`
	Parts         []bundleLabelPart `json:"parts"`
	QrPayload     string            `json:"qr_payload"`
	QrImage       string            `json:"qr_image"`
	GeneratedAt   int64             `json:"generated_at"`
}

const bundleLabelSelect = `
SELECT COALESCE(tk.id,''),COALESCE(tk.public_code,''),
	b.id,b.bundle_no,b.color,b.size,b.current_qty,COALESCE(NULLIF(b.bed_no,''),co.bed_no,''),
	COALESCE(co.cutting_no,''),COALESCE(co.marker_no,''),COALESCE(co.fabric_layers,0),COALESCE(co.cutting_date,0),
	COALESCE(cutter.name,''),
	wo.work_order_code,pj.project_code,cu.code,cu.name,
	pr.product_code,pr.name,COALESCE(pr.spec,''),
	tn.code,tn.name,
	COALESCE((
		SELECT json_agg(json_build_object(
			'part_code',bp.part_code,'part_name',bp.part_name,'qty',bp.qty,
			'ticket_id',COALESCE(pt.id,''),'public_code',COALESCE(pt.public_code,'')
		) ORDER BY bp.sort_no)
		FROM bundle_part bp
		LEFT JOIN LATERAL (
			SELECT t2.id,t2.public_code FROM ticket t2
			WHERE t2.tenant_id=bp.tenant_id AND t2.bundle_part_id=bp.id AND t2.status='active'
			ORDER BY t2.created_at,t2.id LIMIT 1
		) pt ON TRUE
		WHERE bp.tenant_id=b.tenant_id AND bp.bundle_id=b.id
	),'[]'::json)
FROM bundle b
JOIN work_order wo ON wo.tenant_id=b.tenant_id AND wo.id=b.work_order_id
JOIN project pj ON pj.tenant_id=wo.tenant_id AND pj.id=wo.project_id
JOIN customer cu ON cu.tenant_id=pj.tenant_id AND cu.id=pj.customer_id
JOIN product pr ON pr.tenant_id=wo.tenant_id AND pr.id=wo.product_id
JOIN tenant tn ON tn.id=b.tenant_id
LEFT JOIN cutting_detail cd ON cd.tenant_id=b.tenant_id AND cd.id=b.cutting_detail_id
LEFT JOIN cutting_order co ON co.tenant_id=cd.tenant_id AND co.id=cd.cutting_order_id
LEFT JOIN employee cutter ON cutter.tenant_id=co.tenant_id AND cutter.id=co.cutter_id
LEFT JOIN LATERAL (
	SELECT t.id,t.public_code FROM ticket t
	WHERE t.tenant_id=b.tenant_id AND t.bundle_id=b.id AND t.status='active'
	ORDER BY CASE WHEN COALESCE(t.part_code,'')='whole' THEN 0 ELSE 1 END,t.created_at,t.id
	LIMIT 1
) tk ON TRUE
WHERE b.tenant_id=$1`

func scanBundleLabel(row pgx.Row) (bundleLabel, error) {
	var item bundleLabel
	var parts []byte
	err := row.Scan(&item.TicketID, &item.PublicCode, &item.BundleID, &item.BundleNo, &item.Color, &item.Size, &item.Qty, &item.BedNo,
		&item.CuttingNo, &item.MarkerNo, &item.FabricLayers, &item.CuttingDate, &item.CutterName,
		&item.WorkOrderCode, &item.ProjectCode, &item.CustomerCode, &item.CustomerName,
		&item.ProductCode, &item.ProductName, &item.Spec, &item.TenantCode, &item.TenantName, &parts)
	if err != nil {
		return item, err
	}
	item.Brand = item.CustomerName
	item.Style = item.ProductName
	if len(parts) > 0 {
		if err := json.Unmarshal(parts, &item.Parts); err != nil {
			return item, err
		}
	}
	if item.Parts == nil {
		item.Parts = []bundleLabelPart{}
	}
	return item, nil
}

func loadBundleLabel(ctx context.Context, q dbQuerier, tenantID, bundleID string) (bundleLabel, error) {
	return scanBundleLabel(q.QueryRow(ctx, bundleLabelSelect+" AND b.id=$2", tenantID, bundleID))
}

func loadBundleLabelByCode(ctx context.Context, q dbQuerier, tenantID, publicCode string) (bundleLabel, error) {
	return scanBundleLabel(q.QueryRow(ctx, bundleLabelSelect+` AND EXISTS (
		SELECT 1 FROM ticket t WHERE t.tenant_id=b.tenant_id AND t.bundle_id=b.id AND t.public_code=$2
	) LIMIT 1`, tenantID, publicCode))
}

func loadBundleLabels(ctx context.Context, q dbQuerier, tenantID, workOrderID, cuttingOrderID string) ([]bundleLabel, error) {
	args := []any{tenantID}
	where := ""
	if workOrderID != "" {
		args = append(args, workOrderID)
		where = " AND b.work_order_id=$2"
	} else {
		args = append(args, cuttingOrderID)
		where = " AND cd.cutting_order_id=$2"
	}
	rows, err := q.Query(ctx, bundleLabelSelect+where+" ORDER BY b.bundle_no::integer,b.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []bundleLabel{}
	for rows.Next() {
		item, err := scanBundleLabel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func firstHeaderToken(value string) string {
	if index := strings.Index(value, ","); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

// publicBaseURL 返回扫码地址前缀：优先使用配置值，其次使用 Web 端请求的
// Origin/Referer，最后才退回当前请求的 Host。
func publicBaseURL(r *http.Request, configured string) string {
	if base := strings.TrimRight(strings.TrimSpace(configured), "/"); base != "" {
		return base
	}
	if origin := firstHeaderToken(r.Header.Get("Origin")); origin != "" && origin != "null" {
		return strings.TrimRight(origin, "/")
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		if parsed, err := url.Parse(referer); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := firstHeaderToken(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	host := r.Host
	if forwarded := firstHeaderToken(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host
}

func buildScanPayload(baseURL, tenantCode, publicCode string) string {
	code := strings.TrimSpace(publicCode)
	if code == "" {
		return ""
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return code
	}
	return baseURL + "/s/" + url.PathEscape(strings.TrimSpace(tenantCode)) + "/" + url.PathEscape(code)
}

func encodeQRDataURI(payload string) string {
	if payload == "" {
		return ""
	}
	png, err := qrcode.Encode(payload, qrcode.Medium, 320)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func fillBundleLabelQR(r *http.Request, configuredBase string, item *bundleLabel) {
	item.QrPayload = buildScanPayload(publicBaseURL(r, configuredBase), item.TenantCode, item.PublicCode)
	item.QrImage = encodeQRDataURI(item.QrPayload)
	item.GeneratedAt = platform.Now()
}

func registerBundleLabels(api *httpapi.API) {
	api.Handle("GET /api/v1/bundles/{id}/label", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		item, err := loadBundleLabel(r.Context(), api.DB, p.TenantID, r.PathValue("id"))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "扎不存在或尚未生成菲票")
		}
		if err != nil {
			return err
		}
		fillBundleLabelQR(r, api.Config.PublicBaseURL, &item)
		return httpapi.JSON(w, 200, item)
	})

	api.Handle("GET /api/v1/bundle-labels", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		workOrderID := strings.TrimSpace(r.URL.Query().Get("work_order_id"))
		cuttingOrderID := strings.TrimSpace(r.URL.Query().Get("cutting_order_id"))
		if workOrderID == "" && cuttingOrderID == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "请指定工单或裁床单")
		}
		items, err := loadBundleLabels(r.Context(), api.DB, p.TenantID, workOrderID, cuttingOrderID)
		if err != nil {
			return err
		}
		for index := range items {
			fillBundleLabelQR(r, api.Config.PublicBaseURL, &items[index])
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	// 扫码落地页使用：按租户编码 + 菲票公开码读取扎标签，无需登录，只返回
	// 标签展示所需信息，不包含价格、工资等敏感字段。
	api.Public("GET /api/v1/public/scan/{tenant}/{code}", func(w http.ResponseWriter, r *http.Request) error {
		tenant := strings.TrimSpace(r.PathValue("tenant"))
		code := strings.TrimSpace(r.PathValue("code"))
		if tenant == "" || code == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "租户和扫码内容不能为空")
		}
		var tenantID string
		err := api.DB.QueryRow(r.Context(), `SELECT id FROM tenant WHERE (code=$1 OR short_code=$1) AND status='active'`, tenant).Scan(&tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "TENANT_NOT_FOUND", "租户不存在或已停用")
		}
		if err != nil {
			return err
		}
		item, err := loadBundleLabelByCode(r.Context(), api.DB, tenantID, code)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "未找到对应的扎标签")
		}
		if err != nil {
			return err
		}
		item.QrPayload = ""
		item.QrImage = ""
		item.GeneratedAt = platform.Now()
		return httpapi.JSON(w, 200, item)
	})
}
