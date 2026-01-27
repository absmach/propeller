package controllers

import (
	"context"
	"fmt"

	propellerv1alpha1 "github.com/absmach/propeller/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Scheduler struct {
	client client.Client
}

func NewScheduler(c client.Client) *Scheduler {
	return &Scheduler{client: c}
}

func (s *Scheduler) SelectProplet(ctx context.Context, task *propellerv1alpha1.WasmTask) (string, error) {
	if task.Spec.PropletID != "" {
		return task.Spec.PropletID, nil
	}

	if task.Spec.PropletGroupRef != nil {
		group := &propellerv1alpha1.PropletGroup{}
		if err := s.client.Get(ctx, client.ObjectKey{
			Name:      task.Spec.PropletGroupRef.Name,
			Namespace: task.Namespace,
		}, group); err != nil {
			return "", fmt.Errorf("failed to get proplet group: %w", err)
		}

		groupReconciler := &PropletGroupReconciler{Client: s.client}

		return groupReconciler.SelectPropletFromGroup(ctx, group)
	}

	return defaultPropletID, nil
}
