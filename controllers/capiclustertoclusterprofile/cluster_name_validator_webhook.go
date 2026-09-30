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

package capiclustertoclusterprofile

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/cluster-inventory-api/pkg/capicluster"
)

// ClusterNameValidator validates that CAPI Cluster names are unique across all
// namespaces (cluster-wide uniqueness).
//
// Because clusters are referenced by name only in many tools, allowing two
// CAPI Clusters with the same metadata.name in different namespaces would
// cause ambiguity. This webhook rejects CREATE and UPDATE requests when a
// Cluster with the same name already exists in any other namespace.
type ClusterNameValidator struct {
	Client  client.Client
	Log     logr.Logger
	decoder admission.Decoder
}

// NewClusterNameValidator constructs a validator with the given client and a
// decoder derived from the provided scheme.
func NewClusterNameValidator(c client.Client, scheme *runtime.Scheme) *ClusterNameValidator {
	return &ClusterNameValidator{
		Client:  c,
		Log:     logr.Discard(),
		decoder: admission.NewDecoder(scheme),
	}
}

// NewClusterNameValidatorWithLogger constructs a validator with the given
// client, scheme, and logger.
func NewClusterNameValidatorWithLogger(c client.Client, scheme *runtime.Scheme, log logr.Logger) *ClusterNameValidator {
	return &ClusterNameValidator{
		Client:  c,
		Log:     log,
		decoder: admission.NewDecoder(scheme),
	}
}

// Handle implements admission.Handler.
func (v *ClusterNameValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(capicluster.GVK)
	if err := v.decoder.Decode(req, cluster); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	name := cluster.GetName()
	namespace := cluster.GetNamespace()

	// List all existing CAPI Clusters across all namespaces.
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(capicluster.GVK)
	if err := v.Client.List(ctx, list); err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("failed to list Cluster resources: %w", err))
	}

	for i := range list.Items {
		existing := &list.Items[i]
		if existing.GetName() == name && existing.GetNamespace() != namespace {
			v.Log.Info("denied cluster creation", "name", name, "existingNamespace", existing.GetNamespace())
			return admission.Denied(fmt.Sprintf(
				"error: Cluster %q already exists in namespace %q (cluster-wide name must be unique)",
				name, existing.GetNamespace()))
		}
	}

	v.Log.V(4).Info("allowed cluster operation", "name", name, "namespace", namespace, "operation", req.Operation)
	return admission.Allowed("")
}
