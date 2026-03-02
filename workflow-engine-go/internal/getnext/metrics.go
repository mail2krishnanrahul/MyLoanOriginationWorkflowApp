package getnext

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// All metrics are registered via promauto (auto-registration with default registry).
// Label: tenant_id — tracks per-tenant queue and latency decomposition.

var (
	// GetNextClaimsTotal counts successful claim operations.
	GetNextClaimsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "claims_total",
		Help:      "Total number of successful GetNext case claim operations.",
	}, []string{"tenant_id"})

	// GetNextSkipsTotal counts skip operations.
	GetNextSkipsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "skips_total",
		Help:      "Total number of case skip operations by reason.",
	}, []string{"tenant_id", "reason"})

	// GetNextNoEligibleTotal counts times no eligible case was found.
	GetNextNoEligibleTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "no_eligible_total",
		Help:      "Total number of GetNext calls that found no eligible cases.",
	}, []string{"tenant_id"})

	// GetNextCapacityBlockTotal counts times a user was blocked by capacity.
	GetNextCapacityBlockTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "capacity_blocked_total",
		Help:      "Total number of GetNext calls blocked by user capacity limit.",
	}, []string{"tenant_id"})

	// GetNextScoreHistogram observes the composite score distribution for claimed cases.
	GetNextScoreHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "composite_score",
		Help:      "Distribution of composite scores for GetNext claimed cases.",
		Buckets:   []float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
	}, []string{"tenant_id"})

	// GetNextLatencyHistogram observes end-to-end GetNext call duration.
	GetNextLatencyHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "duration_seconds",
		Help:      "End-to-end duration of GetNext API calls in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"tenant_id", "action"})

	// GetNextQueueDepthGauge tracks live allocatable queue depth.
	GetNextQueueDepthGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "queue_depth",
		Help:      "Current number of ALLOCATABLE unassigned cases in the queue.",
	}, []string{"tenant_id"})

	// GetNextSLABreachedGauge tracks number of SLA-breached cases waiting in queue.
	GetNextSLABreachedGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "workflow",
		Subsystem: "getnext",
		Name:      "queue_sla_breached",
		Help:      "Number of ALLOCATABLE cases with a breached SLA deadline.",
	}, []string{"tenant_id"})
)
