package scenario

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

/* 裁床单两级录入（设计 4.19.1 / 5.17 / 6.11）。
 * 上级：cutting_sheet 主表 + cutting_bed 床次行；下级：cutting_bundle_detail 扎号明细。
 * 草稿写入 cutting_bed_draft（每分钟自动保存）；正式保存才写业务明细并回写上级汇总。 */

type bedInput struct {
	ID           string `json:"id"`
	WorkOrderID  string `json:"work_order_id"`
	ProjectID    string `json:"project_id"`
	StyleNo      string `json:"style_no"`
	StyleName    string `json:"style_name"`
	CustomerID   string `json:"customer_id"`
	Color        string `json:"color"`
	BedNo        string `json:"bed_no"`
	MarkerNo     string `json:"marker_no"`
	FabricLayers int64  `json:"fabric_layers"`
	CutQty       int64  `json:"cut_qty"`
	BundleRange  string `json:"bundle_range"`
	SizeRatio    any    `json:"size_ratio"`
	CutterID     string `json:"cutter_id"`
	ReviewerID   string `json:"reviewer_id"`
	Remark       string `json:"remark"`
	SortNo       int    `json:"sort_no"`
}

type detailInput struct {
	ID          string   `json:"id"`
	Size        string   `json:"size"`
	BundleNo    string   `json:"bundle_no"`
	PieceQty    int64    `json:"piece_qty"`
	Parts       []string `json:"parts"`
	WorkOrderNo string   `json:"work_order_no"`
	StyleNo     string   `json:"style_no"`
	Color       string   `json:"color"`
	BedNo       string   `json:"bed_no"`
	SortNo      int      `json:"sort_no"`
}

func registerCuttingSheets(api *httpapi.API) {
	/* 1. 裁床单列表（三态：状态/场景筛选） */
	api.Handle("GET /api/v1/cutting-sheets", "cutting:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "s.tenant_id=$1"
		if v := strings.TrimSpace(r.URL.Query().Get("scene_type")); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND s.scene_type=$%d", len(args))
		}
		if v := strings.TrimSpace(r.URL.Query().Get("status")); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND s.status=$%d", len(args))
		}
		if v := strings.TrimSpace(r.URL.Query().Get("keyword")); v != "" {
			args = append(args, "%"+v+"%")
			where += fmt.Sprintf(" AND s.cutting_no ILIKE $%d", len(args))
		}
		args = append(args, 200)
		rows, err := api.DB.Query(r.Context(), `SELECT s.id,s.cutting_no,s.scene_type,s.scene_sub,s.cutting_date,s.status,
			COALESCE(c.name,''),COALESCE(s.remark,''),s.version,COALESCE(p.project_code,''),COALESCE(s.project_id,''),
			(SELECT COUNT(*) FROM cutting_bed b WHERE b.tenant_id=s.tenant_id AND b.sheet_id=s.id),
			COALESCE((SELECT b.style_no FROM cutting_bed b WHERE b.tenant_id=s.tenant_id AND b.sheet_id=s.id ORDER BY b.sort_no,b.id LIMIT 1),''),
			COALESCE((SELECT b.color FROM cutting_bed b WHERE b.tenant_id=s.tenant_id AND b.sheet_id=s.id ORDER BY b.sort_no,b.id LIMIT 1),''),
			COALESCE(s.total_qty,(SELECT COALESCE(SUM(d.piece_qty),0) FROM cutting_bed b JOIN cutting_bundle_detail d ON d.tenant_id=b.tenant_id AND d.bed_id=b.id WHERE b.tenant_id=s.tenant_id AND b.sheet_id=s.id))
			FROM cutting_sheet s LEFT JOIN customer c ON c.tenant_id=s.tenant_id AND c.id=s.customer_id
			LEFT JOIN project p ON p.tenant_id=s.tenant_id AND p.id=s.project_id
			WHERE `+where+fmt.Sprintf(" ORDER BY s.cutting_date DESC,s.id DESC LIMIT $%d", len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, st, sub, status, customer, remark, projectCode, projectID, styleNo, color string
			var cuttingDate, version, bedCount, pieceQty int64
			if err := rows.Scan(&id, &no, &st, &sub, &cuttingDate, &status, &customer, &remark, &version, &projectCode, &projectID, &bedCount, &styleNo, &color, &pieceQty); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "cutting_no": no, "scene_type": st, "scene_sub": sub,
				"cutting_date": cuttingDate, "status": status, "customer_name": customer, "remark": remark,
				"project_code": projectCode, "project_id": projectID, "style_no": styleNo, "color": color, "piece_qty": pieceQty,
				"version": version, "bed_count": bedCount, "outsource": isOutsourceScene(st)})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 2. 新建裁床单（可同时带床次行，草稿自动保存） */
	api.Handle("POST /api/v1/cutting-sheets", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			CuttingNo   string     `json:"cutting_no"`
			SceneType   string     `json:"scene_type"`
			SceneSub    string     `json:"scene_sub"`
			ProjectID   string     `json:"project_id"`
			CustomerID  string     `json:"customer_id"`
			CuttingDate int64      `json:"cutting_date"`
			TotalQty    *int64     `json:"total_qty"`
			Remark      string     `json:"remark"`
			Beds        []bedInput `json:"beds"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if !validScene(in.SceneType, in.SceneSub) {
			return httpapi.Error(http.StatusBadRequest, "SCENE_INVALID", "业务场景或子类型无效")
		}
		id, now := nowID()
		if in.CuttingNo == "" {
			in.CuttingNo = "CB" + fmt.Sprintf("%d", now)
		}
		if in.CuttingDate == 0 {
			in.CuttingDate = now
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if _, err = tx.Exec(r.Context(), `INSERT INTO cutting_sheet(tenant_id,id,cutting_no,scene_type,scene_sub,project_id,customer_id,cutting_date,status,remark,total_qty,
			created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,'draft',NULLIF($9,''),$10,$11,$12,$13,$11,$12,$13)`,
			p.TenantID, id, in.CuttingNo, in.SceneType, in.SceneSub, in.ProjectID, in.CustomerID, in.CuttingDate, in.Remark, in.TotalQty,
			now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		for _, b := range in.Beds {
			if b.ID == "" {
				b.ID = platformNewID()
			}
			if err := insertBed(r, tx, p, api.Config.NodeID, id, b, now); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create_cutting_sheet", "cutting_sheet", id, nil,
			map[string]any{"cutting_no": in.CuttingNo, "scene_type": in.SceneType, "scene_sub": in.SceneSub}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "cutting_no": in.CuttingNo, "version": 1})
	})

	/* 3. 详情：主表 + 上级床次行 + 每行草稿状态 */
	api.Handle("GET /api/v1/cutting-sheets/{id}", "cutting:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var no, st, sub, status, customer, remark, projectID string
		var cuttingDate, version int64
		var storedTotal *int64
		var detailSum int64
		if err := api.DB.QueryRow(r.Context(), `SELECT s.cutting_no,s.scene_type,s.scene_sub,s.status,COALESCE(c.name,''),
			COALESCE(s.remark,''),s.cutting_date,s.version,COALESCE(s.project_id,''),s.total_qty,
			COALESCE((SELECT SUM(d.piece_qty) FROM cutting_bed b JOIN cutting_bundle_detail d ON d.tenant_id=b.tenant_id AND d.bed_id=b.id WHERE b.tenant_id=s.tenant_id AND b.sheet_id=s.id),0)
			FROM cutting_sheet s
			LEFT JOIN customer c ON c.tenant_id=s.tenant_id AND c.id=s.customer_id
			WHERE s.tenant_id=$1 AND s.id=$2`, p.TenantID, r.PathValue("id")).Scan(&no, &st, &sub, &status, &customer, &remark, &cuttingDate, &version, &projectID, &storedTotal, &detailSum); err != nil {
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "裁床单不存在")
			}
			return err
		}
		// 总件数：手填优先；未填时用下级明细件数合计回填
		totalQty := detailSum
		if storedTotal != nil {
			totalQty = *storedTotal
		}
		rows, err := api.DB.Query(r.Context(), `SELECT b.id,COALESCE(b.work_order_id,''),COALESCE(b.project_id,''),b.style_no,
			COALESCE(b.style_name,''),COALESCE(c.name,''),b.color,b.bed_no,COALESCE(b.marker_no,''),b.fabric_layers,b.cut_qty,
			COALESCE(b.bundle_range,''),b.size_ratio::text,COALESCE(b.cutter_id,''),COALESCE(b.reviewer_id,''),
			COALESCE(b.remark,''),b.sort_no,b.status,b.version,
			(SELECT COUNT(*) FROM cutting_bundle_detail d WHERE d.tenant_id=b.tenant_id AND d.bed_id=b.id),
			(SELECT d.saved_at FROM cutting_bed_draft d WHERE d.tenant_id=b.tenant_id AND d.bed_id=b.id)
			FROM cutting_bed b LEFT JOIN customer c ON c.tenant_id=b.tenant_id AND c.id=b.customer_id
			WHERE b.tenant_id=$1 AND b.sheet_id=$2 ORDER BY b.sort_no,b.id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		beds := []map[string]any{}
		for rows.Next() {
			var bid, woID, projectID, styleNo, styleName, customerName, color, bedNo, markerNo, bundleRange, sizeRatio, cutter, reviewer, remarkB, statusB string
			var layers, cutQty, versionB, detailCount int64
			var sortNo int
			var draftAt *int64
			if err := rows.Scan(&bid, &woID, &projectID, &styleNo, &styleName, &customerName, &color, &bedNo, &markerNo,
				&layers, &cutQty, &bundleRange, &sizeRatio, &cutter, &reviewer, &remarkB, &sortNo, &statusB, &versionB, &detailCount, &draftAt); err != nil {
				return err
			}
			beds = append(beds, map[string]any{"id": bid, "work_order_id": woID, "project_id": projectID,
				"style_no": styleNo, "style_name": styleName, "customer_name": customerName, "color": color,
				"bed_no": bedNo, "marker_no": markerNo, "fabric_layers": layers, "cut_qty": cutQty,
				"bundle_range": bundleRange, "size_ratio": parseSizeRatio(sizeRatio), "cutter_id": cutter,
				"reviewer_id": reviewer, "remark": remarkB, "sort_no": sortNo, "status": statusB,
				"version": versionB, "detail_count": detailCount, "draft_at": draftAt})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"id": r.PathValue("id"), "cutting_no": no, "scene_type": st, "scene_sub": sub, "status": status,
			"project_id": projectID, "customer_name": customer, "remark": remark, "cutting_date": cuttingDate, "version": version,
			"total_qty": totalQty, "total_qty_set": storedTotal != nil, "detail_sum": detailSum,
			"outsource": isOutsourceScene(st), "beds": beds,
		})
	})

	/* 4. 上级表格批量保存：整表 beds（支持增删插、排序、复制粘贴），乐观锁 */
	api.Handle("PATCH /api/v1/cutting-sheets/{id}", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version    int64      `json:"version"`
			SceneType  string     `json:"scene_type"`
			SceneSub   string     `json:"scene_sub"`
			CustomerID string     `json:"customer_id"`
			Remark     string     `json:"remark"`
			Beds       []bedInput `json:"beds"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		sheetID := r.PathValue("id")
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var version int64
		if err := tx.QueryRow(ctx, `SELECT version FROM cutting_sheet WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, sheetID).Scan(&version); err != nil {
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "裁床单不存在")
			}
			return err
		}
		if version != in.Version {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "裁床单已被其他操作更新，请刷新后重试")
		}
		if in.SceneType != "" && !validScene(in.SceneType, in.SceneSub) {
			return httpapi.Error(http.StatusBadRequest, "SCENE_INVALID", "业务场景或子类型无效")
		}
		now := platformNow()
		if _, err := tx.Exec(ctx, `UPDATE cutting_sheet SET
			scene_type=COALESCE(NULLIF($3,''),scene_type),scene_sub=COALESCE(NULLIF($4,''),scene_sub),
			customer_id=COALESCE(NULLIF($5,''),customer_id),remark=NULLIF($6,''),
			updated_at=$7,updated_by=$8,updated_node=$9,version=version+1
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, sheetID, in.SceneType, in.SceneSub, in.CustomerID, in.Remark,
			now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		/* 未在提交列表中的床次行删除；已产生正式明细的禁止删除 */
		keep := []string{}
		for idx, b := range in.Beds {
			if b.SortNo == 0 {
				b.SortNo = idx + 1
			}
			if b.StyleNo == "" || b.Color == "" || b.BedNo == "" {
				return httpapi.Error(http.StatusBadRequest, "CUTTING_BED_REQUIRED", "款号、颜色、床次为必填")
			}
			if b.ID == "" {
				b.ID = platformNewID()
				if err := insertBed(r, tx, p, api.Config.NodeID, sheetID, b, now); err != nil {
					return err
				}
			} else {
				if _, err := tx.Exec(ctx, `UPDATE cutting_bed SET work_order_id=NULLIF($3,''),project_id=NULLIF($4,''),
					style_no=$5,style_name=NULLIF($6,''),customer_id=NULLIF($7,''),color=$8,bed_no=$9,
					marker_no=NULLIF($10,''),fabric_layers=$11,cut_qty=$12,bundle_range=NULLIF($13,''),
					size_ratio=$14::jsonb,cutter_id=NULLIF($15,''),reviewer_id=NULLIF($16,''),remark=NULLIF($17,''),
					sort_no=$18,status=CASE WHEN status='locked' THEN 'locked' ELSE 'saved' END,
					updated_at=$19,updated_by=$20,updated_node=$21,version=version+1
					WHERE tenant_id=$1 AND id=$2 AND sheet_id=$22`,
					p.TenantID, b.ID, b.WorkOrderID, b.ProjectID, b.StyleNo, b.StyleName, b.CustomerID, b.Color, b.BedNo,
					b.MarkerNo, b.FabricLayers, b.CutQty, b.BundleRange, jsonString(b.SizeRatio), b.CutterID, b.ReviewerID,
					b.Remark, b.SortNo, now, p.UserID, api.Config.NodeID, sheetID); err != nil {
					return err
				}
			}
			keep = append(keep, b.ID)
		}
		rows, err := tx.Query(ctx, `SELECT id FROM cutting_bed WHERE tenant_id=$1 AND sheet_id=$2`, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		existing := []string{}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			existing = append(existing, v)
		}
		rows.Close()
		keepSet := map[string]bool{}
		for _, k := range keep {
			keepSet[k] = true
		}
		for _, old := range existing {
			if keepSet[old] {
				continue
			}
			var cnt int64
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cutting_bundle_detail WHERE tenant_id=$1 AND bed_id=$2`, p.TenantID, old).Scan(&cnt); err != nil {
				return err
			}
			if cnt > 0 {
				return httpapi.Error(http.StatusConflict, "CUTTING_BUNDLE_LOCKED", "该床次已产生扎号明细，不能删除")
			}
			if _, err := tx.Exec(ctx, `DELETE FROM cutting_bed_draft WHERE tenant_id=$1 AND bed_id=$2`, p.TenantID, old); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM cutting_bed WHERE tenant_id=$1 AND id=$2`, p.TenantID, old); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE cutting_sheet SET status=CASE WHEN status='published' THEN status ELSE 'saved' END
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, sheetID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "save_cutting_sheet", "cutting_sheet", sheetID, nil,
			map[string]any{"bed_count": len(in.Beds)}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "version": in.Version + 1})
	})

	/* 5. 按上级行生成下级扎号明细草稿（可重复生成、覆盖草稿） */
	api.Handle("POST /api/v1/cutting-sheets/{id}/details/generate", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			BedID string `json:"bed_id"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		bed, err := loadBed(r, api, p, r.PathValue("id"), in.BedID)
		if err != nil {
			return err
		}
		rows := generateDetails(bed)
		if err := saveDraft(r, api, p, bed, rows); err != nil {
			return err
		}
		if err := auditWrite(r.Context(), api, p, "generate_cutting_details", "cutting_bed", bed.ID,
			map[string]any{"row_count": len(rows), "draft": true}); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"rows": rows, "count": len(rows), "draft": true})
	})

	/* 6. 下级明细查询：只读正式记录（不再自动保存草稿，避免草稿覆盖正式明细显示） */
	api.Handle("GET /api/v1/cutting-sheets/{id}/details", "cutting:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		bedID := r.URL.Query().Get("bed_id")
		bed, err := loadBed(r, api, p, r.PathValue("id"), bedID)
		if err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,COALESCE(work_order_no,''),COALESCE(style_no,''),color,bed_no,size,bundle_no,
			piece_qty,parts::text,sort_no,locked,COALESCE(ticket_id,'') FROM cutting_bundle_detail
			WHERE tenant_id=$1 AND bed_id=$2 ORDER BY sort_no,id`, p.TenantID, bedID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, wo, style, color, bedNo, size, bundleNo, parts, ticketID string
			var qty int64
			var sortNo int
			var locked bool
			if err := rows.Scan(&id, &wo, &style, &color, &bedNo, &size, &bundleNo, &qty, &parts, &sortNo, &locked, &ticketID); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "work_order_no": wo, "style_no": style, "color": color, "bed_no": bedNo,
				"size": size, "bundle_no": bundleNo, "piece_qty": qty, "parts": parseParts(parts),
				"sort_no": sortNo, "locked": locked, "ticket_id": ticketID})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"rows": items, "draft": false, "bed": bed})
	})

	/* 7. 每分钟自动保存草稿：只更新草稿区与上级草稿汇总字段，不写正式业务记录 */
	api.Handle("PUT /api/v1/cutting-sheets/{id}/details/autosave", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			BedID string        `json:"bed_id"`
			Rows  []detailInput `json:"rows"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		bed, err := loadBed(r, api, p, r.PathValue("id"), in.BedID)
		if err != nil {
			return err
		}
		if err := saveDraft(r, api, p, bed, in.Rows); err != nil {
			return err
		}
		/* 设计 5.17：两类保存均写审计；自动保存仅记行数与时间，不记业务明细 */
		if err := auditWrite(r.Context(), api, p, "autosave_cutting_details", "cutting_bed", bed.ID,
			map[string]any{"row_count": len(in.Rows), "draft": true}); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "draft": true, "saved_at": platformNow(),
			"bundle_range": summarizeBundleRange(in.Rows), "size_ratio": summarizeSizeRatio(in.Rows)})
	})

	/* 8. 正式保存：扎号唯一 + 锁定校验 + 件数汇总校验 + 回写上级，同一事务 */
	api.Handle("PUT /api/v1/cutting-sheets/{id}/details", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			BedID string        `json:"bed_id"`
			Rows  []detailInput `json:"rows"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		bed, err := loadBed(r, api, p, r.PathValue("id"), in.BedID)
		if err != nil {
			return err
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		/* 扎号唯一（床次内） */
		seen := map[string]bool{}
		var total int64
		for _, d := range in.Rows {
			d.BundleNo = strings.TrimSpace(d.BundleNo)
			if d.BundleNo == "" || d.Size == "" || d.PieceQty <= 0 {
				return httpapi.Error(http.StatusBadRequest, "CUTTING_DETAIL_REQUIRED", "码数、扎号、件数为必填且件数须大于 0")
			}
			if seen[d.BundleNo] {
				return httpapi.Error(http.StatusConflict, "CUTTING_BUNDLE_DUPLICATE", "同一床次内扎号重复："+d.BundleNo)
			}
			seen[d.BundleNo] = true
			total += d.PieceQty
		}
		/* 本床裁片数(cut_qty)与下级明细件数(piece_qty)彼此独立：
		 * 保存扎号明细不再改动/校验裁片数，裁片数由床次行单独维护。
		 * 仅发布到工单时要求裁片数>0（既有 cutting_order CHECK 约束）。 */
		/* 已锁定行不允许删除或改数：先删掉本次未提交的未锁定行，保留已提交行再逐行 UPDATE */
		submittedIDs := []string{}
		submittedSet := map[string]bool{}
		for _, d := range in.Rows {
			if d.ID != "" && !submittedSet[d.ID] {
				submittedSet[d.ID] = true
				submittedIDs = append(submittedIDs, d.ID)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM cutting_bundle_detail WHERE tenant_id=$1 AND bed_id=$2 AND locked=FALSE AND NOT (id = ANY($3::text[]))`, p.TenantID, in.BedID, submittedIDs); err != nil {
			return err
		}
		var lockedCount int64
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cutting_bundle_detail WHERE tenant_id=$1 AND bed_id=$2`, p.TenantID, in.BedID).Scan(&lockedCount); err != nil {
			return err
		}
		if lockedCount > 0 && len(submittedIDs) == 0 {
			return httpapi.Error(http.StatusConflict, "CUTTING_BUNDLE_LOCKED", "存在已锁定的扎号，请保留原记录后再保存")
		}
		now := platformNow()
		type existingRow struct {
			data  detailInput
			index int
		}
		existing := []existingRow{}
		toInsert := []existingRow{}
		insertDetail := func(d detailInput, idx int, id string) error {
			sortNo := d.SortNo
			if sortNo == 0 {
				sortNo = idx + 1
			}
			parts := d.Parts
			if parts == nil {
				parts = []string{}
			}
			_, err := tx.Exec(ctx, `INSERT INTO cutting_bundle_detail(tenant_id,id,bed_id,work_order_no,style_no,color,bed_no,
				size,bundle_no,piece_qty,parts,sort_no,locked,created_at,created_by,created_node,updated_at,updated_by,updated_node)
				VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11::jsonb,$12,FALSE,$13,$14,$15,$13,$14,$15)`,
				p.TenantID, id, in.BedID, d.WorkOrderNo, d.StyleNo, bed.Color, bed.BedNo, d.Size, d.BundleNo,
				d.PieceQty, jsonString(parts), sortNo, now, p.UserID, api.Config.NodeID)
			return err
		}
		/* 先把已提交行的扎号临时化，避免交换扎号时撞唯一约束 */
		for idx, d := range in.Rows {
			if d.ID == "" {
				toInsert = append(toInsert, existingRow{data: d, index: idx})
				continue
			}
			tag, err := tx.Exec(ctx, `UPDATE cutting_bundle_detail SET bundle_no=$4 WHERE tenant_id=$1 AND bed_id=$2 AND id=$3 AND locked=FALSE`,
				p.TenantID, in.BedID, d.ID, "__updating__"+d.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				existing = append(existing, existingRow{data: d, index: idx})
				continue
			}
			var locked bool
			err = tx.QueryRow(ctx, `SELECT locked FROM cutting_bundle_detail WHERE tenant_id=$1 AND bed_id=$2 AND id=$3`, p.TenantID, in.BedID, d.ID).Scan(&locked)
			if err == nil {
				if locked {
					continue
				}
				toInsert = append(toInsert, existingRow{data: d, index: idx})
			} else if errors.Is(err, pgx.ErrNoRows) {
				toInsert = append(toInsert, existingRow{data: d, index: idx})
			} else {
				return err
			}
		}
		for _, item := range existing {
			d := item.data
			sortNo := d.SortNo
			if sortNo == 0 {
				sortNo = item.index + 1
			}
			parts := d.Parts
			if parts == nil {
				parts = []string{}
			}
			if _, err := tx.Exec(ctx, `UPDATE cutting_bundle_detail SET work_order_no=NULLIF($4,''),style_no=NULLIF($5,''),color=$6,bed_no=$7,
				size=$8,bundle_no=$9,piece_qty=$10,parts=$11::jsonb,sort_no=$12,updated_at=$13,updated_by=$14,updated_node=$15,version=version+1
				WHERE tenant_id=$1 AND bed_id=$2 AND id=$3 AND locked=FALSE`,
				p.TenantID, in.BedID, d.ID, d.WorkOrderNo, d.StyleNo, bed.Color, bed.BedNo, d.Size, d.BundleNo,
				d.PieceQty, jsonString(parts), sortNo, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		for _, item := range toInsert {
			if err := insertDetail(item.data, item.index, platformNewID()); err != nil {
				return err
			}
		}
		bundleRange := summarizeBundleRange(in.Rows)
		sizeRatio := summarizeSizeRatio(in.Rows)
		if _, err := tx.Exec(ctx, `UPDATE cutting_bed SET bundle_range=$3,size_ratio=$4::jsonb,status='saved',
			updated_at=$5,updated_by=$6,updated_node=$7,version=version+1 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, in.BedID, bundleRange, jsonString(sizeRatio), now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cutting_sheet SET status=CASE WHEN status='published' THEN status ELSE 'saved' END,
			updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, r.PathValue("id"), now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM cutting_bed_draft WHERE tenant_id=$1 AND bed_id=$2`, p.TenantID, in.BedID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "save_cutting_details", "cutting_bed", in.BedID, nil,
			map[string]any{"rows": len(in.Rows), "total_qty": total, "bundle_range": bundleRange}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "draft": false, "count": len(in.Rows),
			"bundle_range": bundleRange, "size_ratio": sizeRatio, "total_qty": total})
	})

	/* 9. 批量打印菲票：按扎号明细 ID 列表，生成打印任务（print_job） */
	api.Handle("POST /api/v1/tickets/print", "cutting:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		/* 设计 5.17 为 cutting:read + 打印权限（AND），Handle 逗号是 OR，故显式校验。
		 * 打印权限取 print:read 或 print:write 任一，避免裁床负责人（模板仅有 cutting:read）被误挡。 */
		if err := requireAll(p, "cutting:read"); err != nil {
			return err
		}
		if !p.Permissions["print:read"] && !p.Permissions["print:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "缺少必要权限：print:read")
		}
		var in struct {
			IDs     []string `json:"ids"`
			BedID   string   `json:"bed_id"`
			Copies  int      `json:"copies"`
			Collate bool     `json:"collate"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Copies <= 0 {
			in.Copies = 1
		}
		if in.Copies > 50 {
			in.Copies = 50
		}
		if len(in.IDs) == 0 && in.BedID == "" {
			return httpapi.Error(http.StatusBadRequest, "PRINT_TARGET_REQUIRED", "请指定要打印的扎号或整床")
		}
		if len(in.IDs) > 200 {
			return httpapi.Error(http.StatusBadRequest, "PRINT_LIMIT_EXCEEDED", "单次打印最多 200 条扎号")
		}
		args := []any{p.TenantID}
		where := "d.tenant_id=$1"
		if len(in.IDs) > 0 {
			args = append(args, in.IDs)
			where += " AND d.id=ANY($" + itoa(len(args)) + "::text[])"
		} else {
			args = append(args, in.BedID)
			where += " AND d.bed_id=$" + itoa(len(args))
		}
		rows, err := api.DB.Query(r.Context(), `SELECT d.id,s.cutting_no,b.style_no,b.color,b.bed_no,d.size,d.bundle_no,
			d.piece_qty,d.parts::text,COALESCE(d.work_order_no,'') FROM cutting_bundle_detail d
			JOIN cutting_bed b ON b.tenant_id=d.tenant_id AND b.id=d.bed_id
			JOIN cutting_sheet s ON s.tenant_id=b.tenant_id AND s.id=b.sheet_id
			WHERE `+where+` ORDER BY d.sort_no,d.id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, cuttingNo, styleNo, color, bedNo, size, bundleNo, partsRaw, woNo string
			var qty int64
			if err := rows.Scan(&id, &cuttingNo, &styleNo, &color, &bedNo, &size, &bundleNo, &qty, &partsRaw, &woNo); err != nil {
				return err
			}
			parts := parseParts(partsRaw)
			if len(parts) == 0 {
				parts = []string{""}
			}
			/* 每个部位单独一张菲票；未填部位时保留一张整件票 */
			for _, partName := range parts {
				qrcode := fmt.Sprintf("%s|%s|%s|%s|%s", cuttingNo, bedNo, bundleNo, size, color)
				if partName != "" {
					qrcode += "|" + partName
				}
				items = append(items, map[string]any{"id": id, "cutting_no": cuttingNo, "style_no": styleNo,
					"color": color, "bed_no": bedNo, "size": size, "bundle_no": bundleNo, "piece_qty": qty,
					"part_name": partName, "parts": []string{partName}, "work_order_no": woNo, "qrcode": qrcode})
			}
		}
		if len(items) > 1000 {
			return httpapi.Error(http.StatusBadRequest, "PRINT_LIMIT_EXCEEDED", "单次打印最多 1000 张菲票")
		}
		/* 打印任务（print_job）：无打印机/模板时只返回渲染载荷，前端走浏览器打印 */
		var printerID, templateID string
		_ = api.DB.QueryRow(r.Context(), `SELECT id FROM printer WHERE tenant_id=$1 AND status='active' ORDER BY id LIMIT 1`, p.TenantID).Scan(&printerID)
		_ = api.DB.QueryRow(r.Context(), `SELECT id FROM print_template WHERE tenant_id=$1 AND template_type='ticket' AND status='active' ORDER BY id LIMIT 1`, p.TenantID).Scan(&templateID)
		jobID := ""
		if printerID != "" && templateID != "" {
			jobID = platformNewID()
			_, _ = api.DB.Exec(r.Context(), `INSERT INTO print_job(tenant_id,id,printer_id,template_id,owner_type,owner_id,
				rendered_payload,copies,status,requested_by,requested_at) VALUES($1,$2,$3,$4,'cutting_bundle_detail',$5,$6,$7,'queued',$8,$9)`,
				p.TenantID, jobID, printerID, templateID, in.BedID, jsonString(items), in.Copies, p.UserID, platformNow())
		}
		if err := auditWrite(r.Context(), api, p, "print_cutting_tickets", "cutting_bundle_detail", in.BedID,
			map[string]any{"ticket_count": len(items), "copies": in.Copies, "collate": in.Collate, "job_id": jobID}); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "job_id": jobID,
			"mode": map[bool]string{true: "print_job", false: "browser"}[printerID != "" && templateID != ""]})
	})

	/* 10. 发布到工单：同步生成既有 bundle 与 ticket，串接扫码报工链路 */
	api.Handle("POST /api/v1/cutting-sheets/{id}/publish", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			BedID string `json:"bed_id"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		bed, err := loadBed(r, api, p, r.PathValue("id"), in.BedID)
		if err != nil {
			return err
		}
		if bed.WorkOrderID == "" {
			return httpapi.Error(http.StatusBadRequest, "WORK_ORDER_REQUIRED", "发布到工单前请先为该床次关联工单编号")
		}
		/* 既有 cutting_order.cut_qty 有 CHECK(>0)：发布前显式校验，避免数据库 500 */
		if bed.CutQty <= 0 {
			return httpapi.Error(http.StatusBadRequest, "CUTTING_CUT_QTY_REQUIRED",
				"本床裁片数须大于 0，请在床次行填写裁片数后再发布")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var woStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM work_order WHERE tenant_id=$1 AND id=$2`, p.TenantID, bed.WorkOrderID).Scan(&woStatus); err != nil {
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "关联工单不存在")
			}
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,size,bundle_no,piece_qty,color,bed_no,parts::text FROM cutting_bundle_detail
			WHERE tenant_id=$1 AND bed_id=$2 AND bundle_id IS NULL ORDER BY sort_no,id`, p.TenantID, in.BedID)
		if err != nil {
			return err
		}
		type drow struct {
			id, size, bundleNo, color, bedNo string
			qty                              int64
			parts                            []string
		}
		list := []drow{}
		for rows.Next() {
			var d drow
			var partsRaw string
			if err := rows.Scan(&d.id, &d.size, &d.bundleNo, &d.qty, &d.color, &d.bedNo, &partsRaw); err != nil {
				rows.Close()
				return err
			}
			d.parts = parseParts(partsRaw)
			list = append(list, d)
		}
		rows.Close()
		now := platformNow()
		ticketCount := int64(0)
		for _, d := range list {
			/* 复用既有 cutting_order（一工单一张），没有则补建 */
			var cuttingOrderID string
			err := tx.QueryRow(ctx, `SELECT id FROM cutting_order WHERE tenant_id=$1 AND work_order_id=$2`, p.TenantID, bed.WorkOrderID).Scan(&cuttingOrderID)
			if err == pgx.ErrNoRows {
				cuttingOrderID = platformNewID()
				if _, err := tx.Exec(ctx, `INSERT INTO cutting_order(tenant_id,id,cutting_no,work_order_id,cutting_date,bed_no,marker_no,
					fabric_layers,cut_qty,bundle_range,size_ratio,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
					VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,NULLIF($10,''),$11::jsonb,'draft',$12,$13,$14,$12,$13,$14)`,
					p.TenantID, cuttingOrderID, "CB-"+bed.BedNo+"-"+bed.WorkOrderID, bed.WorkOrderID, now, bed.BedNo, bed.MarkerNo,
					bed.FabricLayers, bed.CutQty, bed.BundleRange, jsonString(bed.SizeRatio), now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			detailID := platformNewID()
			if _, err := tx.Exec(ctx, `INSERT INTO cutting_detail(tenant_id,id,cutting_order_id,color,size,bundle_count,piece_qty,sort_no)
				VALUES($1,$2,$3,$4,$5,1,$6,1)`, p.TenantID, detailID, cuttingOrderID, d.color, d.size, d.qty); err != nil {
				return err
			}
			bundleID := platformNewID()
			if _, err := tx.Exec(ctx, `INSERT INTO bundle(tenant_id,id,work_order_id,cutting_detail_id,bundle_no,bed_no,color,size,
				original_qty,current_qty,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,'active',$10)`,
				p.TenantID, bundleID, bed.WorkOrderID, detailID, d.bundleNo, d.bedNo, d.color, d.size, d.qty, now); err != nil {
				return err
			}
			parts := d.parts
			if len(parts) == 0 {
				parts = []string{"整件"}
			}
			firstTicketID := ""
			for pi, partName := range parts {
				partCode := fmt.Sprintf("P%d", pi+1)
				partName = strings.TrimSpace(partName)
				if len(parts) == 1 && partName == "整件" {
					partCode = "whole"
				}
				partID := platformNewID()
				if _, err := tx.Exec(ctx, `INSERT INTO bundle_part(tenant_id,id,bundle_id,part_code,part_name,sort_no,qty,status,
					created_at,created_by,created_node,updated_at,updated_by,updated_node)
					VALUES($1,$2,$3,$4,$5,$6,$7,'active',$8,$9,$10,$8,$9,$10)`,
					p.TenantID, partID, bundleID, partCode, partName, pi+1, d.qty, now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
				ticketID := platformNewID()
				if firstTicketID == "" {
					firstTicketID = ticketID
				}
				codeSource := strings.ToUpper(strings.ReplaceAll(d.bundleNo+bundleID+ticketID, "-", ""))
				if len(codeSource) > 20 {
					codeSource = codeSource[:20]
				}
				publicCode := "T" + codeSource
				if _, err := tx.Exec(ctx, `INSERT INTO ticket(tenant_id,id,bundle_id,bundle_part_id,part_code,part_name,public_code,
					signature_key_id,payload_hash,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,'scene',md5($8),'active',$9)`,
					p.TenantID, ticketID, bundleID, partID, partCode, partName, publicCode, d.bundleNo, now); err != nil {
					return err
				}
				ticketCount++
			}
			if _, err := tx.Exec(ctx, `UPDATE cutting_bundle_detail SET bundle_id=$3,ticket_id=$4,updated_at=$5,updated_by=$6,updated_node=$7
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, d.id, bundleID, firstTicketID, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE cutting_bed SET status='published',updated_at=$3,updated_by=$4,updated_node=$5
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, in.BedID, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cutting_sheet SET status='published',updated_at=$3,updated_by=$4,updated_node=$5
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id"), now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "publish_cutting_sheet", "cutting_sheet", r.PathValue("id"), nil,
			map[string]any{"bed_id": in.BedID, "published": len(list), "tickets": ticketCount}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"published": true, "bundles": len(list), "tickets": ticketCount})
	})
}

/* ---------- 助手 ---------- */

type bedRow struct {
	ID, WorkOrderID, ProjectID, StyleNo, StyleName, CustomerName, Color, BedNo, MarkerNo, BundleRange, CutterID, ReviewerID, Remark string
	FabricLayers, CutQty                                                                                                            int64
	SizeRatio                                                                                                                       []map[string]any
	Status                                                                                                                          string
}

func loadBed(r *http.Request, api *httpapi.API, p auth.Principal, sheetID, bedID string) (*bedRow, error) {
	var b bedRow
	var sizeRatio string
	err := api.DB.QueryRow(r.Context(), `SELECT b.id,COALESCE(b.work_order_id,''),COALESCE(b.project_id,''),b.style_no,
		COALESCE(b.style_name,''),COALESCE(c.name,''),b.color,b.bed_no,COALESCE(b.marker_no,''),b.fabric_layers,b.cut_qty,
		COALESCE(b.bundle_range,''),b.size_ratio::text,COALESCE(b.cutter_id,''),COALESCE(b.reviewer_id,''),COALESCE(b.remark,''),b.status
		FROM cutting_bed b LEFT JOIN customer c ON c.tenant_id=b.tenant_id AND c.id=b.customer_id
		WHERE b.tenant_id=$1 AND b.id=$2 AND b.sheet_id=$3`, p.TenantID, bedID, sheetID).Scan(&b.ID, &b.WorkOrderID,
		&b.ProjectID, &b.StyleNo, &b.StyleName, &b.CustomerName, &b.Color, &b.BedNo, &b.MarkerNo, &b.FabricLayers,
		&b.CutQty, &b.BundleRange, &sizeRatio, &b.CutterID, &b.ReviewerID, &b.Remark, &b.Status)
	if err == pgx.ErrNoRows {
		return nil, httpapi.Error(http.StatusNotFound, "NOT_FOUND", "床次行不存在或不属于该裁床单")
	}
	if err != nil {
		return nil, err
	}
	b.SizeRatio = parseSizeRatio(sizeRatio)
	return &b, nil
}

func insertBed(r *http.Request, tx pgx.Tx, p auth.Principal, nodeID, sheetID string, b bedInput, now int64) error {
	id := b.ID
	if id == "" {
		id = platformNewID()
	}
	if b.StyleNo == "" || b.Color == "" || b.BedNo == "" {
		return httpapi.Error(http.StatusBadRequest, "CUTTING_BED_REQUIRED", "款号、颜色、床次为必填")
	}
	_, err := tx.Exec(r.Context(), `INSERT INTO cutting_bed(tenant_id,id,sheet_id,work_order_id,project_id,style_no,style_name,
		customer_id,color,bed_no,marker_no,fabric_layers,cut_qty,bundle_range,size_ratio,cutter_id,reviewer_id,remark,sort_no,status,
		created_at,created_by,created_node,updated_at,updated_by,updated_node)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),NULLIF($8,''),$9,$10,NULLIF($11,''),$12,$13,
		NULLIF($14,''),$15::jsonb,NULLIF($16,''),NULLIF($17,''),NULLIF($18,''),$19,'draft',$20,$21,$22,$20,$21,$22)`,
		p.TenantID, id, sheetID, b.WorkOrderID, b.ProjectID, b.StyleNo, b.StyleName, b.CustomerID, b.Color, b.BedNo,
		b.MarkerNo, b.FabricLayers, b.CutQty, b.BundleRange, jsonString(b.SizeRatio), b.CutterID, b.ReviewerID,
		b.Remark, b.SortNo, now, p.UserID, nodeID)
	return err
}

func saveDraft(r *http.Request, api *httpapi.API, p auth.Principal, bed *bedRow, rows []detailInput) error {
	now := platformNow()
	if rows == nil {
		rows = []detailInput{}
	}
	_, err := api.DB.Exec(r.Context(), `INSERT INTO cutting_bed_draft(tenant_id,id,bed_id,payload,saved_at)
		VALUES($1,$2,$3,$4::jsonb,$5) ON CONFLICT(tenant_id,bed_id) DO UPDATE SET payload=EXCLUDED.payload,
		saved_at=EXCLUDED.saved_at,version=cutting_bed_draft.version+1`,
		p.TenantID, platformNewID(), bed.ID, jsonString(rows), now)
	if err != nil {
		return err
	}
	/* 草稿汇总只回显到前端，不写正式业务字段：调用方通过返回值呈现 */
	return nil
}

func generateDetails(bed *bedRow) []detailInput {
	rows := []detailInput{}
	bundleNos := expandBundleRange(bed.BundleRange)
	if len(bundleNos) == 0 && bed.CutQty > 0 {
		for i := int64(1); i <= bed.CutQty; i++ {
			bundleNos = append(bundleNos, fmt.Sprintf("%d", i))
		}
	}
	sizes := []string{}
	for _, sr := range bed.SizeRatio {
		if s, ok := sr["size"].(string); ok && s != "" {
			sizes = append(sizes, s)
		}
	}
	if len(sizes) == 0 {
		sizes = []string{"S", "M", "L", "XL"}
	}
	perBundle := int64(1)
	if bed.FabricLayers > 0 {
		perBundle = bed.FabricLayers
	}
	for idx, no := range bundleNos {
		rows = append(rows, detailInput{Size: sizes[idx%len(sizes)], BundleNo: no, PieceQty: perBundle,
			Parts: []string{}, WorkOrderNo: bed.WorkOrderID, StyleNo: bed.StyleNo, Color: bed.Color,
			BedNo: bed.BedNo, SortNo: idx + 1})
	}
	return rows
}

func expandBundleRange(raw string) []string {
	out := []string{}
	for _, seg := range strings.Split(raw, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		parts := strings.SplitN(seg, "~", 2)
		if len(parts) == 2 {
			a, errA := strconv.Atoi(strings.TrimSpace(parts[0]))
			b, errB := strconv.Atoi(strings.TrimSpace(parts[1]))
			if errA == nil && errB == nil && b >= a && b-a < 1000 {
				for i := a; i <= b; i++ {
					out = append(out, strconv.Itoa(i))
				}
				continue
			}
		}
		out = append(out, seg)
	}
	return out
}

func summarizeBundleRange(rows []detailInput) string {
	if len(rows) == 0 {
		return ""
	}
	nums := []int{}
	others := []string{}
	for _, d := range rows {
		if n, err := strconv.Atoi(strings.TrimSpace(d.BundleNo)); err == nil {
			nums = append(nums, n)
		} else {
			others = append(others, strings.TrimSpace(d.BundleNo))
		}
	}
	sort.Ints(nums)
	segments := []string{}
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		if i == j {
			segments = append(segments, strconv.Itoa(nums[i]))
		} else {
			segments = append(segments, fmt.Sprintf("%d~%d", nums[i], nums[j]))
		}
		i = j + 1
	}
	segments = append(segments, others...)
	return strings.Join(segments, ",")
}

func summarizeSizeRatio(rows []detailInput) []map[string]any {
	bySize := map[string]int64{}
	order := []string{}
	for _, d := range rows {
		if d.Size == "" {
			continue
		}
		if _, ok := bySize[d.Size]; !ok {
			order = append(order, d.Size)
		}
		bySize[d.Size] += d.PieceQty
	}
	out := []map[string]any{}
	for _, s := range order {
		out = append(out, map[string]any{"size": s, "qty": bySize[s]})
	}
	return out
}

func parseSizeRatio(raw string) []map[string]any {
	var v []map[string]any
	if json.Unmarshal([]byte(raw), &v) != nil || v == nil {
		return []map[string]any{}
	}
	return v
}

func parseParts(raw string) []string {
	var v []string
	if json.Unmarshal([]byte(raw), &v) != nil || v == nil {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range v {
		name := strings.TrimSpace(item)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
