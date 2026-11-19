// Package attendance 简单考勤域（设计 4.27）：扫描考勤屏二维码登记上下班。
// 先刷新后扫、一码一次（事务 + 唯一约束双重保证）、服务端时间为准。
package attendance

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

const ticketTTL = int64(60 * time.Second)

func Register(api *httpapi.API) {
	/* 考勤屏：刷新二维码（同事务作废旧码、签发新码，TTL 60 秒） */
	api.Handle("POST /api/v1/attendance/qr/refresh", "attendance:screen", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Point        string `json:"point"`
			DisplayLabel string `json:"display_label"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.Point = strings.TrimSpace(in.Point)
		if in.Point == "" {
			return httpapi.Error(http.StatusBadRequest, "POINT_REQUIRED", "考勤点不能为空")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		now := platform.Now()
		if _, err := tx.Exec(ctx, `UPDATE attendance_qr_ticket SET status='superseded'
			WHERE tenant_id=$1 AND att_point=$2 AND status='active'`, p.TenantID, in.Point); err != nil {
			return err
		}
		ticket := "ATD." + platform.Token(32)
		id := platform.ID()
		expiresAt := now + ticketTTL
		if _, err := tx.Exec(ctx, `INSERT INTO attendance_qr_ticket(tenant_id,id,ticket_hash,att_point,display_label,created_at,expires_at,status)
			VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,'active')`,
			p.TenantID, id, hashTicket(ticket), in.Point, in.DisplayLabel, now, expiresAt); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "refresh_attendance_qr", "attendance_qr_ticket", id, nil,
			map[string]any{"point": in.Point}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ticket": ticket, "expires_at": expiresAt, "point": in.Point})
	})

	/* 考勤屏：当前有效票据状态（不含明文，明文仅刷新时返回一次） */
	api.Handle("GET /api/v1/attendance/qr/current", "attendance:screen", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		point := r.URL.Query().Get("point")
		var expiresAt int64
		err := api.DB.QueryRow(r.Context(), `SELECT expires_at FROM attendance_qr_ticket
			WHERE tenant_id=$1 AND att_point=$2 AND status='active' AND expires_at>$3 ORDER BY created_at DESC LIMIT 1`,
			p.TenantID, point, platform.Now()).Scan(&expiresAt)
		if err == pgx.ErrNoRows {
			return httpapi.JSON(w, http.StatusOK, map[string]any{"active": false})
		}
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"active": true, "expires_at": expiresAt})
	})

	/* 员工扫码登记：校验票据并消费、写考勤记录，同一事务完成 */
	api.Handle("POST /api/v1/attendance/scan", "attendance:record", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Ticket     string `json:"ticket"`
			AttType    string `json:"att_type"`
			DeviceInfo string `json:"device_info"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.AttType != "in" && in.AttType != "out" {
			return httpapi.Error(http.StatusBadRequest, "ATT_TYPE_INVALID", "考勤类型仅支持 in/out")
		}
		employeeID := p.EmployeeID
		if employeeID == "" {
			return httpapi.Error(http.StatusBadRequest, "ATTENDANCE_NO_EMPLOYEE", "当前账号未关联员工档案，不能考勤")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var ticketID, status, point string
		var expiresAt int64
		err = tx.QueryRow(ctx, `SELECT id,status,att_point,expires_at FROM attendance_qr_ticket
			WHERE tenant_id=$1 AND ticket_hash=$2 FOR UPDATE`, p.TenantID, hashTicket(in.Ticket)).
			Scan(&ticketID, &status, &point, &expiresAt)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusBadRequest, "ATTENDANCE_TICKET_INVALID", "考勤二维码无效或已过期，请刷新后重扫")
		}
		if err != nil {
			return err
		}
		if status == "consumed" {
			return httpapi.Error(http.StatusConflict, "ATTENDANCE_TICKET_CONSUMED", "二维码已被使用，请刷新后重扫")
		}
		now := platform.Now()
		if status != "active" || expiresAt <= now {
			return httpapi.Error(http.StatusBadRequest, "ATTENDANCE_TICKET_INVALID", "考勤二维码无效或已过期，请刷新后重扫")
		}
		/* 2 分钟内同人同类型重复扫码不重复写 */
		var dupAt int64
		dupErr := tx.QueryRow(ctx, `SELECT att_at FROM attendance_record
			WHERE tenant_id=$1 AND employee_id=$2 AND att_type=$3 AND status='normal' AND att_at>$4
			ORDER BY att_at DESC LIMIT 1`, p.TenantID, employeeID, in.AttType, now-int64(2*time.Minute)).Scan(&dupAt)
		if dupErr == nil {
			return httpapi.Error(http.StatusConflict, "ATTENDANCE_DUPLICATE", "短时间内已登记同类型考勤，请勿重复扫码")
		}
		if dupErr != pgx.ErrNoRows {
			return dupErr
		}
		if _, err := tx.Exec(ctx, `UPDATE attendance_qr_ticket SET status='consumed',consumed_by=$3,consumed_at=$4
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, ticketID, employeeID, now); err != nil {
			return err
		}
		recordID := platform.ID()
		if _, err := tx.Exec(ctx, `INSERT INTO attendance_record(tenant_id,id,employee_id,att_type,att_at,ticket_id,att_point,device_info,
			status,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),'normal',$5,$9,$10,$5,$9,$10,1)`,
			p.TenantID, recordID, employeeID, in.AttType, now, ticketID, point, in.DeviceInfo, p.UserID, api.Config.NodeID); err != nil {
			if strings.Contains(err.Error(), "attendance_record_tenant_id_ticket_id_key") {
				return httpapi.Error(http.StatusConflict, "ATTENDANCE_TICKET_CONSUMED", "二维码已被使用，请刷新后重扫")
			}
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "attendance_scan", "attendance_record", recordID, nil,
			map[string]any{"att_type": in.AttType, "point": point}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"ok": true, "att_type": in.AttType, "att_at": now, "point": point})
	})

	api.Handle("GET /api/v1/attendance/my", "attendance:record", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,att_type,att_at,att_point,status FROM attendance_record
			WHERE tenant_id=$1 AND employee_id=$2 ORDER BY att_at DESC LIMIT 100`, p.TenantID, p.EmployeeID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, attType, point, status string
			var attAt int64
			if err := rows.Scan(&id, &attType, &attAt, &point, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "att_type": attType, "att_at": attAt, "point": point, "status": status})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("GET /api/v1/attendance/records", "attendance:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "a.tenant_id=$1"
		q := r.URL.Query()
		if v := q.Get("employee_id"); v != "" {
			args = append(args, v)
			where += " AND a.employee_id=$" + intStr(len(args))
		}
		if v := q.Get("att_type"); v != "" {
			args = append(args, v)
			where += " AND a.att_type=$" + intStr(len(args))
		}
		if v := q.Get("point"); v != "" {
			args = append(args, v)
			where += " AND a.att_point=$" + intStr(len(args))
		}
		if v := q.Get("date"); v != "" {
			/* date=yyyy-MM-dd，按本地日界过滤 */
			if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
				args = append(args, t.UnixMicro(), t.Add(24*time.Hour).UnixMicro())
				where += " AND a.att_at>=$" + intStr(len(args)-1) + " AND a.att_at<$" + intStr(len(args))
			}
		}
		rows, err := api.DB.Query(r.Context(), `SELECT a.id,COALESCE(e.employee_no,''),COALESCE(e.name,''),a.att_type,a.att_at,a.att_point,a.status
			FROM attendance_record a LEFT JOIN employee e ON e.tenant_id=a.tenant_id AND e.id=a.employee_id
			WHERE `+where+` ORDER BY a.att_at DESC LIMIT 500`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, empNo, empName, attType, point, status string
			var attAt int64
			if err := rows.Scan(&id, &empNo, &empName, &attType, &attAt, &point, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "employee_no": empNo, "employee_name": empName,
				"att_type": attType, "att_at": attAt, "point": point, "status": status})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 作废：原因必填，记录不可物理删除（设计 4.27.3） */
	api.Handle("POST /api/v1/attendance/records/{id}/void", "attendance:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Reason  string `json:"reason"`
			Version int64  `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(http.StatusBadRequest, "REASON_REQUIRED", "作废必须填写原因")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		now := platform.Now()
		result, err := tx.Exec(ctx, `UPDATE attendance_record SET status='void',remark=$3,updated_at=$4,updated_by=$5,updated_node=$6,version=version+1
			WHERE tenant_id=$1 AND id=$2 AND status='normal'`, p.TenantID, r.PathValue("id"), in.Reason, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录不存在或已作废")
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "void_attendance_record", "attendance_record", r.PathValue("id"),
			map[string]any{"status": "normal"}, map[string]any{"status": "void"}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func hashTicket(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

func intStr(n int) string { return strconv.Itoa(n) }
