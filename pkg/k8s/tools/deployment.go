/*
Copyright 2026 CodeFuture Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tools

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/codefuture-io/kube-agents/pkg/k8s"
)

// DeploymentListReq is the input for listing deployments.
type DeploymentListReq struct {
	Namespace string `json:"namespace" jsonschema:"description=namespace,omitempty"`
	Label     string `json:"label" jsonschema:"description=label selector,omitempty"`
}

// DeploymentListRsp is the output for the deployment list.
type DeploymentListRsp struct {
	Deployments []DeploySummary `json:"deployments"`
	Err         string          `json:"error,omitempty"`
}

// DeploySummary is a simplified deployment view.
type DeploySummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	UpToDate  int32  `json:"up_to_date"`
	Available int32  `json:"available"`
	Age       string `json:"age"`
}

// NewDeploymentListTool creates a tool for listing deployments.
func NewDeploymentListTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req DeploymentListReq) (DeploymentListRsp, error) {
			return deployList(ctx, clients, req)
		},
		function.WithName("deployment_list"),
		function.WithDescription("List deployments with ready/available status."),
	)
}

func deployList(ctx context.Context, c *k8s.Clients, req DeploymentListReq) (DeploymentListRsp, error) {
	ns := req.Namespace
	if ns == "" {
		ns = c.Namespace
	}
	opts := metav1.ListOptions{}
	if req.Label != "" {
		opts.LabelSelector = req.Label
	}
	list, err := c.ClientSet.AppsV1().Deployments(ns).List(ctx, opts)
	if err != nil {
		return DeploymentListRsp{Err: err.Error()}, nil
	}
	summaries := make([]DeploySummary, 0, len(list.Items))
	for _, d := range list.Items {
		summaries = append(summaries, DeploySummary{
			Name:      d.Name,
			Namespace: d.Namespace,
			Ready:     fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, d.Status.Replicas),
			UpToDate:  d.Status.UpdatedReplicas,
			Available: d.Status.AvailableReplicas,
			Age:       ageStr(d.CreationTimestamp.Time),
		})
	}
	return DeploymentListRsp{Deployments: summaries}, nil
}

// DeployGetReq is the input for describing a deployment.
type DeployGetReq struct {
	Name      string `json:"name" jsonschema:"description=deployment name,required"`
	Namespace string `json:"namespace" jsonschema:"description=namespace,omitempty"`
}

// DeployGetRsp is the output for a deployment detail.
type DeployGetRsp struct {
	Deployment *DeployDetail `json:"deployment"`
	Err        string        `json:"error,omitempty"`
}

// DeployDetail contains key deployment information.
type DeployDetail struct {
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Replicas      int32             `json:"replicas"`
	ReadyReplicas int32             `json:"ready_replicas"`
	Strategy      string            `json:"strategy"`
	Image         string            `json:"image"`
	Labels        map[string]string `json:"labels"`
	Conditions    []string          `json:"conditions"`
}

// NewDeploymentGetTool creates a tool for describing a deployment.
func NewDeploymentGetTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req DeployGetReq) (DeployGetRsp, error) {
			return deployGet(ctx, clients, req)
		},
		function.WithName("deployment_get"),
		function.WithDescription("Get details of a deployment including replicas, image, strategy, and conditions."),
	)
}

func deployGet(ctx context.Context, c *k8s.Clients, req DeployGetReq) (DeployGetRsp, error) {
	ns := req.Namespace
	if ns == "" {
		ns = c.Namespace
	}
	d, err := c.ClientSet.AppsV1().Deployments(ns).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return DeployGetRsp{Err: err.Error()}, nil
	}

	image := ""
	if len(d.Spec.Template.Spec.Containers) > 0 {
		image = d.Spec.Template.Spec.Containers[0].Image
	}

	conditions := make([]string, 0, len(d.Status.Conditions))
	for _, cond := range d.Status.Conditions {
		conditions = append(conditions, fmt.Sprintf("%s: %s (%s)",
			cond.Type, cond.Status, cond.Reason))
	}

	return DeployGetRsp{Deployment: &DeployDetail{
		Name:          d.Name,
		Namespace:     d.Namespace,
		Replicas:      *d.Spec.Replicas,
		ReadyReplicas: d.Status.ReadyReplicas,
		Strategy:      string(d.Spec.Strategy.Type),
		Image:         image,
		Labels:        d.Labels,
		Conditions:    conditions,
	}}, nil
}

// DeployScaleReq is the input for scaling a deployment.
type DeployScaleReq struct {
	Name      string `json:"name" jsonschema:"description=deployment name,required"`
	Namespace string `json:"namespace" jsonschema:"description=namespace,omitempty"`
	Replicas  int32  `json:"replicas" jsonschema:"description=target replicas,required"`
}

// DeployScaleRsp is the output after scaling.
type DeployScaleRsp struct {
	Message string `json:"message"`
	Err     string `json:"error,omitempty"`
}

// NewDeploymentScaleTool creates a tool for scaling a deployment.
func NewDeploymentScaleTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req DeployScaleReq) (DeployScaleRsp, error) {
			return deployScale(ctx, clients, req)
		},
		function.WithName("deployment_scale"),
		function.WithDescription("Scale a deployment to the specified number of replicas."),
	)
}

func deployScale(ctx context.Context, c *k8s.Clients, req DeployScaleReq) (DeployScaleRsp, error) {
	ns := req.Namespace
	if ns == "" {
		ns = c.Namespace
	}
	scale, err := c.ClientSet.AppsV1().Deployments(ns).GetScale(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return DeployScaleRsp{Err: err.Error()}, nil
	}
	scale.Spec.Replicas = req.Replicas
	_, err = c.ClientSet.AppsV1().Deployments(ns).UpdateScale(ctx, req.Name, scale, metav1.UpdateOptions{})
	if err != nil {
		return DeployScaleRsp{Err: err.Error()}, nil
	}
	return DeployScaleRsp{
		Message: fmt.Sprintf("deployment %s/%s scaled to %d replicas", ns, req.Name, req.Replicas),
	}, nil
}

// DeployUpdateReq is the input for updating deployment pod template spec.
type DeployUpdateReq struct {
	Name         string            `json:"name" jsonschema:"description=deployment name,required"`
	Namespace    string            `json:"namespace" jsonschema:"description=namespace,omitempty"`
	Replicas     *int32            `json:"replicas" jsonschema:"description=target number of replicas,omitempty"`
	Image        string            `json:"image" jsonschema:"description=new container image,omitempty"`
	NodeSelector map[string]string `json:"node_selector" jsonschema:"description=node labels for pod scheduling,omitempty"`
	Tolerations  []string          `json:"tolerations" jsonschema:"description=tolerations in key=value:Effect format,omitempty"`
}

// DeployUpdateRsp is the output.
type DeployUpdateRsp struct {
	Message string `json:"message"`
	Err     string `json:"error,omitempty"`
}

// NewDeploymentUpdateTool creates a tool for updating a deployment's pod template.
func NewDeploymentUpdateTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req DeployUpdateReq) (DeployUpdateRsp, error) {
			return deployUpdate(ctx, clients, req)
		},
		function.WithName("deployment_update"),
		function.WithDescription("Update deployment: adjust replicas, change container image, set nodeSelector, or add tolerations for scheduling control."),
	)
}

func deployUpdate(ctx context.Context, c *k8s.Clients, req DeployUpdateReq) (DeployUpdateRsp, error) {
	ns := req.Namespace
	if ns == "" {
		ns = c.Namespace
	}

	d, err := c.ClientSet.AppsV1().Deployments(ns).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return DeployUpdateRsp{Err: err.Error()}, nil
	}

	updated := false
	podSpec := &d.Spec.Template.Spec

	if req.Replicas != nil {
		d.Spec.Replicas = req.Replicas
		updated = true
	}

	if req.Image != "" && len(podSpec.Containers) > 0 {
		podSpec.Containers[0].Image = req.Image
		updated = true
	}

	if req.NodeSelector != nil {
		podSpec.NodeSelector = req.NodeSelector
		updated = true
	}

	for _, t := range req.Tolerations {
		tol := corev1.Toleration{Operator: corev1.TolerationOpExists}
		parts := splitTol(t)
		if len(parts) >= 1 {
			tol.Key = parts[0]
		}
		if len(parts) >= 2 && parts[1] != "" {
			tol.Value = parts[1]
			tol.Operator = corev1.TolerationOpEqual
		}
		if len(parts) >= 3 {
			tol.Effect = corev1.TaintEffect(parts[2])
		}
		podSpec.Tolerations = append(podSpec.Tolerations, tol)
		updated = true
	}

	if !updated {
		return DeployUpdateRsp{Message: "no changes specified"}, nil
	}

	_, err = c.ClientSet.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		return DeployUpdateRsp{Err: err.Error()}, nil
	}
	msg := fmt.Sprintf("deployment %s/%s updated", ns, req.Name)
	if req.Replicas != nil {
		msg = fmt.Sprintf("deployment %s/%s updated (replicas=%d)", ns, req.Name, *req.Replicas)
	}
	return DeployUpdateRsp{Message: msg}, nil
}

func splitTol(s string) []string {
	var parts []string
	rest := s
	if idx := strings.IndexByte(rest, '='); idx >= 0 {
		parts = append(parts, rest[:idx])
		rest = rest[idx+1:]
	} else if idx := strings.IndexByte(rest, ':'); idx >= 0 {
		parts = append(parts, rest[:idx])
		rest = rest[idx+1:]
	} else {
		return append(parts, rest)
	}
	if idx := strings.IndexByte(rest, ':'); idx >= 0 {
		parts = append(parts, rest[:idx])
		parts = append(parts, rest[idx+1:])
	} else {
		parts = append(parts, rest)
	}
	return parts
}
