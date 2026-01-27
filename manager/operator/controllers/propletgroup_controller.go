package controllers

import (
	"context"
	"fmt"
	"time"

	propellerv1alpha1 "github.com/absmach/propeller/api/v1alpha1"
	"github.com/absmach/propeller/manager/operator/metrics"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type PropletGroupReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=propeller.absmach.io,resources=propletgroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=propletgroups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=propeller.absmach.io,resources=propletgroups/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *PropletGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)
	namespace := req.Namespace

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ReconcileDuration.WithLabelValues("propletgroup", namespace).Observe(duration)
	}()

	group := &propellerv1alpha1.PropletGroup{}
	if err := r.Get(ctx, req.NamespacedName, group); err != nil {
		metrics.ReconcileTotal.WithLabelValues("propletgroup", namespace, "not_found").Inc()

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("reconciling proplet group", "group", group.Name)

	selector := labels.SelectorFromSet(group.Spec.Selector.MatchLabels)
	proplets := r.collectPropletsFromPods(ctx, selector)
	if len(proplets) == 0 {
		proplets = r.collectPropletsFromNodes(ctx, selector)
	}

	availableCount := r.countAvailableProplets(proplets)

	// Update metrics
	aliveCount := 0
	for i := range proplets {
		proplet := proplets[i]
		if proplet.Alive {
			aliveCount++
			metrics.PropletTotal.WithLabelValues(namespace, "true").Inc()
			metrics.PropletTaskCount.WithLabelValues(namespace, proplet.ID).Set(float64(proplet.TaskCount))
		} else {
			metrics.PropletTotal.WithLabelValues(namespace, "false").Inc()
		}
	}
	metrics.PropletActive.WithLabelValues(namespace).Set(float64(aliveCount))

	group.Status.Proplets = proplets
	group.Status.TotalProplets = len(proplets)
	group.Status.AvailableProplets = availableCount

	if err := r.Status().Update(ctx, group); err != nil {
		metrics.ReconcileTotal.WithLabelValues("propletgroup", namespace, "error").Inc()

		return ctrl.Result{}, err
	}

	logger.Info("updated proplet group", "total", group.Status.TotalProplets, "available", group.Status.AvailableProplets)
	metrics.ReconcileTotal.WithLabelValues("propletgroup", namespace, "success").Inc()

	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
}

func (r *PropletGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&propellerv1alpha1.PropletGroup{}).
		Complete(r)
}

func (r *PropletGroupReconciler) SelectPropletFromGroup(ctx context.Context, group *propellerv1alpha1.PropletGroup) (string, error) {
	if len(group.Status.Proplets) == 0 {
		return "", fmt.Errorf("no proplets available in group %s", group.Name)
	}

	aliveProplets := make([]propellerv1alpha1.PropletInfo, 0)
	for i := range group.Status.Proplets {
		if group.Status.Proplets[i].Alive {
			aliveProplets = append(aliveProplets, group.Status.Proplets[i])
		}
	}

	if len(aliveProplets) == 0 {
		return "", fmt.Errorf("no alive proplets in group %s", group.Name)
	}

	switch group.Spec.Scheduling.Algorithm {
	case "round-robin":
		selected := aliveProplets[0]
		for i := range aliveProplets {
			if aliveProplets[i].TaskCount < selected.TaskCount {
				selected = aliveProplets[i]
			}
		}

		return selected.ID, nil

	case "least-loaded":
		selected := aliveProplets[0]
		for i := range aliveProplets {
			if aliveProplets[i].TaskCount < selected.TaskCount {
				selected = aliveProplets[i]
			}
		}

		return selected.ID, nil

	case "random":
		selected := aliveProplets[time.Now().UnixNano()%int64(len(aliveProplets))]

		return selected.ID, nil

	default:
		return aliveProplets[0].ID, nil
	}
}

func (r *PropletGroupReconciler) collectPropletsFromPods(ctx context.Context, selector labels.Selector) []propellerv1alpha1.PropletInfo {
	proplets := make([]propellerv1alpha1.PropletInfo, 0)
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return proplets
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		propletID := r.getPropletIDFromPod(pod)
		taskCount := r.countTasksForPod(ctx, pod)

		proplets = append(proplets, propellerv1alpha1.PropletInfo{
			ID:            propletID,
			Namespace:     pod.Namespace,
			Alive:         true,
			TaskCount:     taskCount,
			LastHeartbeat: pod.Status.StartTime,
		})
	}

	return proplets
}

func (r *PropletGroupReconciler) collectPropletsFromNodes(ctx context.Context, selector labels.Selector) []propellerv1alpha1.PropletInfo {
	proplets := make([]propellerv1alpha1.PropletInfo, 0)
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return proplets
	}

	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		propletID := r.getPropletIDFromNode(node)
		alive := r.isNodeAlive(node)

		proplets = append(proplets, propellerv1alpha1.PropletInfo{
			ID:            propletID,
			Namespace:     "",
			Alive:         alive,
			TaskCount:     0,
			LastHeartbeat: &metav1.Time{Time: time.Now()},
		})
	}

	return proplets
}

func (r *PropletGroupReconciler) getPropletIDFromPod(pod *corev1.Pod) string {
	if propletID := pod.Labels["proplet-id"]; propletID != "" {
		return propletID
	}

	return pod.Name
}

func (r *PropletGroupReconciler) getPropletIDFromNode(node *corev1.Node) string {
	if propletID := node.Labels["proplet-id"]; propletID != "" {
		return propletID
	}

	return node.Name
}

func (r *PropletGroupReconciler) countTasksForPod(ctx context.Context, pod *corev1.Pod) int {
	taskCount := 0
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList, client.InNamespace(pod.Namespace)); err != nil {
		return taskCount
	}

	for j := range jobList.Items {
		job := &jobList.Items[j]
		if job.Spec.Template.Spec.NodeSelector == nil {
			continue
		}
		if nodeName, ok := job.Spec.Template.Spec.NodeSelector["kubernetes.io/hostname"]; ok {
			if nodeName == pod.Spec.NodeName {
				taskCount++
			}
		}
	}

	return taskCount
}

func (r *PropletGroupReconciler) isNodeAlive(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return true
}

func (r *PropletGroupReconciler) countAvailableProplets(proplets []propellerv1alpha1.PropletInfo) int {
	count := 0
	for i := range proplets {
		if proplets[i].Alive {
			count++
		}
	}

	return count
}
