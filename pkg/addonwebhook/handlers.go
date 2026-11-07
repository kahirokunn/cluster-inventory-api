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

package addonwebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	api "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// Handlers uses an uncached API reader. In particular, a stale informer cache
// must not make an in-use Class appear unreferenced during deletion.
type Handlers struct {
	Reader client.Reader
}

// ValidateAddonUpdate prevents a Class rebase from handing an Addon to another manager.
func (h Handlers) ValidateAddonUpdate(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Update || req.SubResource != "" {
		return admission.Allowed("")
	}
	var oldAddon, newAddon api.Addon
	if err := json.Unmarshal(req.OldObject.Raw, &oldAddon); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode old Addon: %w", err))
	}
	if err := json.Unmarshal(req.Object.Raw, &newAddon); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode new Addon: %w", err))
	}
	oldKey := classKey(oldAddon.Namespace, oldAddon.Spec.ClassRef)
	newKey := classKey(newAddon.Namespace, newAddon.Spec.ClassRef)
	if oldKey == newKey {
		return admission.Allowed("")
	}
	if len(oldAddon.Finalizers) > 0 && oldAddon.Status.ClassRef == nil {
		return admission.Denied("cannot change classRef: Addon has a finalizer but no recorded Class and manager")
	}
	var oldClass, newClass api.AddonClass
	if err := h.Reader.Get(ctx, oldKey, &oldClass); err != nil {
		return classReadError("old", oldKey, err)
	}
	if err := h.Reader.Get(ctx, newKey, &newClass); err != nil {
		return classReadError("new", newKey, err)
	}
	if oldClass.Spec.ControllerName != newClass.Spec.ControllerName {
		return admission.Denied("cannot change classRef to a Class managed by a different controllerName")
	}
	if recorded := oldAddon.Status.ClassRef; recorded != nil && recorded.ControllerName != oldClass.Spec.ControllerName {
		return admission.Denied("old Class controllerName conflicts with the manager recorded in status.classRef")
	}
	return admission.Allowed("")
}

// ValidateAddonClassDelete protects a Class while an Addon remains bound to its manager.
func (h Handlers) ValidateAddonClassDelete(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Delete {
		return admission.Allowed("")
	}
	target := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	if target.Name == "" || target.Namespace == "" {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("AddonClass namespace and name are required"))
	}
	var class api.AddonClass
	if err := json.Unmarshal(req.OldObject.Raw, &class); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode deleting AddonClass: %w", err))
	}
	if class.Name != target.Name || class.Namespace != target.Namespace || class.Spec.ControllerName == "" {
		return admission.Errored(http.StatusBadRequest,
			fmt.Errorf("deleting AddonClass identity or controllerName is invalid"))
	}
	continueToken := ""
	for {
		var addons api.AddonList
		if err := h.Reader.List(ctx, &addons, &client.ListOptions{Limit: 500, Continue: continueToken}); err != nil {
			return admission.Errored(http.StatusInternalServerError,
				fmt.Errorf("list Addons to validate deletion of AddonClass %s: %w", target, err))
		}
		for i := range addons.Items {
			addon := &addons.Items[i]
			statusRef := addon.Status.ClassRef
			if statusRef != nil && statusRef.Namespace == target.Namespace && statusRef.Name == target.Name &&
				statusRef.ControllerName == class.Spec.ControllerName {
				return admission.Denied(fmt.Sprintf(
					"AddonClass %s is still bound to Addon %s/%s", target, addon.Namespace, addon.Name))
			}
		}
		continueToken = addons.Continue
		if continueToken == "" {
			return admission.Allowed("")
		}
	}
}

func classKey(addonNamespace string, ref api.AddonClassReference) types.NamespacedName {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = addonNamespace
	}
	return types.NamespacedName{Namespace: namespace, Name: ref.Name}
}

func classReadError(position string, key types.NamespacedName, err error) admission.Response {
	if apierrors.IsNotFound(err) {
		return admission.Denied(fmt.Sprintf("cannot change classRef: %s AddonClass %s does not exist", position, key))
	}
	return admission.Errored(http.StatusInternalServerError, fmt.Errorf("read %s AddonClass %s: %w", position, key, err))
}
