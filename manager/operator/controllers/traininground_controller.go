package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	propellerv1alpha1 "github.com/absmach/propeller/api/v1alpha1"
	"github.com/absmach/propeller/manager/operator/metrics"
	flpkg "github.com/absmach/propeller/pkg/fl"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type TrainingRoundReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=propeller.absmach.io,resources=trainingrounds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=trainingrounds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=trainingrounds/finalizers,verbs=update
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=wasmtasks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=wasmtasks/status,verbs=get;update;patch

func (r *TrainingRoundReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)
	namespace := req.Namespace

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ReconcileDuration.WithLabelValues("traininground", namespace).Observe(duration)
	}()

	round := &propellerv1alpha1.TrainingRound{}
	if err := r.Get(ctx, req.NamespacedName, round); err != nil {
		metrics.ReconcileTotal.WithLabelValues("traininground", namespace, "not_found").Inc()

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	federatedJobName := ""
	if round.Spec.FederatedJobRef.Name != "" {
		federatedJobName = round.Spec.FederatedJobRef.Name
	}

	if round.Status.Phase == "" {
		now := metav1.Now()
		round.Status.Phase = phasePending
		round.Status.StartTime = &now
		round.Status.UpdatesRequired = round.Spec.KOfN
		metrics.FLRoundTotal.WithLabelValues(namespace, federatedJobName, phasePending).Inc()
		metrics.FLUpdatesRequired.WithLabelValues(namespace, federatedJobName, round.Spec.RoundID).Set(float64(round.Spec.KOfN))
		if err := r.Status().Update(ctx, round); err != nil {
			metrics.ReconcileTotal.WithLabelValues("traininground", namespace, "error").Inc()

			return ctrl.Result{}, err
		}
	}

	var result ctrl.Result
	var err error

	switch round.Status.Phase {
	case phasePending:
		result, err = r.handlePending(ctx, round)
	case phaseRunning:
		result, err = r.handleRunning(ctx, round)
	case "Aggregating":
		result, err = r.handleAggregating(ctx, round)
	case phaseCompleted, phaseFailed:
		metrics.ReconcileTotal.WithLabelValues("traininground", namespace, "noop").Inc()

		return ctrl.Result{}, nil
	default:
		logger.Info("unknown phase", "phase", round.Status.Phase)
		metrics.ReconcileTotal.WithLabelValues("traininground", namespace, "unknown_phase").Inc()

		return ctrl.Result{}, nil
	}

	if err != nil {
		metrics.ReconcileTotal.WithLabelValues("traininground", namespace, "error").Inc()
	} else {
		metrics.ReconcileTotal.WithLabelValues("traininground", namespace, "success").Inc()
	}

	return result, err
}

func (r *TrainingRoundReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&propellerv1alpha1.TrainingRound{}).
		Owns(&propellerv1alpha1.WasmTask{}).
		Complete(r)
}

func (r *TrainingRoundReconciler) handlePending(ctx context.Context, round *propellerv1alpha1.TrainingRound) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if len(round.Status.Participants) == 0 {
		round.Status.Participants = make([]propellerv1alpha1.RoundParticipantStatus, len(round.Spec.Participants))
		for i, propletID := range round.Spec.Participants {
			round.Status.Participants[i] = propellerv1alpha1.RoundParticipantStatus{
				PropletID:      propletID,
				Status:         "Pending",
				UpdateReceived: false,
			}
		}
	}

	for i, participantID := range round.Spec.Participants {
		taskName := fmt.Sprintf("%s-%s", round.Name, participantID)

		existingTask := &propellerv1alpha1.WasmTask{}
		if err := r.Get(ctx, client.ObjectKey{Name: taskName, Namespace: round.Namespace}, existingTask); err == nil {
			round.Status.Participants[i].TaskRef = &corev1.ObjectReference{
				APIVersion: "propeller.absmach.io/v1alpha1",
				Kind:       "WasmTask",
				Name:       taskName,
				Namespace:  round.Namespace,
			}

			continue
		}

		env := make(map[string]string)
		env["ROUND_ID"] = round.Spec.RoundID
		env["MODEL_URI"] = round.Spec.ModelRef
		env["PROPLET_ID"] = participantID

		if aggregatedUpdateJSON, ok := round.Annotations["propeller.absmach.io/aggregated-update"]; ok {
			var aggEnv flpkg.UpdateEnvelope
			if err := json.Unmarshal([]byte(aggregatedUpdateJSON), &aggEnv); err == nil {
				env["FL_GLOBAL_VERSION"] = aggEnv.GlobalVersion
				env["FL_GLOBAL_UPDATE_B64"] = aggEnv.UpdateB64
				if aggEnv.Format != "" {
					env["FL_GLOBAL_UPDATE_FORMAT"] = aggEnv.Format
				}
			}
		}

		if round.Spec.FederatedJobRef.Name != "" {
			federatedJob := &propellerv1alpha1.FederatedJob{}
			if err := r.Get(ctx, client.ObjectKey{Name: round.Spec.FederatedJobRef.Name, Namespace: round.Namespace}, federatedJob); err == nil {
				env["FL_JOB_ID"] = federatedJob.Spec.ExperimentID
			}
		}

		if round.Spec.Hyperparams != nil {
			env["HYPERPARAMS"] = string(round.Spec.Hyperparams.Raw)
		}

		wasmTask := &propellerv1alpha1.WasmTask{
			ObjectMeta: metav1.ObjectMeta{
				Name:      taskName,
				Namespace: round.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: round.APIVersion,
						Kind:       round.Kind,
						Name:       round.Name,
						UID:        round.UID,
						Controller: func() *bool {
							b := true

							return &b
						}(),
					},
				},
				Labels: map[string]string{
					"training-round": round.Name,
					"round-id":       round.Spec.RoundID,
					"participant":    participantID,
				},
			},
			Spec: propellerv1alpha1.WasmTaskSpec{
				ImageURL:  round.Spec.TaskWasmImage,
				PropletID: participantID,
				Env:       env,
				Mode:      "train",
				Daemon:    false,
				CLIArgs:   []string{},
			},
		}

		if err := r.Create(ctx, wasmTask); err != nil {
			logger.Error(err, "failed to create wasmtask", "wasmtask", taskName)

			continue
		}

		round.Status.Participants[i].TaskRef = &corev1.ObjectReference{
			APIVersion: "propeller.absmach.io/v1alpha1",
			Kind:       "WasmTask",
			Name:       taskName,
			Namespace:  round.Namespace,
		}
		round.Status.Participants[i].Status = phaseRunning
	}

	round.Status.Phase = phaseRunning
	federatedJobName := ""
	if round.Spec.FederatedJobRef.Name != "" {
		federatedJobName = round.Spec.FederatedJobRef.Name
	}
	metrics.FLRoundTotal.WithLabelValues(round.Namespace, federatedJobName, phaseRunning).Inc()
	r.updateCondition(round, "True", "Running", "Round is running")
	if err := r.Status().Update(ctx, round); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func (r *TrainingRoundReconciler) handleRunning(ctx context.Context, round *propellerv1alpha1.TrainingRound) (ctrl.Result, error) {
	updatesReceived, collectedUpdates := r.processParticipants(ctx, round)
	round.Status.UpdatesReceived = updatesReceived

	if len(collectedUpdates) == 0 {
		return r.updateStatusAndRequeue(ctx, round, time.Second*10)
	}

	r.storeCollectedUpdates(round, collectedUpdates)

	if updatesReceived >= round.Spec.KOfN {
		return r.transitionToAggregating(ctx, round, updatesReceived)
	}

	if result := r.checkTimeout(ctx, round, updatesReceived); result != nil {
		return *result, nil
	}

	return r.updateStatusAndRequeue(ctx, round, time.Second*10)
}

//nolint:unparam // ctrl.Result is required by the interface signature
func (r *TrainingRoundReconciler) handleAggregating(ctx context.Context, round *propellerv1alpha1.TrainingRound) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var collectedUpdates []flpkg.UpdateEnvelope
	if updatesJSON, ok := round.Annotations["propeller.absmach.io/collected-updates"]; ok {
		if err := json.Unmarshal([]byte(updatesJSON), &collectedUpdates); err != nil {
			logger.Error(err, "failed to unmarshal collected updates")
		}
	}

	algorithm := "fedavg"
	if round.Spec.FederatedJobRef.Name != "" {
		federatedJob := &propellerv1alpha1.FederatedJob{}
		if err := r.Get(ctx, client.ObjectKey{Name: round.Spec.FederatedJobRef.Name, Namespace: round.Namespace}, federatedJob); err == nil {
			if federatedJob.Spec.Aggregator.Algorithm != "" {
				algorithm = federatedJob.Spec.Aggregator.Algorithm
			}
		}
	}

	aggregatedModelRef := r.aggregateUpdates(ctx, round, collectedUpdates, algorithm)
	if aggregatedModelRef == "" {
		return ctrl.Result{}, nil
	}

	now := metav1.Now()
	round.Status.EndTime = &now
	round.Status.Phase = phaseCompleted
	round.Status.AggregatedModelRef = aggregatedModelRef
	federatedJobName := ""
	if round.Spec.FederatedJobRef.Name != "" {
		federatedJobName = round.Spec.FederatedJobRef.Name
	}

	// Calculate round duration
	var duration float64
	if round.Status.StartTime != nil {
		duration = now.Sub(round.Status.StartTime.Time).Seconds()
	}

	// Update metrics
	metrics.FLRoundTotal.WithLabelValues(round.Namespace, federatedJobName, phaseCompleted).Inc()
	if duration > 0 {
		metrics.FLRoundDuration.WithLabelValues(round.Namespace, federatedJobName).Observe(duration)
	}

	r.updateCondition(round, "True", "Completed", fmt.Sprintf("Round completed and aggregated %d updates", len(collectedUpdates)))

	if err := r.Status().Update(ctx, round); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("round completed", "round", round.Name, "aggregatedModel", round.Status.AggregatedModelRef, "updates", len(collectedUpdates))

	return ctrl.Result{}, nil
}

func (r *TrainingRoundReconciler) processParticipants(ctx context.Context, round *propellerv1alpha1.TrainingRound) (int, []flpkg.UpdateEnvelope) {
	logger := log.FromContext(ctx)
	updatesReceived := 0
	collectedUpdates := make([]flpkg.UpdateEnvelope, 0)

	for i, participant := range round.Status.Participants {
		if participant.TaskRef == nil {
			continue
		}

		wasmTask := &propellerv1alpha1.WasmTask{}
		if err := r.Get(ctx, client.ObjectKey{
			Name:      participant.TaskRef.Name,
			Namespace: participant.TaskRef.Namespace,
		}, wasmTask); err != nil {
			logger.Error(err, "failed to get wasmtask", "wasmtask", participant.TaskRef.Name)

			continue
		}

		switch wasmTask.Status.Phase {
		case phaseCompleted:
			if participant.UpdateReceived {
				break
			}
			updatesReceived++
			round.Status.Participants[i].UpdateReceived = true
			round.Status.Participants[i].Status = phaseCompleted

			env, ok := processCompletedParticipant(&round.Status.Participants[i], wasmTask, round)
			if ok {
				collectedUpdates = append(collectedUpdates, env)
				federatedJobName := ""
				if round.Spec.FederatedJobRef.Name != "" {
					federatedJobName = round.Spec.FederatedJobRef.Name
				}
				metrics.FLUpdatesCollected.WithLabelValues(round.Namespace, federatedJobName, round.Spec.RoundID).Inc()
				logger.Info("collected FL update", "proplet", participant.PropletID, "round", round.Spec.RoundID)
			} else {
				r.logMissingUpdate(ctx, wasmTask)
			}
		case phaseFailed:
			round.Status.Participants[i].Status = phaseFailed
			logger.Info("participant task failed", "participant", participant.PropletID, "task", participant.TaskRef.Name)
		case phaseRunning, phasePending:
		}
	}

	return updatesReceived, collectedUpdates
}

func (r *TrainingRoundReconciler) logMissingUpdate(ctx context.Context, wasmTask *propellerv1alpha1.WasmTask) {
	logger := log.FromContext(ctx)
	if wasmTask.Status.Results == nil {
		logger.Info("task completed but no results found", "task", wasmTask.Name)
	} else {
		logger.Info("task completed but no FL update found", "task", wasmTask.Name)
	}
}

func (r *TrainingRoundReconciler) storeCollectedUpdates(round *propellerv1alpha1.TrainingRound, collectedUpdates []flpkg.UpdateEnvelope) {
	updatesJSON, err := json.Marshal(collectedUpdates)
	if err == nil {
		if round.Annotations == nil {
			round.Annotations = make(map[string]string)
		}
		round.Annotations["propeller.absmach.io/collected-updates"] = string(updatesJSON)
	}
}

func (r *TrainingRoundReconciler) transitionToAggregating(ctx context.Context, round *propellerv1alpha1.TrainingRound, updatesReceived int) (ctrl.Result, error) {
	round.Status.Phase = "Aggregating"
	r.updateCondition(round, "True", "Aggregating", fmt.Sprintf("Collected %d updates, starting aggregation", updatesReceived))
	if err := r.Status().Update(ctx, round); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 2}, nil
}

func (r *TrainingRoundReconciler) checkTimeout(ctx context.Context, round *propellerv1alpha1.TrainingRound, updatesReceived int) *ctrl.Result {
	if round.Spec.TimeoutSeconds > 0 && round.Status.StartTime != nil {
		elapsed := time.Since(round.Status.StartTime.Time)
		if elapsed > time.Duration(round.Spec.TimeoutSeconds)*time.Second {
			round.Status.Phase = phaseFailed
			r.updateCondition(round, "False", "Timeout", fmt.Sprintf("Round timed out: only %d/%d updates received", updatesReceived, round.Spec.KOfN))
			if err := r.Status().Update(ctx, round); err != nil {
				return &ctrl.Result{}
			}

			return &ctrl.Result{}
		}
	}

	return nil
}

func (r *TrainingRoundReconciler) aggregateUpdates(ctx context.Context, round *propellerv1alpha1.TrainingRound, collectedUpdates []flpkg.UpdateEnvelope, algorithm string) string {
	startTime := time.Now()
	logger := log.FromContext(ctx)
	namespace := round.Namespace
	federatedJobName := ""
	if round.Spec.FederatedJobRef.Name != "" {
		federatedJobName = round.Spec.FederatedJobRef.Name
	}

	if len(collectedUpdates) == 0 {
		aggregatedModelRef := fmt.Sprintf("oci://registry/model:%s-fallback", round.Spec.RoundID)
		logger.Info("no updates collected, using fallback model reference")

		return aggregatedModelRef
	}

	aggEnv, err := AggregateFLUpdates(collectedUpdates, algorithm)
	if err != nil {
		logger.Error(err, "failed to aggregate updates")
		round.Status.Phase = phaseFailed
		r.updateCondition(round, "False", "AggregationFailed", fmt.Sprintf("Failed to aggregate: %v", err))
		if err := r.Status().Update(ctx, round); err != nil {
			return ""
		}

		return ""
	}

	r.storeAggregatedUpdate(round, aggEnv)
	aggregatedModelRef := fmt.Sprintf("oci://registry/model:%s-aggregated", round.Spec.RoundID)
	logger.Info("aggregation completed", "updates", len(collectedUpdates), "algorithm", algorithm)

	// Update metrics
	duration := time.Since(startTime).Seconds()
	metrics.FLAggregationsTotal.WithLabelValues(namespace, federatedJobName, algorithm).Inc()
	metrics.FLAggregationDuration.WithLabelValues(namespace, federatedJobName, algorithm).Observe(duration)

	return aggregatedModelRef
}

func (r *TrainingRoundReconciler) storeAggregatedUpdate(round *propellerv1alpha1.TrainingRound, aggEnv flpkg.UpdateEnvelope) {
	aggJSON, err := json.Marshal(aggEnv)
	if err == nil {
		if round.Annotations == nil {
			round.Annotations = make(map[string]string)
		}
		round.Annotations["propeller.absmach.io/aggregated-update"] = string(aggJSON)
	}
}

func (r *TrainingRoundReconciler) updateStatusAndRequeue(ctx context.Context, round *propellerv1alpha1.TrainingRound, requeueAfter time.Duration) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, round); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *TrainingRoundReconciler) updateCondition(round *propellerv1alpha1.TrainingRound, status, reason, message string) {
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionStatus(status),
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	found := false
	for i, c := range round.Status.Conditions {
		if c.Type == "Ready" {
			round.Status.Conditions[i] = condition
			found = true

			break
		}
	}
	if !found {
		round.Status.Conditions = append(round.Status.Conditions, condition)
	}
}
