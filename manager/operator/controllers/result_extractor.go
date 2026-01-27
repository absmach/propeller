package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/absmach/propeller/manager/operator/metrics"
	flpkg "github.com/absmach/propeller/pkg/fl"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrNoResult = errors.New("no result found")

// ExtractResultFromJob extracts result from a Kubernetes Job using multiple fallback mechanisms:
// 1. Job annotations (fastest)
// 2. Pod termination message
// 3. ConfigMap with result
// 4. Secret with result
// 5. Pod logs (last resort, requires log access).
func ExtractResultFromJob(ctx context.Context, c client.Client, job *batchv1.Job) (map[string]any, error) {
	startTime := time.Now()
	namespace := job.Namespace

	// Fallback 1: Job annotations (fastest, most reliable)
	if resultJSON, ok := job.Annotations["propeller.absmach.io/result"]; ok {
		var result map[string]any
		if err := json.Unmarshal([]byte(resultJSON), &result); err == nil {
			metrics.ResultExtractionTotal.WithLabelValues(namespace, "annotation", "success").Inc()
			metrics.ResultExtractionDuration.WithLabelValues(namespace, "annotation").Observe(time.Since(startTime).Seconds())

			return result, nil
		}
		metrics.ResultExtractionTotal.WithLabelValues(namespace, "annotation", "parse_error").Inc()
	}

	// Get pods for this job
	podList := &corev1.PodList{}
	labels := job.Spec.Selector.MatchLabels
	if err := c.List(ctx, podList, client.MatchingLabels(labels), client.InNamespace(job.Namespace)); err != nil {
		metrics.ResultExtractionTotal.WithLabelValues(namespace, "pod_list", "error").Inc()

		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var succeededPod *corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase == corev1.PodSucceeded {
			succeededPod = &podList.Items[i]

			break
		}
	}

	if succeededPod == nil {
		metrics.ResultExtractionTotal.WithLabelValues(namespace, "pod", "not_found").Inc()

		return nil, fmt.Errorf("%w: no succeeded pod found", ErrNoResult)
	}

	// Fallback 2: Pod termination message
	if len(succeededPod.Status.ContainerStatuses) > 0 {
		cs := succeededPod.Status.ContainerStatuses[0]
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			var result map[string]any
			if err := json.Unmarshal([]byte(cs.State.Terminated.Message), &result); err == nil {
				metrics.ResultExtractionTotal.WithLabelValues(namespace, "termination_message", "success").Inc()
				metrics.ResultExtractionDuration.WithLabelValues(namespace, "termination_message").Observe(time.Since(startTime).Seconds())

				return result, nil
			}
			metrics.ResultExtractionTotal.WithLabelValues(namespace, "termination_message", "parse_error").Inc()
		}
	}

	// Fallback 3: ConfigMap with result
	configMapName := job.Name + "-result"
	configMap := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: job.Namespace}, configMap); err == nil {
		if resultJSON, ok := configMap.Data["result"]; ok {
			var result map[string]any
			if err := json.Unmarshal([]byte(resultJSON), &result); err == nil {
				metrics.ResultExtractionTotal.WithLabelValues(namespace, "configmap", "success").Inc()
				metrics.ResultExtractionDuration.WithLabelValues(namespace, "configmap").Observe(time.Since(startTime).Seconds())

				return result, nil
			}
			metrics.ResultExtractionTotal.WithLabelValues(namespace, "configmap", "parse_error").Inc()
		}
	}

	// Fallback 4: Secret with result
	secretName := job.Name + "-result"
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: job.Namespace}, secret); err == nil {
		if resultData, ok := secret.Data["result"]; ok {
			var result map[string]any
			if err := json.Unmarshal(resultData, &result); err == nil {
				metrics.ResultExtractionTotal.WithLabelValues(namespace, "secret", "success").Inc()
				metrics.ResultExtractionDuration.WithLabelValues(namespace, "secret").Observe(time.Since(startTime).Seconds())

				return result, nil
			}
			metrics.ResultExtractionTotal.WithLabelValues(namespace, "secret", "parse_error").Inc()
		}
	}

	// Fallback 5: Check for result in pod annotations
	if resultJSON, ok := succeededPod.Annotations["propeller.absmach.io/result"]; ok {
		var result map[string]any
		if err := json.Unmarshal([]byte(resultJSON), &result); err == nil {
			metrics.ResultExtractionTotal.WithLabelValues(namespace, "pod_annotation", "success").Inc()
			metrics.ResultExtractionDuration.WithLabelValues(namespace, "pod_annotation").Observe(time.Since(startTime).Seconds())

			return result, nil
		}
		metrics.ResultExtractionTotal.WithLabelValues(namespace, "pod_annotation", "parse_error").Inc()
	}

	// All fallbacks exhausted
	metrics.ResultExtractionTotal.WithLabelValues(namespace, "all", "not_found").Inc()
	metrics.ResultExtractionDuration.WithLabelValues(namespace, "all").Observe(time.Since(startTime).Seconds())

	return nil, fmt.Errorf("%w: tried all extraction methods", ErrNoResult)
}

func ExtractFLUpdateFromResult(result map[string]any) (flpkg.UpdateEnvelope, error) {
	if env, ok := result["update_envelope"].(map[string]any); ok {
		return parseUpdateEnvelope(env)
	}

	if results, ok := result["results"].(map[string]any); ok {
		if env, ok := results["update_envelope"].(map[string]any); ok {
			return parseUpdateEnvelope(env)
		}
	}

	return parseUpdateEnvelope(result)
}

func parseUpdateEnvelope(data map[string]any) (flpkg.UpdateEnvelope, error) {
	var env flpkg.UpdateEnvelope

	switch v := data["round_id"].(type) {
	case string:
		var roundIDNum uint64
		if _, err := fmt.Sscanf(v, "%d", &roundIDNum); err == nil {
			env.RoundID = roundIDNum
		}
	case float64:
		env.RoundID = uint64(v)
	case uint64:
		env.RoundID = v
	}

	if jobID, ok := data["job_id"].(string); ok {
		env.JobID = jobID
	}
	if propletID, ok := data["proplet_id"].(string); ok {
		env.PropletID = propletID
	}
	if globalVersion, ok := data["global_version"].(string); ok {
		env.GlobalVersion = globalVersion
	}
	if updateB64, ok := data["update_b64"].(string); ok {
		env.UpdateB64 = updateB64
	}
	if format, ok := data["format"].(string); ok {
		env.Format = format
	}

	switch v := data["num_samples"].(type) {
	case float64:
		env.NumSamples = uint64(v)
	case uint64:
		env.NumSamples = v
	}

	if metricsData, ok := data["metrics"].(map[string]any); ok {
		env.Metrics = metricsData
	}

	if env.JobID == "" || env.RoundID == 0 || env.PropletID == "" {
		return flpkg.UpdateEnvelope{}, errors.New("missing required fields in update envelope")
	}

	return env, nil
}

func GetJobPodLogs(ctx context.Context, c client.Client, job *batchv1.Job) (string, error) {
	podList := &corev1.PodList{}
	labels := job.Spec.Selector.MatchLabels
	if err := c.List(ctx, podList, client.MatchingLabels(labels), client.InNamespace(job.Namespace)); err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			return pod.Name, nil
		}
	}

	return "", errors.New("no completed pod found for job")
}

func UpdateJobWithResult(ctx context.Context, c client.Client, job *batchv1.Job, result map[string]any) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if job.Annotations == nil {
		job.Annotations = make(map[string]string)
	}
	job.Annotations["propeller.absmach.io/result"] = string(resultJSON)

	return c.Update(ctx, job)
}

func GetPod(ctx context.Context, c client.Client, name, namespace string) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod); err != nil {
		return nil, err
	}

	return pod, nil
}
