package tech

import "clothp/internal/httpapi"

/* Register 技术内核模块路由（设计 8 章）：
 * 产线资源配置、用户资料模板、生产标准工序表（两确认与查看授权）。 */
func Register(api *httpapi.API) {
	registerLineResource(api)
	registerProfileTemplate(api)
	registerProcessStandard(api)
}
