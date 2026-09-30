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
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/cluster-inventory-api/pkg/capicluster"
	cpSDK "sigs.k8s.io/cluster-inventory-api/pkg/clusterprofile"
	"sigs.k8s.io/cluster-inventory-api/pkg/wellknown"
)

//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=clusterprofiles,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=clusterprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconciler watches CAPI Cluster objects and syncs them to Cluster Inventory
// API ClusterProfile resources.
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger

	// StatusApplier performs SSA patches to ClusterProfile status.
	// Must be set; the reconciler will panic if nil when a Cluster is found.
	StatusApplier cpSDK.StatusApplier

	// FieldManager identifies this controller as an SSA field manager.
	// Defaults to cpSDK.DefaultFieldManager if empty.
	FieldManager string

	// Recorder emits Kubernetes events for significant state transitions.
	// When nil, event recording is silently skipped.
	Recorder record.EventRecorder

	// ClusterManagerName identifies this cluster manager instance.
	// Defaults to "capi" if empty.
	ClusterManagerName string

	// DomainSuffix is the label domain suffix for extracting domain labels
	// from CAPI Clusters. Defaults to wellknown.LabelDomainSuffix.
	DomainSuffix string

	// RequeueAfterDuration is the duration to wait before requeuing when
	// access providers are not ready. Defaults to 15s if zero.
	RequeueAfterDuration time.Duration

	// MaxConcurrentReconciles is the maximum number of concurrent reconciles.
	// Defaults to 3 if zero.
	MaxConcurrentReconciles int
}

func (r *Reconciler) clusterManagerName() string {
	if r.ClusterManagerName != "" {
		return r.ClusterManagerName
	}
	return "capi"
}

func (r *Reconciler) domainSuffix() string {
	if r.DomainSuffix != "" {
		return r.DomainSuffix
	}
	return wellknown.LabelDomainSuffix
}

func (r *Reconciler) requeueAfterDuration() time.Duration {
	if r.RequeueAfterDuration > 0 {
		return r.RequeueAfterDuration
	}
	return 15 * time.Second
}

func (r *Reconciler) maxConcurrentReconciles() int {
	if r.MaxConcurrentReconciles > 0 {
		return r.MaxConcurrentReconciles
	}
	return 3
}

// Condition type and reason constants for AccessProvidersReady and ControlPlaneHealthy.
const (
	conditionAccessProvidersReady = "AccessProvidersReady"

	accessProvidersReasonPopulated            = "Populated"
	accessProvidersReasonEndpointNotAvailable = "EndpointNotAvailable"
	accessProvidersReasonSecretReadFailed     = "SecretReadFailed"

	controlPlaneReasonHealthy  = "ControlPlaneReachable"
	controlPlaneReasonNotReady = "ControlPlaneNotReady"
)

// Event reason constants.
const (
	eventAccessProviderSyncFailed = "AccessProviderSyncFailed"
	eventAccessProviderPopulated  = "AccessProviderPopulated"
)

// accessProviderResult captures the outcome of resolveAccessProviders.
type accessProviderResult struct {
	// Payload is the SSA payload for accessProviders (nil when skipped).
	Payload map[string]any
	// Reason is the condition reason.
	Reason string
	// Message is the human-readable condition message.
	Message string
	// ServerURL is the resolved API server URL (set only on success).
	ServerURL string
}

// Reconcile fetches the CAPI Cluster and ensures the corresponding
// ClusterProfile exists with up-to-date status. The ClusterProfile is created
// eagerly on first reconciliation; the ControlPlaneHealthy and
// AccessProvidersReady conditions remain False until the kubeconfig Secret
// becomes available.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (retResult ctrl.Result, retErr error) {
	start := time.Now()
	defer func() {
		result := "success"
		if retErr != nil {
			result = "error"
		} else if retResult.RequeueAfter > 0 {
			result = "requeue"
		}
		reconciliationDuration.WithLabelValues(result).Observe(time.Since(start).Seconds())
	}()

	log := r.Log.WithValues("cluster", req.NamespacedName)

	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(capicluster.GVK)

	if err := r.Client.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CAPI Cluster not found; possibly deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get CAPI Cluster %s: %w", req.NamespacedName, err)
	}

	// Skip deleted clusters: their kubeconfig Secret will never appear, so
	// the accessProviders fallback requeue would loop every 15 s forever,
	// flooding the work queue and starving live clusters of reconciliation.
	if cluster.GetDeletionTimestamp() != nil {
		log.Info("CAPI Cluster is being deleted; skipping reconciliation")
		return ctrl.Result{}, nil
	}

	fields := capicluster.SafeClusterFields(cluster)
	log.Info("reconciling CAPI Cluster", fields...)

	cpKey, apResult, err := r.ensureClusterProfile(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure ClusterProfile for %s: %w", req.NamespacedName, err)
	}

	// Build the merged SSA payload (properties + accessProviders + conditions).
	statusPayload := r.buildStatusPayload(cluster, apResult)

	opts := cpSDK.StatusPatchOptions{
		FieldManager: r.fieldManager(),
	}
	if err := r.StatusApplier.ApplyStatus(ctx, cpKey, statusPayload, opts); err != nil {
		var conflictErr *cpSDK.ConflictError
		if errors.As(err, &conflictErr) {
			log.Info("SSA conflict on ClusterProfile status; will requeue", "clusterprofile", cpKey)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("SSA status patch for %s: %w", cpKey, err)
	}
	log.Info("applied ClusterProfile status", "clusterprofile", cpKey,
		"properties", len(statusPayload)-2, // subtract conditions + accessProviders keys
		"accessProvidersReady", apResult.Reason == accessProvidersReasonPopulated)

	r.emitAccessProviderEvents(cluster, apResult)

	// Requeue as a safety fallback when accessProviders are not yet ready.
	// The primary fast path is the kubeconfig Secret watch registered in
	// SetupWithManager; this shorter interval is a backstop in case the
	// watch misses the Secret creation event.
	if apResult.Reason != accessProvidersReasonPopulated {
		return ctrl.Result{RequeueAfter: r.requeueAfterDuration()}, nil
	}

	return ctrl.Result{}, nil
}

// ensureClusterProfile resolves accessProviders from the kubeconfig Secret and
// finds or creates the ClusterProfile. Returns zero key when creation is deferred.
func (r *Reconciler) ensureClusterProfile(
	ctx context.Context,
	cluster *unstructured.Unstructured,
) (types.NamespacedName, accessProviderResult, error) {
	apResult := r.resolveAccessProviders(ctx, cluster)
	cpKey, err := r.findOrCreateClusterProfile(ctx, cluster)
	return cpKey, apResult, err
}

// findOrCreateClusterProfile looks up an existing ClusterProfile or creates a
// new one. Creation is always attempted when no existing CP is found.
func (r *Reconciler) findOrCreateClusterProfile(
	ctx context.Context,
	cluster *unstructured.Unstructured,
) (types.NamespacedName, error) {
	clusterKey := types.NamespacedName{Name: cluster.GetName(), Namespace: cluster.GetNamespace()}
	desiredKey := r.desiredClusterProfileKey(cluster)

	// Try direct lookup by desired key.
	current := &v1alpha1.ClusterProfile{}
	if err := r.Client.Get(ctx, desiredKey, current); err == nil {
		changed := r.ensureClusterProfileShape(current, cluster)
		if changed {
			if err := r.Client.Update(ctx, current); err != nil {
				return types.NamespacedName{}, fmt.Errorf("update ClusterProfile %s: %w", desiredKey, err)
			}
		}
		return desiredKey, nil
	} else if !apierrors.IsNotFound(err) {
		return types.NamespacedName{}, fmt.Errorf("get ClusterProfile %s: %w", desiredKey, err)
	}

	// Fallback: search by label.
	list, err := r.listClusterProfilesForCluster(ctx, clusterKey)
	if err != nil {
		return types.NamespacedName{}, err
	}
	switch len(list.Items) {
	case 0:
		desired := r.buildClusterProfile(cluster, desiredKey)
		if err := r.Client.Create(ctx, desired); err != nil {
			return types.NamespacedName{}, fmt.Errorf("create ClusterProfile %s: %w", desiredKey, err)
		}
		return desiredKey, nil
	case 1:
		cp := list.Items[0]
		return types.NamespacedName{Namespace: cp.Namespace, Name: cp.Name}, nil
	default:
		return types.NamespacedName{}, fmt.Errorf("multiple ClusterProfiles (%d) found for CAPI Cluster %s", len(list.Items), clusterKey)
	}
}

// buildStatusPayload computes the merged SSA status payload from properties,
// accessProviders, and conditions.
func (r *Reconciler) buildStatusPayload(cluster *unstructured.Unstructured, apResult accessProviderResult) map[string]any {
	owned := cpSDK.OwnedProperties{}

	podCIDRs := capicluster.ExtractPodCIDRs(cluster)
	if len(podCIDRs) > 0 {
		owned[wellknown.PodCIDRProperty] = podCIDRs[0]
	}
	if cs, ok := cluster.GetLabels()["cluster.clusterset.k8s.io"]; ok && cs != "" {
		owned["cluster.clusterset.k8s.io"] = cs
	}
	maps.Copy(owned, capicluster.ExtractDomainLabels(cluster, r.domainSuffix()))

	statusPayload := map[string]any{}
	maps.Copy(statusPayload, cpSDK.BuildApplyPayload(owned))
	if apResult.Payload != nil {
		maps.Copy(statusPayload, apResult.Payload)
	}

	// ControlPlaneHealthy is required by multicluster-runtime's default
	// IsReady check (cluster-inventory-api provider).
	statusPayload["conditions"] = []any{
		buildAccessProvidersCondition(apResult, cluster.GetGeneration()),
		buildControlPlaneHealthyCondition(apResult, cluster.GetGeneration()),
	}
	return statusPayload
}

func (r *Reconciler) fieldManager() string {
	if r.FieldManager != "" {
		return r.FieldManager
	}
	return cpSDK.DefaultFieldManager
}

// SetupWithManager registers the reconciler to watch CAPI Cluster objects.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	watched := &unstructured.Unstructured{}
	watched.SetGroupVersionKind(capicluster.GVK)

	cpProto := &v1alpha1.ClusterProfile{}

	return ctrl.NewControllerManagedBy(mgr).
		For(watched, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))).
		Watches(cpProto, handler.EnqueueRequestsFromMapFunc(r.mapClusterProfileToCluster), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.mapSecretToCluster), builder.WithPredicates(kubeconfigSecretPredicate{})).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.maxConcurrentReconciles()}).
		Named("capi-cluster-to-clusterprofile-controller").
		Complete(r)
}

func (r *Reconciler) mapClusterProfileToCluster(_ context.Context, obj client.Object) []reconcile.Request {
	cpLabels := obj.GetLabels()
	clusterName := strings.TrimSpace(cpLabels[wellknown.LabelSourceClusterName])
	clusterNamespace := strings.TrimSpace(cpLabels[wellknown.LabelSourceClusterNamespace])
	if clusterName == "" || clusterNamespace == "" {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}}
}

const kubeconfigSecretSuffix = "-kubeconfig" //nolint:gosec // G101 false positive: suffix for Secret name, not a credential

func (r *Reconciler) mapSecretToCluster(_ context.Context, obj client.Object) []reconcile.Request {
	name := obj.GetName()
	if !strings.HasSuffix(name, kubeconfigSecretSuffix) {
		return nil
	}
	clusterName := strings.TrimSuffix(name, kubeconfigSecretSuffix)
	if clusterName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      clusterName,
		},
	}}
}

// kubeconfigSecretPredicate filters Secret events to only those whose name
// ends with "-kubeconfig". This prevents the controller from processing
// unrelated Secret events (TLS certs, Helm releases, etc.) which would add
// unnecessary load to the work queue.
type kubeconfigSecretPredicate struct{}

func (kubeconfigSecretPredicate) Create(e event.CreateEvent) bool {
	return strings.HasSuffix(e.Object.GetName(), kubeconfigSecretSuffix)
}

func (kubeconfigSecretPredicate) Update(e event.UpdateEvent) bool {
	return strings.HasSuffix(e.ObjectNew.GetName(), kubeconfigSecretSuffix)
}

func (kubeconfigSecretPredicate) Delete(e event.DeleteEvent) bool {
	return strings.HasSuffix(e.Object.GetName(), kubeconfigSecretSuffix)
}

func (kubeconfigSecretPredicate) Generic(e event.GenericEvent) bool {
	return strings.HasSuffix(e.Object.GetName(), kubeconfigSecretSuffix)
}

func (r *Reconciler) desiredClusterProfileKey(cluster *unstructured.Unstructured) types.NamespacedName {
	clusterLabels := cluster.GetLabels()
	name := strings.TrimSpace(clusterLabels[wellknown.LabelClusterProfileName])
	if name == "" {
		name = cluster.GetName()
	}
	namespace := strings.TrimSpace(clusterLabels[wellknown.LabelClusterProfileNamespace])
	if namespace == "" {
		namespace = cluster.GetNamespace()
	}
	return types.NamespacedName{Name: name, Namespace: namespace}
}

func (r *Reconciler) buildClusterProfile(cluster *unstructured.Unstructured, key types.NamespacedName) *v1alpha1.ClusterProfile {
	managerName := r.clusterManagerName()
	cpLabels := map[string]string{
		wellknown.LabelSourceClusterName:      cluster.GetName(),
		wellknown.LabelSourceClusterNamespace: cluster.GetNamespace(),
		v1alpha1.LabelClusterManagerKey:       managerName,
	}
	maps.Copy(cpLabels, capicluster.ExtractDomainLabels(cluster, r.domainSuffix()))

	cp := &v1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    cpLabels,
		},
		Spec: v1alpha1.ClusterProfileSpec{
			ClusterManager: v1alpha1.ClusterManager{
				Name: managerName,
			},
		},
	}
	return cp
}

func (r *Reconciler) ensureClusterProfileShape(cp *v1alpha1.ClusterProfile, cluster *unstructured.Unstructured) bool {
	changed := false
	managerName := r.clusterManagerName()

	if cp.Labels == nil {
		cp.Labels = map[string]string{}
	}
	if cp.Labels[wellknown.LabelSourceClusterName] != cluster.GetName() {
		cp.Labels[wellknown.LabelSourceClusterName] = cluster.GetName()
		changed = true
	}
	if cp.Labels[wellknown.LabelSourceClusterNamespace] != cluster.GetNamespace() {
		cp.Labels[wellknown.LabelSourceClusterNamespace] = cluster.GetNamespace()
		changed = true
	}
	if cp.Labels[v1alpha1.LabelClusterManagerKey] != managerName {
		cp.Labels[v1alpha1.LabelClusterManagerKey] = managerName
		changed = true
	}
	// Sync domain labels from CAPI Cluster to ClusterProfile metadata.labels.
	for k, v := range capicluster.ExtractDomainLabels(cluster, r.domainSuffix()) {
		if cp.Labels[k] != v {
			cp.Labels[k] = v
			changed = true
		}
	}

	if cp.Spec.ClusterManager.Name != "" && cp.Spec.ClusterManager.Name != managerName {
		// ClusterManager is immutable in the CRD, so we cannot change it.
		// Log a warning but don't error out — the existing CP can still be used.
		return changed
	}
	if cp.Spec.ClusterManager.Name == "" {
		cp.Spec.ClusterManager.Name = managerName
		changed = true
	}

	return changed
}

func (r *Reconciler) listClusterProfilesForCluster(ctx context.Context, clusterKey types.NamespacedName) (*v1alpha1.ClusterProfileList, error) {
	list := &v1alpha1.ClusterProfileList{}

	sel := labels.SelectorFromSet(labels.Set{
		wellknown.LabelSourceClusterName:      clusterKey.Name,
		wellknown.LabelSourceClusterNamespace: clusterKey.Namespace,
	})

	if err := r.Client.List(ctx, list, &client.ListOptions{LabelSelector: sel}); err != nil {
		return nil, fmt.Errorf("list ClusterProfiles: %w", err)
	}
	return list, nil
}

// resolveAccessProviders reads the CAPI kubeconfig Secret, extracts server URL
// and CA, and builds the accessProviders SSA payload.
func (r *Reconciler) resolveAccessProviders(ctx context.Context, cluster *unstructured.Unstructured) accessProviderResult {
	// Check if control plane endpoint is available.
	host, _, _ := unstructured.NestedString(cluster.Object, "spec", "controlPlaneEndpoint", "host")
	if host == "" {
		accessProviderResolutionFailures.WithLabelValues("endpoint_not_available").Inc()
		return accessProviderResult{
			Reason:  accessProvidersReasonEndpointNotAvailable,
			Message: "control plane endpoint is not yet available",
		}
	}

	// Read the kubeconfig Secret.
	secretName := cluster.GetName() + "-kubeconfig"
	secretKey := types.NamespacedName{
		Namespace: cluster.GetNamespace(),
		Name:      secretName,
	}
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, secretKey, secret); err != nil {
		msg := fmt.Sprintf("failed to read kubeconfig Secret %s: %v", secretKey, err)
		accessProviderResolutionFailures.WithLabelValues("secret_read_failed").Inc()
		return accessProviderResult{
			Reason:  accessProvidersReasonSecretReadFailed,
			Message: msg,
		}
	}

	kubeconfigData, ok := secret.Data["value"]
	if !ok || len(kubeconfigData) == 0 {
		accessProviderResolutionFailures.WithLabelValues("secret_read_failed").Inc()
		return accessProviderResult{
			Reason:  accessProvidersReasonSecretReadFailed,
			Message: fmt.Sprintf("kubeconfig Secret %s missing or empty data key \"value\"", secretKey),
		}
	}

	// Parse the kubeconfig to extract server URL and CA data.
	kubeconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		accessProviderResolutionFailures.WithLabelValues("secret_read_failed").Inc()
		return accessProviderResult{
			Reason:  accessProvidersReasonSecretReadFailed,
			Message: fmt.Sprintf("failed to parse kubeconfig from Secret %s: %v", secretKey, err),
		}
	}

	// Find the first cluster entry in the kubeconfig.
	var serverURL string
	var caData []byte
	for _, c := range kubeconfig.Clusters {
		serverURL = c.Server
		caData = c.CertificateAuthorityData
		break
	}
	if serverURL == "" {
		accessProviderResolutionFailures.WithLabelValues("secret_read_failed").Inc()
		return accessProviderResult{
			Reason:  accessProvidersReasonSecretReadFailed,
			Message: fmt.Sprintf("kubeconfig from Secret %s contains no cluster entries with server URL", secretKey),
		}
	}

	// Build the accessProviders payload.
	provider := cpSDK.AccessProvider{
		Name:            "kubeconfig-secretreader",
		ServerURL:       serverURL,
		CAData:          caData,
		SecretNamespace: cluster.GetNamespace(),
		SecretName:      secretName,
		SecretKey:       "value",
	}
	payload := cpSDK.BuildAccessProviderPayload([]cpSDK.AccessProvider{provider})

	return accessProviderResult{
		Payload:   payload,
		Reason:    accessProvidersReasonPopulated,
		Message:   "accessProviders[kubeconfig-secretreader] populated with server URL " + serverURL,
		ServerURL: serverURL,
	}
}

// buildAccessProvidersCondition constructs a metav1.Condition-compatible
// unstructured map for the AccessProvidersReady condition.
func buildAccessProvidersCondition(result accessProviderResult, observedGeneration int64) map[string]any {
	status := metav1.ConditionFalse
	if result.Reason == accessProvidersReasonPopulated {
		status = metav1.ConditionTrue
	}
	return cpSDK.BuildConditionMap(conditionAccessProvidersReady, status, result.Reason, result.Message, observedGeneration)
}

// buildControlPlaneHealthyCondition constructs a metav1.Condition-compatible
// unstructured map for the ControlPlaneHealthy condition.
func buildControlPlaneHealthyCondition(apResult accessProviderResult, observedGeneration int64) map[string]any {
	status := metav1.ConditionFalse
	reason := controlPlaneReasonNotReady
	message := "control plane not yet reachable: " + apResult.Message
	if apResult.Reason == accessProvidersReasonPopulated {
		status = metav1.ConditionTrue
		reason = controlPlaneReasonHealthy
		message = "control plane is reachable"
	}
	return cpSDK.BuildConditionMap(v1alpha1.ClusterConditionControlPlaneHealthy, status, reason, message, observedGeneration)
}

// emitAccessProviderEvents records Kubernetes events based on the
// accessProvider write result.
func (r *Reconciler) emitAccessProviderEvents(cluster *unstructured.Unstructured, result accessProviderResult) {
	if r.Recorder == nil {
		return
	}

	switch result.Reason {
	case accessProvidersReasonPopulated:
		r.Recorder.Eventf(cluster, corev1.EventTypeNormal, eventAccessProviderPopulated, "%s", result.Message)
	case accessProvidersReasonEndpointNotAvailable:
		// Not a failure, just not ready yet — skip event.
	case accessProvidersReasonSecretReadFailed:
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, eventAccessProviderSyncFailed, "%s", result.Message)
	}
}
