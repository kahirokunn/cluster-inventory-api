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

package clusterprofiletolabels

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

const (
	// defaultPropsToLabelsFieldManager is the SSA field manager for the properties-to-labels controller.
	defaultPropsToLabelsFieldManager = "clusterprofile-properties-to-labels"

	// ConditionPropertiesLabelsSynced is the condition type for properties-to-labels sync status.
	ConditionPropertiesLabelsSynced = "PropertiesLabelsSynced"

	// ReasonAllSynced indicates all properties were synced to labels.
	ReasonAllSynced = "AllSynced"
	// ReasonPartialSync indicates some properties failed to sync.
	ReasonPartialSync = "PartialSync"
	// ReasonNoProperties indicates no properties were found to sync.
	ReasonNoProperties = "NoProperties"
)

//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=clusterprofiles,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=clusterprofiles/status,verbs=get;update;patch

// PropertiesToLabelsReconciler watches ClusterProfile objects and syncs
// status.properties entries to metadata.labels, skipping entries that violate
// Kubernetes label constraints.
type PropertiesToLabelsReconciler struct {
	Client       client.Client
	Log          logr.Logger
	FieldManager string // default: "clusterprofile-properties-to-labels"
}

func (r *PropertiesToLabelsReconciler) fieldManager() string {
	if r.FieldManager != "" {
		return r.FieldManager
	}
	return defaultPropsToLabelsFieldManager
}

// SetupWithManager registers the reconciler to watch ClusterProfile objects,
// filtering updates to only trigger when status.properties changes.
func (r *PropertiesToLabelsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterProfile{}, builder.WithPredicates(
			statusPropertiesChangedPredicate{},
		)).
		Named("clusterprofile-properties-to-labels").
		Complete(r)
}

// Reconcile reads status.properties from a ClusterProfile, validates each
// entry as a label key/value, and SSA-patches the valid entries onto
// metadata.labels. It also sets a PropertiesLabelsSynced condition.
func (r *PropertiesToLabelsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("clusterprofile", req.NamespacedName)

	// 1. Get the ClusterProfile (typed read)
	cp := &v1alpha1.ClusterProfile{}
	if err := r.Client.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("ClusterProfile not found; possibly deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get ClusterProfile %s: %w", req.NamespacedName, err)
	}

	// 2. Read status.properties from typed object
	properties := extractPropertiesFromTyped(cp)

	// 3. Validate each property as a label key/value
	validLabels := make(map[string]string)
	var skipped []skippedProperty

	for _, prop := range properties {
		// 3a. Validate label key
		if errs := validation.IsQualifiedName(prop.Name); len(errs) > 0 {
			skipped = append(skipped, skippedProperty{
				Name:   prop.Name,
				Reason: "InvalidLabelKey",
				Detail: strings.Join(errs, "; "),
			})
			continue
		}

		// 3b. Validate label value
		if errs := validation.IsValidLabelValue(prop.Value); len(errs) > 0 {
			skipped = append(skipped, skippedProperty{
				Name:   prop.Name,
				Reason: "InvalidLabelValue",
				Detail: strings.Join(errs, "; "),
			})
			continue
		}

		validLabels[prop.Name] = prop.Value
	}

	// 4. SSA patch metadata.labels
	if err := r.applyMetadataLabels(ctx, req.NamespacedName, validLabels); err != nil {
		return ctrl.Result{}, fmt.Errorf("SSA patch labels for %s: %w", req.NamespacedName, err)
	}

	// 5. SSA patch status.conditions (PropertiesLabelsSynced)
	if err := r.applyStatusCondition(ctx, req.NamespacedName, properties, skipped); err != nil {
		return ctrl.Result{}, fmt.Errorf("SSA patch status for %s: %w", req.NamespacedName, err)
	}

	log.Info("synced properties to labels",
		"total", len(properties), "synced", len(validLabels), "skipped", len(skipped))

	return ctrl.Result{}, nil
}

// applyMetadataLabels performs an SSA patch to set metadata.labels on the ClusterProfile.
func (r *PropertiesToLabelsReconciler) applyMetadataLabels(
	ctx context.Context,
	key types.NamespacedName,
	labelMap map[string]string,
) error {
	obj := buildMetadataLabelsApplyObject(key, labelMap)

	return r.Client.Patch(ctx, obj, client.Apply, //nolint:staticcheck // SA1019: migration deferred
		client.FieldOwner(r.fieldManager()),
		client.ForceOwnership,
	)
}

// applyStatusCondition performs an SSA patch to set the PropertiesLabelsSynced condition.
func (r *PropertiesToLabelsReconciler) applyStatusCondition(
	ctx context.Context,
	key types.NamespacedName,
	allProperties []propertyEntry,
	skipped []skippedProperty,
) error {
	cond := buildPropertiesLabelsSyncedCondition(allProperties, skipped)

	obj, err := buildStatusConditionApplyObject(key, cond)
	if err != nil {
		return err
	}

	force := true
	return r.Client.Status().
		Patch(ctx, obj, client.Apply, //nolint:staticcheck // SA1019: migration deferred
			&client.SubResourcePatchOptions{
				PatchOptions: client.PatchOptions{
					FieldManager: r.fieldManager(),
					Force:        &force,
				},
			})
}

func buildMetadataLabelsApplyObject(key types.NamespacedName, labelMap map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(v1alpha1.ClusterProfileSchemeGroupVersionKind)
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)
	obj.SetLabels(labelMap)
	return obj
}

func buildStatusConditionApplyObject(
	key types.NamespacedName, cond metav1.Condition,
) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(v1alpha1.ClusterProfileSchemeGroupVersionKind)
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)

	condMap := map[string]any{
		"type":               cond.Type,
		"status":             string(cond.Status),
		"reason":             cond.Reason,
		"message":            cond.Message,
		"lastTransitionTime": cond.LastTransitionTime.UTC().Format(time.RFC3339),
	}

	if err := unstructured.SetNestedSlice(obj.Object, []any{condMap}, "status", "conditions"); err != nil {
		return nil, fmt.Errorf("set nested conditions: %w", err)
	}

	return obj, nil
}

// propertyEntry represents a single status.properties entry.
type propertyEntry struct {
	Name  string
	Value string
}

// skippedProperty records why a property was not synced to a label.
type skippedProperty struct {
	Name   string
	Reason string
	Detail string
}

// extractPropertiesFromTyped reads status.properties from a typed ClusterProfile.
func extractPropertiesFromTyped(cp *v1alpha1.ClusterProfile) []propertyEntry {
	var result []propertyEntry
	for _, p := range cp.Status.Properties {
		if p.Name == "" {
			continue
		}
		result = append(result, propertyEntry{Name: p.Name, Value: p.Value})
	}
	return result
}

// buildPropertiesLabelsSyncedCondition builds the PropertiesLabelsSynced condition.
func buildPropertiesLabelsSyncedCondition(allProperties []propertyEntry, skipped []skippedProperty) metav1.Condition {
	now := metav1.Now()

	if len(allProperties) == 0 {
		return metav1.Condition{
			Type:               ConditionPropertiesLabelsSynced,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonNoProperties,
			Message:            "No properties to sync",
			LastTransitionTime: now,
		}
	}

	if len(skipped) == 0 {
		return metav1.Condition{
			Type:               ConditionPropertiesLabelsSynced,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonAllSynced,
			Message:            fmt.Sprintf("All %d properties synced to labels", len(allProperties)),
			LastTransitionTime: now,
		}
	}

	synced := len(allProperties) - len(skipped)
	var skippedDetails []string
	for _, s := range skipped {
		skippedDetails = append(skippedDetails, fmt.Sprintf("%s (%s: %s)", s.Name, s.Reason, s.Detail))
	}

	msg := fmt.Sprintf("%d of %d properties synced to labels; skipped: %s",
		synced, len(allProperties), strings.Join(skippedDetails, ", "))

	// Truncate if needed (cap at ~30000 chars to stay well within 32768 limit)
	const maxLen = 30000
	if len(msg) > maxLen {
		remaining := len(skipped) - 1
		for remaining > 0 && len(msg) > maxLen {
			skippedDetails = skippedDetails[:len(skippedDetails)-1]
			remaining--
			msg = fmt.Sprintf("%d of %d properties synced to labels; skipped: %s ...and %d more",
				synced, len(allProperties), strings.Join(skippedDetails, ", "), len(skipped)-len(skippedDetails))
		}
	}

	return metav1.Condition{
		Type:               ConditionPropertiesLabelsSynced,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonPartialSync,
		Message:            msg,
		LastTransitionTime: now,
	}
}

// statusPropertiesChangedPredicate filters events so that the reconciler only
// triggers when status.properties actually changes, preventing infinite loops
// from label write-back.
type statusPropertiesChangedPredicate struct{}

var _ predicate.Predicate = statusPropertiesChangedPredicate{}

func (p statusPropertiesChangedPredicate) Create(_ event.CreateEvent) bool   { return true }
func (p statusPropertiesChangedPredicate) Delete(_ event.DeleteEvent) bool   { return false }
func (p statusPropertiesChangedPredicate) Generic(_ event.GenericEvent) bool { return false }

func (p statusPropertiesChangedPredicate) Update(e event.UpdateEvent) bool {
	oldHash := extractPropertiesHash(e.ObjectOld)
	newHash := extractPropertiesHash(e.ObjectNew)
	return oldHash != newHash
}

// extractPropertiesHash computes a stable hash of status.properties for change detection.
func extractPropertiesHash(obj client.Object) string {
	// Try typed ClusterProfile first
	if cp, ok := obj.(*v1alpha1.ClusterProfile); ok {
		if len(cp.Status.Properties) == 0 {
			return ""
		}
		type prop struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		props := make([]prop, len(cp.Status.Properties))
		for i, p := range cp.Status.Properties {
			props[i] = prop{Name: p.Name, Value: p.Value}
		}
		sort.Slice(props, func(i, j int) bool { return props[i].Name < props[j].Name })

		data, err := json.Marshal(props)
		if err != nil {
			return ""
		}
		h := sha256.Sum256(data)
		return hex.EncodeToString(h[:8])
	}

	// Fallback to unstructured
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return ""
	}

	raw, found, err := unstructured.NestedSlice(u.Object, "status", "properties")
	if err != nil || !found || len(raw) == 0 {
		return ""
	}

	type prop struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	var props []prop
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		props = append(props, prop{Name: name, Value: value})
	}
	sort.Slice(props, func(i, j int) bool { return props[i].Name < props[j].Name })

	data, err := json.Marshal(props)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}
