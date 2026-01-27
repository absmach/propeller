package controllers

import (
	"context"
	"encoding/json"
	"time"

	propellerv1alpha1 "github.com/absmach/propeller/api/v1alpha1"
	"github.com/absmach/propeller/manager/operator/metrics"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type WasmTaskReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=propeller.absmach.io,resources=wasmtasks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=wasmtasks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=wasmtasks/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=propletgroups,verbs=get;list;watch

func (r *WasmTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)
	namespace := req.Namespace

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ReconcileDuration.WithLabelValues("wasmtask", namespace).Observe(duration)
	}()

	task := &propellerv1alpha1.WasmTask{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		metrics.ReconcileTotal.WithLabelValues("wasmtask", namespace, "not_found").Inc()

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if task.Status.Phase == "" {
		task.Status.Phase = phasePending
		if err := r.Status().Update(ctx, task); err != nil {
			metrics.ReconcileTotal.WithLabelValues("wasmtask", namespace, "error").Inc()

			return ctrl.Result{}, err
		}
	}

	var result ctrl.Result
	var err error

	switch task.Status.Phase {
	case phasePending:
		result, err = r.handlePending(ctx, task)
	case phaseScheduled, phaseRunning:
		result, err = r.handleRunning(ctx, task)
	case phaseCompleted, phaseFailed:
		metrics.ReconcileTotal.WithLabelValues("wasmtask", namespace, "noop").Inc()

		return ctrl.Result{}, nil
	default:
		logger.Info("unknown phase", "phase", task.Status.Phase)
		metrics.ReconcileTotal.WithLabelValues("wasmtask", namespace, "unknown_phase").Inc()

		return ctrl.Result{}, nil
	}

	if err != nil {
		metrics.ReconcileTotal.WithLabelValues("wasmtask", namespace, "error").Inc()
	} else {
		metrics.ReconcileTotal.WithLabelValues("wasmtask", namespace, "success").Inc()
	}

	return result, err
}

func (r *WasmTaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&propellerv1alpha1.WasmTask{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func (r *WasmTaskReconciler) handlePending(ctx context.Context, task *propellerv1alpha1.WasmTask) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	propletID := r.resolvePropletID(ctx, task)
	if propletID == "" {
		propletID = defaultPropletID
		logger.Info("no proplet specified, using default", "proplet", propletID)
	}

	configMapName := task.Name + "-config"
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: task.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: task.APIVersion,
					Kind:       task.Kind,
					Name:       task.Name,
					UID:        task.UID,
					Controller: func() *bool {
						b := true

						return &b
					}(),
				},
			},
		},
		Data: task.Spec.Env,
	}

	if len(task.Spec.File) > 0 {
		configMap.Data["wasm_file_provided"] = "true"
	}

	if err := r.Create(ctx, configMap); err != nil {
		if client.IgnoreAlreadyExists(err) == nil {
			return ctrl.Result{}, err
		}
	}

	jobName := task.Name + "-job"
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: task.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: task.APIVersion,
					Kind:       task.Kind,
					Name:       task.Name,
					UID:        task.UID,
					Controller: func() *bool {
						b := true

						return &b
					}(),
				},
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: func() corev1.RestartPolicy {
						if task.Spec.RestartPolicy != "" {
							return task.Spec.RestartPolicy
						}
						if task.Spec.Daemon {
							return corev1.RestartPolicyAlways
						}

						return corev1.RestartPolicyOnFailure
					}(),
					Containers: []corev1.Container{
						{
							Name:  "wasm-task",
							Image: task.Spec.ImageURL,
							Args:  task.Spec.CLIArgs,
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: configMapName,
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "PROPLET_ID",
									Value: propletID,
								},
								{
									Name:  "TASK_ID",
									Value: task.Name,
								},
							},
							Resources: func() corev1.ResourceRequirements {
								if task.Spec.Resources != nil {
									return *task.Spec.Resources
								}

								return corev1.ResourceRequirements{}
							}(),
						},
					},
				},
			},
		},
	}

	if err := r.Create(ctx, job); err != nil {
		logger.Error(err, "failed to create job", "job", jobName)

		return ctrl.Result{}, err
	}

	now := metav1.Now()
	task.Status.Phase = phaseRunning
	task.Status.PropletID = propletID
	task.Status.StartTime = &now
	task.Status.JobRef = &corev1.ObjectReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       jobName,
		Namespace:  task.Namespace,
	}

	// Update metrics
	metrics.TaskTotal.WithLabelValues(task.Namespace, phaseRunning).Inc()
	metrics.TaskActive.WithLabelValues(task.Namespace, phaseRunning).Inc()
	metrics.TaskActive.WithLabelValues(task.Namespace, phasePending).Dec()

	r.updateCondition(task, "Ready", "True", "Running", "Task is running")
	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func (r *WasmTaskReconciler) handleRunning(ctx context.Context, task *propellerv1alpha1.WasmTask) (ctrl.Result, error) {
	if task.Status.JobRef == nil {
		return ctrl.Result{}, nil
	}

	job := &batchv1.Job{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      task.Status.JobRef.Name,
		Namespace: task.Status.JobRef.Namespace,
	}, job); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if job.Status.Succeeded > 0 {
		return r.handleJobSucceeded(ctx, task, job)
	}

	if job.Status.Failed > 0 {
		return r.handleJobFailed(ctx, task, job)
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

func (r *WasmTaskReconciler) resolvePropletID(ctx context.Context, task *propellerv1alpha1.WasmTask) string {
	logger := log.FromContext(ctx)
	propletID := task.Spec.PropletID
	if propletID != "" {
		return propletID
	}

	if task.Spec.PropletGroupRef == nil {
		return ""
	}

	group := &propellerv1alpha1.PropletGroup{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      task.Spec.PropletGroupRef.Name,
		Namespace: task.Namespace,
	}, group); err != nil {
		logger.Error(err, "failed to get proplet group", "group", task.Spec.PropletGroupRef.Name)

		return defaultPropletID
	}

	return r.selectPropletFromGroup(ctx, group)
}

func (r *WasmTaskReconciler) selectPropletFromGroup(ctx context.Context, group *propellerv1alpha1.PropletGroup) string {
	logger := log.FromContext(ctx)
	groupReconciler := &PropletGroupReconciler{Client: r.Client, Scheme: r.Scheme}
	selectedID, err := groupReconciler.SelectPropletFromGroup(ctx, group)
	if err != nil {
		logger.Error(err, "failed to select proplet from group", "group", group.Name)
		selectedID = r.findFirstAliveProplet(group)
	} else {
		logger.Info("selected proplet from group", "proplet", selectedID, "group", group.Name)
	}

	if selectedID == "" {
		selectedID = defaultPropletID
		logger.Info("using fallback proplet", "proplet", selectedID)
	}

	return selectedID
}

func (r *WasmTaskReconciler) findFirstAliveProplet(group *propellerv1alpha1.PropletGroup) string {
	if len(group.Status.Proplets) == 0 {
		return ""
	}

	for i := range group.Status.Proplets {
		if group.Status.Proplets[i].Alive {
			return group.Status.Proplets[i].ID
		}
	}

	return ""
}

func (r *WasmTaskReconciler) handleJobSucceeded(ctx context.Context, task *propellerv1alpha1.WasmTask, job *batchv1.Job) (ctrl.Result, error) {
	now := metav1.Now()
	task.Status.Phase = phaseCompleted
	task.Status.FinishTime = &now

	// Calculate task duration
	var duration float64
	if task.Status.StartTime != nil {
		duration = now.Sub(task.Status.StartTime.Time).Seconds()
	}

	r.extractAndStoreResult(ctx, task, job)

	// Update metrics
	metrics.TaskTotal.WithLabelValues(task.Namespace, phaseCompleted).Inc()
	metrics.TaskDuration.WithLabelValues(task.Namespace, phaseCompleted).Observe(duration)
	metrics.TaskActive.WithLabelValues(task.Namespace, phaseRunning).Dec()
	metrics.JobTotal.WithLabelValues(task.Namespace, "succeeded").Inc()
	if duration > 0 {
		metrics.JobDuration.WithLabelValues(task.Namespace, "succeeded").Observe(duration)
	}

	r.updateCondition(task, "Ready", "True", "Completed", "Task completed successfully")

	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WasmTaskReconciler) handleJobFailed(ctx context.Context, task *propellerv1alpha1.WasmTask, job *batchv1.Job) (ctrl.Result, error) {
	now := metav1.Now()
	task.Status.Phase = phaseFailed
	task.Status.FinishTime = &now

	// Calculate task duration
	var duration float64
	if task.Status.StartTime != nil {
		duration = now.Sub(task.Status.StartTime.Time).Seconds()
	}

	errorMsg := r.extractJobFailureMessage(job)
	task.Status.Error = errorMsg

	// Update metrics
	metrics.TaskTotal.WithLabelValues(task.Namespace, phaseFailed).Inc()
	metrics.TaskDuration.WithLabelValues(task.Namespace, phaseFailed).Observe(duration)
	metrics.TaskActive.WithLabelValues(task.Namespace, phaseRunning).Dec()
	metrics.JobTotal.WithLabelValues(task.Namespace, "failed").Inc()
	if duration > 0 {
		metrics.JobDuration.WithLabelValues(task.Namespace, "failed").Observe(duration)
	}

	r.updateCondition(task, "Ready", "False", "Failed", errorMsg)

	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WasmTaskReconciler) extractJobFailureMessage(job *batchv1.Job) string {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Message != "" {
			return condition.Message
		}
	}

	return "Job failed"
}

func (r *WasmTaskReconciler) extractAndStoreResult(ctx context.Context, task *propellerv1alpha1.WasmTask, job *batchv1.Job) {
	logger := log.FromContext(ctx)
	result, err := ExtractResultFromJob(ctx, r.Client, job)
	if err != nil {
		logger.Error(err, "failed to extract result from job", "job", job.Name)

		return
	}

	if result == nil {
		return
	}

	resultJSON, err := json.Marshal(result)
	if err == nil {
		task.Status.Results = &apiextensionsv1.JSON{Raw: resultJSON}
	}
}

func (r *WasmTaskReconciler) updateCondition(task *propellerv1alpha1.WasmTask, conditionType, status, reason, message string) {
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionStatus(status),
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	found := false
	for i, c := range task.Status.Conditions {
		if c.Type == conditionType {
			task.Status.Conditions[i] = condition
			found = true

			break
		}
	}
	if !found {
		task.Status.Conditions = append(task.Status.Conditions, condition)
	}
}
