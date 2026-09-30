/*
Copyright The Kubernetes Authors.

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

// Package capicluster provides utilities for working with Cluster API (CAPI)
// Cluster objects using unstructured types.
//
// This package intentionally avoids importing github.com/kubernetes-sigs/cluster-api
// to prevent a hard module dependency. This allows the controller to work with
// multiple CAPI versions and reduces version coupling. The trade-off is loss of
// compile-time type safety, which is acceptable because:
//
//  1. The CAPI Cluster schema is stable (v1beta1)
//  2. Controllers validate field presence before use
//  3. Type-checking is deferred to integration tests
package capicluster

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GVK is the GroupVersionKind for CAPI Cluster objects.
var GVK = schema.GroupVersionKind{
	Group:   "cluster.x-k8s.io",
	Version: "v1beta1",
	Kind:    "Cluster",
}

// ExtractPodCIDRs reads Pod CIDRs from a CAPI Cluster's
// spec.clusterNetwork.pods.cidrBlocks. Returns nil if the field is not
// present or empty.
func ExtractPodCIDRs(cluster *unstructured.Unstructured) []string {
	cidrBlocksRaw, found, err := unstructured.NestedStringSlice(
		cluster.Object, "spec", "clusterNetwork", "pods", "cidrBlocks",
	)
	if err != nil || !found || len(cidrBlocksRaw) == 0 {
		return nil
	}
	return cidrBlocksRaw
}

// SafeClusterFields returns a whitelist of loggable key-value pairs from
// an unstructured CAPI Cluster. This prevents accidental logging of specs
// or secrets as the controller evolves.
func SafeClusterFields(u *unstructured.Unstructured) []any {
	fields := []any{
		"name", u.GetName(),
		"namespace", u.GetNamespace(),
	}

	if uid := string(u.GetUID()); uid != "" {
		fields = append(fields, "uid", uid)
	}

	if ts := u.GetDeletionTimestamp(); ts != nil {
		fields = append(fields, "deletionTimestamp", ts.Time)
	}

	return fields
}

// IsDomainLabel reports whether k has the given domain suffix.
// It matches both unprefixed keys like "environment.<domain>" and
// prefixed keys like "role.<domain>/workload-cluster".
func IsDomainLabel(k, domainSuffix string) bool {
	if strings.HasSuffix(k, domainSuffix) {
		return true
	}
	if idx := strings.Index(k, "/"); idx > 0 {
		return strings.HasSuffix(k[:idx], domainSuffix)
	}
	return false
}

// ExtractDomainLabels returns labels whose key (or prefix before "/")
// ends with the given domain suffix. These are synced as ClusterProfile
// properties.
func ExtractDomainLabels(cluster *unstructured.Unstructured, domainSuffix string) map[string]string {
	result := map[string]string{}
	for k, v := range cluster.GetLabels() {
		if IsDomainLabel(k, domainSuffix) {
			result[k] = v
		}
	}
	return result
}
