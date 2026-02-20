package multitenancy

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	metricsOnce sync.Once

	casesCreatedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cases_created_total",
			Help: "Total cases created by tenant and case type.",
		},
		[]string{"tenant_id", "case_type_code"},
	)
	tasksClaimedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tasks_claimed_total",
			Help: "Total tasks claimed by tenant and assigned service.",
		},
		[]string{"tenant_id", "assigned_service"},
	)
	tasksFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tasks_failed_total",
			Help: "Total tasks failed by tenant, assigned service and reason.",
		},
		[]string{"tenant_id", "assigned_service", "reason"},
	)
	notificationsQueuedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notifications_queued_total",
			Help: "Total notifications queued by tenant and channel.",
		},
		[]string{"tenant_id", "channel"},
	)
	slaBreachedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sla_breached_total",
			Help: "Total SLA breaches by tenant and case type.",
		},
		[]string{"tenant_id", "case_type_code"},
	)
	tenantActiveCasesGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tenant_active_cases",
			Help: "Current active case count by tenant.",
		},
		[]string{"tenant_id"},
	)
	tenantMaxActiveCasesConfigGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tenant_max_active_cases_config",
			Help: "Configured max_active_cases value per tenant from tenants.config.",
		},
		[]string{"tenant_id"},
	)
)

// RegisterTenantMetrics registers all tenant-labeled metric vectors.
func RegisterTenantMetrics(registry prometheus.Registerer) {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}
	metricsOnce.Do(func() {
		registry.MustRegister(
			casesCreatedTotal,
			tasksClaimedTotal,
			tasksFailedTotal,
			notificationsQueuedTotal,
			slaBreachedTotal,
			tenantActiveCasesGauge,
			tenantMaxActiveCasesConfigGauge,
		)
	})
}

func IncCasesCreated(tenantID, caseTypeCode string) {
	casesCreatedTotal.WithLabelValues(tenantID, caseTypeCode).Inc()
}

func IncTasksClaimed(tenantID, assignedService string) {
	tasksClaimedTotal.WithLabelValues(tenantID, assignedService).Inc()
}

func IncTasksFailed(tenantID, assignedService, reason string) {
	tasksFailedTotal.WithLabelValues(tenantID, assignedService, reason).Inc()
}

func IncNotificationsQueued(tenantID, channel string) {
	notificationsQueuedTotal.WithLabelValues(tenantID, channel).Inc()
}

func IncSLABreached(tenantID, caseTypeCode string) {
	slaBreachedTotal.WithLabelValues(tenantID, caseTypeCode).Inc()
}

// RefreshTenantActiveCasesGauge updates tenant_active_cases and max config gauges from DB.
func RefreshTenantActiveCasesGauge(ctx context.Context, db *sqlx.DB) error {
	if db == nil {
		return fmt.Errorf("RefreshTenantActiveCasesGauge: db is nil")
	}

	rows := make([]struct {
		TenantID        string `db:"tenant_id"`
		ActiveCases     int64  `db:"active_cases"`
		MaxActiveConfig int64  `db:"max_active_config"`
	}, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			t.tenant_id::text AS tenant_id,
			COALESCE(active.active_cases, 0)::bigint AS active_cases,
			COALESCE((t.config->>'max_active_cases')::bigint, 0)::bigint AS max_active_config
		FROM tenants t
		LEFT JOIN (
			SELECT tenant_id, COUNT(*)::bigint AS active_cases
			FROM cases
			WHERE status NOT IN ('COMPLETED', 'CANCELLED', 'REJECTED', 'CLONED')
			GROUP BY tenant_id
		) active ON active.tenant_id = t.tenant_id
	`); err != nil {
		return fmt.Errorf("RefreshTenantActiveCasesGauge: query active counts: %w", err)
	}

	for i := range rows {
		row := rows[i]
		tenantActiveCasesGauge.WithLabelValues(row.TenantID).Set(float64(row.ActiveCases))
		tenantMaxActiveCasesConfigGauge.WithLabelValues(row.TenantID).Set(float64(row.MaxActiveConfig))
	}
	return nil
}
