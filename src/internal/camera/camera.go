// Package camera 生产现场摄像跟踪与证据存证域（设计 4.26）：
// 摄像设备管理、录像时间轴标记、手机拍摄存证、证据封存与一次性导出。
// 数据库只存元数据与哈希；原始录像由 NVR/流媒体网关承载。
package camera

import (
	"clothp/internal/httpapi"
)

func Register(api *httpapi.API) {
	registerDevices(api)
	registerEvidence(api)
}
