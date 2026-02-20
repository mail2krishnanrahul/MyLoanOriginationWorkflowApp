package integration

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	integrationMetricsOnce sync.Once

	webhookDeliveriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_deliveries_total",
			Help: "Total webhook delivery attempts by tenant, event type and delivery status.",
		},
		[]string{"tenant_id", "event_type", "status"},
	)
	webhookDeliveryLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "webhook_delivery_latency_seconds",
			Help:    "Webhook delivery latency in seconds by tenant and event type.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tenant_id", "event_type"},
	)
	webhookSubscriptionFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_subscription_failures_total",
			Help: "Total webhook subscriptions transitioned to FAILED by tenant/subscriber.",
		},
		[]string{"tenant_id", "subscriber_code"},
	)
	externalTaskCompletionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "external_task_completions_total",
			Help: "Total external task completion attempts by tenant/service/status.",
		},
		[]string{"tenant_id", "assigned_service", "status"},
	)
	externalEventsIngestedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "external_events_ingested_total",
			Help: "Total external events accepted by tenant/event type/source system.",
		},
		[]string{"tenant_id", "event_type", "source_system"},
	)
	externalEventsRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "external_events_rejected_total",
			Help: "Total external events rejected by tenant/event type/reason.",
		},
		[]string{"tenant_id", "event_type", "reason"},
	)
	serviceHealthStatusGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_health_status",
			Help: "External service health status (1=ACTIVE,0.5=DEGRADED,0=OFFLINE).",
		},
		[]string{"tenant_id", "service_code"},
	)
	idempotencyDuplicatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "idempotency_duplicates_total",
			Help: "Total idempotency duplicate detections by tenant/keyspace.",
		},
		[]string{"tenant_id", "keyspace"},
	)
)

// RegisterIntegrationMetrics registers integration metrics.
func RegisterIntegrationMetrics(registry prometheus.Registerer) {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}
	integrationMetricsOnce.Do(func() {
		registry.MustRegister(
			webhookDeliveriesTotal,
			webhookDeliveryLatencySeconds,
			webhookSubscriptionFailuresTotal,
			externalTaskCompletionsTotal,
			externalEventsIngestedTotal,
			externalEventsRejectedTotal,
			serviceHealthStatusGauge,
			idempotencyDuplicatesTotal,
		)
	})
}

func IncWebhookDelivery(tenantID, eventType, status string) {
	webhookDeliveriesTotal.WithLabelValues(tenantID, eventType, status).Inc()
}

func ObserveWebhookDeliveryLatency(tenantID, eventType string, seconds float64) {
	webhookDeliveryLatencySeconds.WithLabelValues(tenantID, eventType).Observe(seconds)
}

func IncWebhookSubscriptionFailure(tenantID, subscriberCode string) {
	webhookSubscriptionFailuresTotal.WithLabelValues(tenantID, subscriberCode).Inc()
}

func IncExternalTaskCompletion(tenantID, assignedService, status string) {
	externalTaskCompletionsTotal.WithLabelValues(tenantID, assignedService, status).Inc()
}

func IncExternalEventIngested(tenantID, eventType, sourceSystem string) {
	externalEventsIngestedTotal.WithLabelValues(tenantID, eventType, sourceSystem).Inc()
}

func IncExternalEventRejected(tenantID, eventType, reason string) {
	externalEventsRejectedTotal.WithLabelValues(tenantID, eventType, reason).Inc()
}

func SetServiceHealthStatus(tenantID, serviceCode string, statusValue float64) {
	serviceHealthStatusGauge.WithLabelValues(tenantID, serviceCode).Set(statusValue)
}

func IncIdempotencyDuplicate(tenantID string, keyspace IdempotencyKeyspace) {
	idempotencyDuplicatesTotal.WithLabelValues(tenantID, string(keyspace)).Inc()
}
