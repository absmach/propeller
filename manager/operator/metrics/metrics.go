package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// TaskTotal is the total number of tasks created.
	TaskTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_task_total",
			Help: "Total number of tasks created",
		},
		[]string{"namespace", "phase"},
	)

	TaskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "propeller_task_duration_seconds",
			Help:    "Task execution duration in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17m
		},
		[]string{"namespace", "phase"},
	)

	TaskActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "propeller_task_active",
			Help: "Number of active tasks",
		},
		[]string{"namespace", "phase"},
	)

	// PropletTotal is the total number of proplets discovered.
	PropletTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_proplet_total",
			Help: "Total number of proplets discovered",
		},
		[]string{"namespace", "alive"},
	)

	PropletActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "propeller_proplet_active",
			Help: "Number of active proplets",
		},
		[]string{"namespace"},
	)

	PropletTaskCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "propeller_proplet_tasks",
			Help: "Number of tasks assigned to a proplet",
		},
		[]string{"namespace", "proplet_id"},
	)

	// FLRoundTotal is the total number of FL training rounds.
	FLRoundTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_fl_round_total",
			Help: "Total number of FL training rounds",
		},
		[]string{"namespace", "federated_job", "phase"},
	)

	FLRoundDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "propeller_fl_round_duration_seconds",
			Help:    "FL training round duration in seconds",
			Buckets: prometheus.ExponentialBuckets(10, 2, 12), // 10s to ~11h
		},
		[]string{"namespace", "federated_job"},
	)

	FLUpdatesCollected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_fl_updates_collected_total",
			Help: "Total number of FL updates collected",
		},
		[]string{"namespace", "federated_job", "round_id"},
	)

	FLUpdatesRequired = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "propeller_fl_updates_required",
			Help: "Number of updates required for aggregation (k-of-n)",
		},
		[]string{"namespace", "federated_job", "round_id"},
	)

	FLAggregationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_fl_aggregations_total",
			Help: "Total number of FL aggregations performed",
		},
		[]string{"namespace", "federated_job", "algorithm"},
	)

	FLAggregationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "propeller_fl_aggregation_duration_seconds",
			Help:    "FL aggregation duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 100ms to ~3m
		},
		[]string{"namespace", "federated_job", "algorithm"},
	)

	// JobTotal is the total number of Kubernetes jobs created.
	JobTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_job_total",
			Help: "Total number of Kubernetes jobs created",
		},
		[]string{"namespace", "status"},
	)

	JobDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "propeller_job_duration_seconds",
			Help:    "Kubernetes job duration in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		},
		[]string{"namespace", "status"},
	)

	// ResultExtractionTotal is the total number of result extraction attempts.
	ResultExtractionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_result_extraction_total",
			Help: "Total number of result extraction attempts",
		},
		[]string{"namespace", "source", "status"},
	)

	ResultExtractionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "propeller_result_extraction_duration_seconds",
			Help:    "Result extraction duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to ~10s
		},
		[]string{"namespace", "source"},
	)

	// ReconcileTotal is the total number of reconcile operations.
	ReconcileTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "propeller_reconcile_total",
			Help: "Total number of reconcile operations",
		},
		[]string{"controller", "namespace", "result"},
	)

	ReconcileDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "propeller_reconcile_duration_seconds",
			Help:    "Reconcile operation duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
		},
		[]string{"controller", "namespace"},
	)
)
