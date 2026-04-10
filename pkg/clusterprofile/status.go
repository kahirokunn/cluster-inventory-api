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

// Package clusterprofile provides a reusable Go SDK for reading and writing
// ClusterProfile (multicluster.x-k8s.io/v1alpha1) resources using
// Server-Side Apply (SSA).
//
// The key invariant is multi-writer safety: each controller writes only the
// status fields it owns. SSA with distinct field managers ensures that
// concurrent controllers (e.g. one writing properties, another writing
// accessProviders) do not conflict. The CRD's listType=map on properties
// and accessProviders (keyed by "name") guarantees that two writers can
// safely maintain disjoint entries in the same list.
package clusterprofile

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// DefaultFieldManager is the SSA field manager used when StatusPatchOptions
// does not specify one.
const DefaultFieldManager = "capi-cluster-to-clusterprofile-controller"

// ErrNotImplemented is returned by stub methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// StatusPatchOptions configures the behavior of an SSA status patch.
type StatusPatchOptions struct {
	// FieldManager identifies the actor applying the patch.
	// When empty, DefaultFieldManager is used.
	FieldManager string

	// Force, when true, forces the SSA patch to steal ownership of fields
	// owned by other managers. This should almost never be set to true.
	Force bool
}

func (o StatusPatchOptions) fieldManager() string {
	if o.FieldManager != "" {
		return o.FieldManager
	}
	return DefaultFieldManager
}

// StatusApplier writes the status subresource of a ClusterProfile using SSA.
type StatusApplier interface {
	// ApplyStatus performs a Server-Side Apply patch to the status subresource
	// of the named ClusterProfile. desiredStatus is a map representing the
	// status fields this writer owns (e.g. {"properties": [...]}).
	ApplyStatus(ctx context.Context, key types.NamespacedName, desiredStatus map[string]any, opts StatusPatchOptions) error
}

// ---------- real implementation ----------

// RuntimeClient implements StatusApplier using a controller-runtime client.Client.
type RuntimeClient struct {
	Inner client.Client
}

var _ StatusApplier = (*RuntimeClient)(nil)

// ApplyStatus performs a Server-Side Apply patch to the status subresource.
func (c *RuntimeClient) ApplyStatus(
	ctx context.Context,
	key types.NamespacedName,
	desiredStatus map[string]any,
	opts StatusPatchOptions,
) error {
	patch := BuildStatusPatchPayload(key, desiredStatus)

	force := opts.Force
	err := c.Inner.Status().
		Patch(ctx, patch, client.Apply, //nolint:staticcheck // SA1019: migration deferred
			&client.SubResourcePatchOptions{
				PatchOptions: client.PatchOptions{
					FieldManager: opts.fieldManager(),
					Force:        &force,
				},
			})
	if err != nil {
		return ClassifyError(err, "SSA status patch ClusterProfile %s", key)
	}
	return nil
}

// BuildStatusPatchPayload builds the unstructured SSA payload for a
// ClusterProfile status patch. It is exported so that unit tests can
// verify the payload shape without an apiserver.
func BuildStatusPatchPayload(key types.NamespacedName, desiredStatus map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(v1alpha1.ClusterProfileSchemeGroupVersionKind)
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)
	// ManagedFields must remain nil in apply payloads; the apiserver rejects
	// non-nil values, even an empty slice. We set only the fields we own under
	// status.
	if err := unstructured.SetNestedField(obj.Object, toAnyMap(desiredStatus), "status"); err != nil {
		// SetNestedField only errors on invalid path; "status" is always valid.
		panic(fmt.Sprintf("unreachable: SetNestedField(status): %v", err))
	}

	return obj
}

// ---------- stub implementation ----------

// StubClient is a placeholder implementation that returns ErrNotImplemented
// for all operations. It exists so that controllers can be wired now and
// real implementations can be swapped in later.
type StubClient struct{}

var _ StatusApplier = (*StubClient)(nil)

// ApplyStatus returns ErrNotImplemented.
func (s *StubClient) ApplyStatus(
	_ context.Context, _ types.NamespacedName, _ map[string]any, _ StatusPatchOptions,
) error {
	return ErrNotImplemented
}

// ---------- helpers ----------

// toAnyMap deep-copies a map[string]any so that SetNestedField receives
// the expected types.
func toAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}
