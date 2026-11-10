package httpapi

import (
	"net/http"

	"clothp/internal/datastore"
)

/* RegisterDataStoreRoutes 双库健康状态（设计 3.1：单写/单库不可达必须显示 degraded，不得静默当作正常）。
 * plain/secure 均不可达时返回 503，便于编排层摘流。 */
func (a *API) RegisterDataStoreRoutes(store *datastore.Store) {
	a.Public("GET /api/v1/system/health/datastore", func(w http.ResponseWriter, r *http.Request) error {
		h := store.Health(r.Context())
		status := http.StatusOK
		if h.Status == "down" {
			status = http.StatusServiceUnavailable
		}
		return JSON(w, status, h)
	})
}
