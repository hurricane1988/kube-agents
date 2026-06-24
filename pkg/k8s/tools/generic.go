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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/codefuture-io/kube-agents/pkg/k8s"
)

// ResourceListReq is the input for listing generic resources.
type ResourceListReq struct {
	Namespace  string `json:"namespace" jsonschema:"description=namespace,omitempty"`
	Resource   string `json:"resource" jsonschema:"description=resource plural name (e.g. deployments, ingresses),required"`
	APIGroup   string `json:"api_group" jsonschema:"description=API group (e.g. apps, networking.k8s.io),omitempty"`
	APIVersion string `json:"api_version" jsonschema:"description=API version (e.g. v1),omitempty"`
	Limit      int64  `json:"limit" jsonschema:"description=max items (default 20),omitempty"`
}

// ResourceListRsp is the output.
type ResourceListRsp struct {
	Items []map[string]interface{} `json:"items"`
	Count int                      `json:"count"`
	Err   string                   `json:"error,omitempty"`
}

// NewResourceListTool creates a tool for listing any K8s resource.
func NewResourceListTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req ResourceListReq) (ResourceListRsp, error) {
			return resourceList(ctx, clients, req)
		},
		function.WithName("resource_list"),
		function.WithDescription("List any Kubernetes resource by resource plural name. Use for resources without dedicated tools."),
	)
}

func resourceList(ctx context.Context, c *k8s.Clients, req ResourceListReq) (ResourceListRsp, error) {
	ns := req.Namespace
	if ns == "" {
		ns = c.Namespace
	}

	gvr, err := resolveGVR(ctx, c, req.Resource, req.APIGroup, req.APIVersion)
	if err != nil {
		return ResourceListRsp{Err: err.Error()}, nil
	}

	opts := metav1.ListOptions{}
	if req.Limit > 0 {
		opts.Limit = req.Limit
	} else {
		opts.Limit = 20
	}

	var list *unstructured.UnstructuredList
	if ns != "" && gvr.Resource != "namespaces" && gvr.Resource != "nodes" {
		list, err = c.DynamicClient.Resource(gvr).Namespace(ns).List(ctx, opts)
	} else {
		list, err = c.DynamicClient.Resource(gvr).List(ctx, opts)
	}
	if err != nil {
		return ResourceListRsp{Err: err.Error()}, nil
	}

	items := make([]map[string]interface{}, 0, len(list.Items))
	for _, item := range list.Items {
		summary := map[string]interface{}{
			"name":      item.GetName(),
			"namespace": item.GetNamespace(),
		}
		if status, ok := item.Object["status"].(map[string]interface{}); ok {
			statusSummary := map[string]interface{}{}
			for _, key := range []string{"phase", "readyReplicas", "availableReplicas"} {
				if v, exists := status[key]; exists {
					statusSummary[key] = v
				}
			}
			if len(statusSummary) > 0 {
				summary["status"] = statusSummary
			}
		}
		items = append(items, summary)
	}
	return ResourceListRsp{Items: items, Count: len(items)}, nil
}

// ResourceGetReq is the input for getting a specific resource.
type ResourceGetReq struct {
	Name       string `json:"name" jsonschema:"description=resource name,required"`
	Namespace  string `json:"namespace" jsonschema:"description=namespace,omitempty"`
	Resource   string `json:"resource" jsonschema:"description=resource plural name,required"`
	APIGroup   string `json:"api_group" jsonschema:"description=API group,omitempty"`
	APIVersion string `json:"api_version" jsonschema:"description=API version,omitempty"`
}

// ResourceGetRsp is the output.
type ResourceGetRsp struct {
	Object map[string]interface{} `json:"object"`
	Err    string                 `json:"error,omitempty"`
}

// NewResourceGetTool creates a tool for getting a specific resource.
func NewResourceGetTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req ResourceGetReq) (ResourceGetRsp, error) {
			return resourceGet(ctx, clients, req)
		},
		function.WithName("resource_get"),
		function.WithDescription("Get a specific Kubernetes resource by name and resource type."),
	)
}

func resourceGet(ctx context.Context, c *k8s.Clients, req ResourceGetReq) (ResourceGetRsp, error) {
	ns := req.Namespace
	if ns == "" {
		ns = c.Namespace
	}

	gvr, err := resolveGVR(ctx, c, req.Resource, req.APIGroup, req.APIVersion)
	if err != nil {
		return ResourceGetRsp{Err: err.Error()}, nil
	}

	var obj *unstructured.Unstructured
	if ns != "" {
		obj, err = c.DynamicClient.Resource(gvr).Namespace(ns).Get(ctx, req.Name, metav1.GetOptions{})
	} else {
		obj, err = c.DynamicClient.Resource(gvr).Get(ctx, req.Name, metav1.GetOptions{})
	}
	if err != nil {
		return ResourceGetRsp{Err: err.Error()}, nil
	}
	return ResourceGetRsp{Object: obj.Object}, nil
}

// ClusterInfoReq is the input for cluster info.
type ClusterInfoReq struct{}

// ClusterInfoRsp contains basic cluster information.
type ClusterInfoRsp struct {
	Version    string   `json:"version"`
	APIGroups  []string `json:"api_groups"`
	Namespaces int      `json:"namespaces"`
	Nodes      int      `json:"nodes"`
	Err        string   `json:"error,omitempty"`
}

// NewClusterInfoTool creates a tool for getting cluster info.
func NewClusterInfoTool(clients *k8s.Clients) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req ClusterInfoReq) (ClusterInfoRsp, error) {
			return clusterInfo(ctx, clients)
		},
		function.WithName("cluster_info"),
		function.WithDescription("Get cluster version, available API groups, namespace and node count."),
	)
}

func clusterInfo(ctx context.Context, c *k8s.Clients) (ClusterInfoRsp, error) {
	version, err := c.ClientSet.Discovery().ServerVersion()
	if err != nil {
		return ClusterInfoRsp{Err: err.Error()}, nil
	}

	groups, _, err := c.ClientSet.Discovery().ServerGroupsAndResources()
	if err != nil {
		_ = err
	}

	apiGroups := make([]string, 0)
	if groups != nil {
		for _, g := range groups {
			for _, v := range g.Versions {
				apiGroups = append(apiGroups, fmt.Sprintf("%s/%s", g.Name, v.Version))
			}
		}
	}

	nsList, _ := c.ClientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	nodeList, _ := c.ClientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})

	nsCount, nodeCount := 0, 0
	if nsList != nil {
		nsCount = len(nsList.Items)
	}
	if nodeList != nil {
		nodeCount = len(nodeList.Items)
	}

	return ClusterInfoRsp{
		Version:    version.GitVersion,
		APIGroups:  apiGroups,
		Namespaces: nsCount,
		Nodes:      nodeCount,
	}, nil
}

// resolveGVR discovers the GroupVersionResource for a resource name.
func resolveGVR(ctx context.Context, c *k8s.Clients, resource, apiGroup, apiVersion string) (schema.GroupVersionResource, error) {
	_, apiLists, err := c.Discovery.ServerGroupsAndResources()
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to discover API resources: %w", err)
	}

	for _, list := range apiLists {
		// Check group filter if specified.
		group := ""
		if idx := strings.Index(list.GroupVersion, "/"); idx >= 0 {
			group = list.GroupVersion[:idx]
		}
		if apiGroup != "" && group != apiGroup {
			continue
		}

		for _, r := range list.APIResources {
			if r.Name == resource {
				version := apiVersion
				if version == "" {
					version = groupVersionToVersion(list.GroupVersion)
				}
				return schema.GroupVersionResource{
					Group:    group,
					Version:  version,
					Resource: r.Name,
				}, nil
			}
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("resource %q not found", resource)
}

// groupVersionToVersion extracts version from GroupVersion (e.g. "apps/v1" → "v1").
func groupVersionToVersion(gv string) string {
	if idx := strings.Index(gv, "/"); idx >= 0 {
		return gv[idx+1:]
	}
	return gv
}

func MustNewToolSet(clients *k8s.Clients) []tool.Tool {
	return []tool.Tool{
		NewPodListTool(clients),
		NewPodGetTool(clients),
		NewPodLogsTool(clients),
		NewPodDeleteTool(clients),
		NewDeploymentListTool(clients),
		NewDeploymentGetTool(clients),
		NewDeploymentScaleTool(clients),
		NewServiceListTool(clients),
		NewServiceGetTool(clients),
		NewNamespaceListTool(clients),
		NewNamespaceGetTool(clients),
		NewSetNamespaceTool(clients),
		NewEventListTool(clients),
		NewIngressListTool(clients),
		NewIngressGetTool(clients),
		NewHPAListTool(clients),
		NewHPAGetTool(clients),
		NewConfigMapListTool(clients),
		NewConfigMapGetTool(clients),
		NewSecretListTool(clients),
		NewSecretGetTool(clients),
		NewResourceListTool(clients),
		NewResourceGetTool(clients),
		NewClusterInfoTool(clients),
	}
}
