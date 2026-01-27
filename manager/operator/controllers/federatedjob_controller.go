package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	propellerv1alpha1 "github.com/absmach/propeller/api/v1alpha1"
	"github.com/absmach/propeller/manager/operator/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type FederatedJobReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=propeller.absmach.io,resources=federatedjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=federatedjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=federatedjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=trainingrounds,verbs=get;list;watch;create;update;patch;delete

func (r *FederatedJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)
	namespace := req.Namespace

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ReconcileDuration.WithLabelValues("federatedjob", namespace).Observe(duration)
	}()

	federatedJob := &propellerv1alpha1.FederatedJob{}
	if err := r.Get(ctx, req.NamespacedName, federatedJob); err != nil {
		metrics.ReconcileTotal.WithLabelValues("federatedjob", namespace, "not_found").Inc()

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if federatedJob.Status.Phase == "" {
		federatedJob.Status.Phase = phasePending
		metrics.FLRoundTotal.WithLabelValues(namespace, federatedJob.Name, phasePending).Inc()
		if err := r.Status().Update(ctx, federatedJob); err != nil {
			metrics.ReconcileTotal.WithLabelValues("federatedjob", namespace, "error").Inc()

			return ctrl.Result{}, err
		}
	}

	var result ctrl.Result
	var err error

	switch federatedJob.Status.Phase {
	case phasePending:
		result, err = r.handlePending(ctx, federatedJob)
	case phaseRunning:
		result, err = r.handleRunning(ctx, federatedJob)
	case phaseCompleted, phaseFailed:
		metrics.ReconcileTotal.WithLabelValues("federatedjob", namespace, "noop").Inc()

		return ctrl.Result{}, nil
	default:
		logger.Info("unknown phase", "phase", federatedJob.Status.Phase)
		metrics.ReconcileTotal.WithLabelValues("federatedjob", namespace, "unknown_phase").Inc()

		return ctrl.Result{}, nil
	}

	if err != nil {
		metrics.ReconcileTotal.WithLabelValues("federatedjob", namespace, "error").Inc()
	} else {
		metrics.ReconcileTotal.WithLabelValues("federatedjob", namespace, "success").Inc()
	}

	return result, err
}

func (r *FederatedJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&propellerv1alpha1.FederatedJob{}).
		Owns(&propellerv1alpha1.TrainingRound{}).
		Complete(r)
}

//nolint:unparam // ctrl.Result is required by the interface signature
func (r *FederatedJobReconciler) handlePending(ctx context.Context, job *propellerv1alpha1.FederatedJob) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.validateSpec(job); err != nil {
		logger.Error(err, "invalid spec")
		job.Status.Phase = phaseFailed
		r.updateCondition(job, "False", "InvalidSpec", err.Error())
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	roundName := fmt.Sprintf("round-%d-%s", 1, job.Name)
	round := &propellerv1alpha1.TrainingRound{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roundName,
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: job.APIVersion,
					Kind:       job.Kind,
					Name:       job.Name,
					UID:        job.UID,
					Controller: func() *bool {
						b := true

						return &b
					}(),
				},
			},
		},
		Spec: propellerv1alpha1.TrainingRoundSpec{
			RoundID: fmt.Sprintf("round-%d", 1),
			FederatedJobRef: corev1.LocalObjectReference{
				Name: job.Name,
			},
			ModelRef:       job.Spec.ModelRef,
			TaskWasmImage:  job.Spec.TaskWasmImage,
			Participants:   r.getParticipantIDs(job.Spec.Participants),
			Hyperparams:    job.Spec.Hyperparams,
			KOfN:           job.Spec.KOfN,
			TimeoutSeconds: job.Spec.TimeoutSeconds,
		},
	}

	if err := r.Create(ctx, round); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create training round: %w", err)
	}

	job.Status.Phase = phaseRunning
	job.Status.CurrentRound = 1
	r.updateCondition(job, "True", "Running", "Job is running")
	if err := r.Status().Update(ctx, job); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *FederatedJobReconciler) handleRunning(ctx context.Context, job *propellerv1alpha1.FederatedJob) (ctrl.Result, error) {
	roundName := fmt.Sprintf("round-%d-%s", job.Status.CurrentRound, job.Name)
	round := &propellerv1alpha1.TrainingRound{}
	if err := r.Get(ctx, client.ObjectKey{Name: roundName, Namespace: job.Namespace}, round); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch round.Status.Phase {
	case phaseCompleted:
		job.Status.CompletedRounds++
		if round.Status.AggregatedModelRef != "" {
			job.Status.AggregatedModelRef = round.Status.AggregatedModelRef
		}

		if job.Status.CompletedRounds >= job.Spec.Rounds.Total {
			job.Status.Phase = phaseCompleted
			r.updateCondition(job, "True", "Completed", "All rounds completed")
		} else {
			nextRoundNum := job.Status.CurrentRound + 1
			nextRoundName := fmt.Sprintf("round-%d-%s", nextRoundNum, job.Name)
			modelRef := round.Status.AggregatedModelRef
			if modelRef == "" {
				modelRef = job.Spec.ModelRef
			}

			nextRoundAnnotations := make(map[string]string)
			if aggregatedUpdateJSON, ok := round.Annotations["propeller.absmach.io/aggregated-update"]; ok {
				nextRoundAnnotations["propeller.absmach.io/aggregated-update"] = aggregatedUpdateJSON
			}

			nextRound := &propellerv1alpha1.TrainingRound{
				ObjectMeta: metav1.ObjectMeta{
					Name:        nextRoundName,
					Namespace:   job.Namespace,
					Annotations: nextRoundAnnotations,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: job.APIVersion,
							Kind:       job.Kind,
							Name:       job.Name,
							UID:        job.UID,
							Controller: func() *bool {
								b := true

								return &b
							}(),
						},
					},
				},
				Spec: propellerv1alpha1.TrainingRoundSpec{
					RoundID: fmt.Sprintf("round-%d", nextRoundNum),
					FederatedJobRef: corev1.LocalObjectReference{
						Name: job.Name,
					},
					ModelRef:       modelRef,
					TaskWasmImage:  job.Spec.TaskWasmImage,
					Participants:   r.getParticipantIDs(job.Spec.Participants),
					Hyperparams:    job.Spec.Hyperparams,
					KOfN:           job.Spec.KOfN,
					TimeoutSeconds: job.Spec.TimeoutSeconds,
				},
			}

			if err := r.Create(ctx, nextRound); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create next training round: %w", err)
			}

			job.Status.CurrentRound = nextRoundNum
		}

		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Second * 5}, nil

	case phaseFailed:
		job.Status.Phase = phaseFailed
		r.updateCondition(job, "False", "RoundFailed", "Training round failed")
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil

	default:
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}
}

func (r *FederatedJobReconciler) validateSpec(job *propellerv1alpha1.FederatedJob) error {
	if job.Spec.ExperimentID == "" {
		return errors.New("experimentId is required")
	}
	if job.Spec.ModelRef == "" {
		return errors.New("modelRef is required")
	}
	if job.Spec.TaskWasmImage == "" {
		return errors.New("taskWasmImage is required")
	}
	if len(job.Spec.Participants) == 0 {
		return errors.New("at least one participant is required")
	}
	if job.Spec.KOfN <= 0 {
		return errors.New("kOfN must be greater than 0")
	}
	if job.Spec.KOfN > len(job.Spec.Participants) {
		return errors.New("kOfN cannot be greater than number of participants")
	}

	return nil
}

func (r *FederatedJobReconciler) getParticipantIDs(participants []propellerv1alpha1.ParticipantSpec) []string {
	ids := make([]string, len(participants))
	for i, p := range participants {
		ids[i] = p.PropletID
	}

	return ids
}

func (r *FederatedJobReconciler) updateCondition(job *propellerv1alpha1.FederatedJob, status, reason, message string) {
	now := time.Now()
	condition := &metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionStatus(status),
		LastTransitionTime: metav1.NewTime(now),
		Reason:             reason,
		Message:            message,
	}

	found := false
	for i, c := range job.Status.Conditions {
		if c.Type == "Ready" {
			job.Status.Conditions[i] = *condition
			found = true

			break
		}
	}
	if !found {
		job.Status.Conditions = append(job.Status.Conditions, *condition)
	}
}
