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

// NodeListReq is the input for listing nodes.
type NodeListReq struct {
	Label string `json:"label" jsonschema:"description=label selector,omitempty"`
}

// NodeListRsp is the output.
type NodeListRsp struct {
	Nodes []NodeSummary `json:"nodes"`
	Err   string        `json:"error,omitempty"`
}

// NodeSummary is a simplified node view.
type NodeSummary struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Roles   []string `json:"roles"`
	Version string   `json:"version"`
	CPU     string   `json:"cpu"`
	Memory  string   `json:"memory"`
	Age     string   `json:"age"`
}

// NewNodeListTool creates a tool for listing nodes.
func NewNodeListTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req NodeListReq) (NodeListRsp, error) {
			return nodeList(ctx, clients, req)
		},
		function.WithName("node_list"),
		function.WithDescription("List cluster nodes with status, roles, version, CPU and memory capacity."),
	)
}

func nodeList(ctx context.Context, c *k8s.Clients, req NodeListReq) (NodeListRsp, error) {
	opts := metav1.ListOptions{}
	if req.Label != "" {
		opts.LabelSelector = req.Label
	}
	list, err := c.ClientSet.CoreV1().Nodes().List(ctx, opts)
	if err != nil {
		return NodeListRsp{Err: err.Error()}, nil
	}
	summaries := make([]NodeSummary, 0, len(list.Items))
	for _, n := range list.Items {
		summaries = append(summaries, NodeSummary{
			Name:    n.Name,
			Status:  nodeStatus(n),
			Roles:   nodeRoles(n.Labels),
			Version: n.Status.NodeInfo.KubeletVersion,
			CPU:     n.Status.Capacity.Cpu().String(),
			Memory:  formatBytes(n.Status.Capacity.Memory().Value()),
			Age:     ageStr(n.CreationTimestamp.Time),
		})
	}
	return NodeListRsp{Nodes: summaries}, nil
}

// NodeGetReq is the input for describing a node.
type NodeGetReq struct {
	Name string `json:"name" jsonschema:"description=node name,required"`
}

// NodeGetRsp is the output.
type NodeGetRsp struct {
	Node *NodeDetail `json:"node"`
	Err  string      `json:"error,omitempty"`
}

// NodeDetail contains key node information.
type NodeDetail struct {
	Name              string            `json:"name"`
	Status            string            `json:"status"`
	Roles             []string          `json:"roles"`
	Version           string            `json:"version"`
	OS                string            `json:"os"`
	Arch              string            `json:"arch"`
	CPUCapacity       string            `json:"cpu_capacity"`
	MemoryCapacity    string            `json:"memory_capacity"`
	PodCapacity       int64             `json:"pod_capacity"`
	CPUAllocatable    string            `json:"cpu_allocatable"`
	MemoryAllocatable string            `json:"memory_allocatable"`
	InternalIP        string            `json:"internal_ip"`
	ExternalIP        string            `json:"external_ip,omitempty"`
	Conditions        []string          `json:"conditions"`
	Taints            []string          `json:"taints,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

// NewNodeGetTool creates a tool for describing a node.
func NewNodeGetTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req NodeGetReq) (NodeGetRsp, error) {
			return nodeGet(ctx, clients, req)
		},
		function.WithName("node_get"),
		function.WithDescription("Get detailed information about a node including capacity, allocatable, conditions, taints, and addresses."),
	)
}

func nodeGet(ctx context.Context, c *k8s.Clients, req NodeGetReq) (NodeGetRsp, error) {
	n, err := c.ClientSet.CoreV1().Nodes().Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return NodeGetRsp{Err: err.Error()}, nil
	}

	conditions := make([]string, 0, len(n.Status.Conditions))
	for _, cond := range n.Status.Conditions {
		status := "OK"
		if cond.Status != corev1.ConditionTrue {
			status = "False"
		}
		conditions = append(conditions, fmt.Sprintf("%s: %s (%s)",
			cond.Type, status, cond.Reason))
	}

	taints := make([]string, 0, len(n.Spec.Taints))
	for _, t := range n.Spec.Taints {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect))
	}

	var internalIP, externalIP string
	for _, addr := range n.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP:
			internalIP = addr.Address
		case corev1.NodeExternalIP:
			externalIP = addr.Address
		}
	}

	return NodeGetRsp{Node: &NodeDetail{
		Name:              n.Name,
		Status:            nodeStatus(*n),
		Roles:             nodeRoles(n.Labels),
		Version:           n.Status.NodeInfo.KubeletVersion,
		OS:                fmt.Sprintf("%s %s", n.Status.NodeInfo.OperatingSystem, n.Status.NodeInfo.OSImage),
		Arch:              n.Status.NodeInfo.Architecture,
		CPUCapacity:       n.Status.Capacity.Cpu().String(),
		MemoryCapacity:    formatBytes(n.Status.Capacity.Memory().Value()),
		PodCapacity:       n.Status.Capacity.Pods().Value(),
		CPUAllocatable:    n.Status.Allocatable.Cpu().String(),
		MemoryAllocatable: formatBytes(n.Status.Allocatable.Memory().Value()),
		InternalIP:        internalIP,
		ExternalIP:        externalIP,
		Conditions:        conditions,
		Taints:            taints,
		Labels:            n.Labels,
	}}, nil
}

func nodeStatus(n corev1.Node) string {
	for _, cond := range n.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			if cond.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return "NotReady"
		}
	}
	return "Unknown"
}

func nodeRoles(labels map[string]string) []string {
	var roles []string
	for k := range labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(k, "node-role.kubernetes.io/")
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		return []string{"worker"}
	}
	return roles
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
