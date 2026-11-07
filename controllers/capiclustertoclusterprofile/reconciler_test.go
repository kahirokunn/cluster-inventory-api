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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/cluster-inventory-api/pkg/capicluster"
	cpSDK "sigs.k8s.io/cluster-inventory-api/pkg/clusterprofile"
	"sigs.k8s.io/cluster-inventory-api/pkg/wellknown"
)

const testNamespace = "mgmt"

func newTestReconciler(objs ...runtime.Object) *Reconciler {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		switch v := o.(type) {
		case *unstructured.Unstructured:
			builder = builder.WithObjects(v)
		case *corev1.Secret:
			builder = builder.WithObjects(v)
		case *v1alpha1.ClusterProfile:
			builder = builder.WithObjects(v)
		}
	}

	return &Reconciler{
		Client: builder.Build(),
		Scheme: scheme,
		Log:    zap.New(zap.UseDevMode(true)),
	}
}

type recordingStatusApplier struct {
	key     types.NamespacedName
	payload map[string]any
	called  bool
}

func (r *recordingStatusApplier) ApplyStatus(_ context.Context, key types.NamespacedName, desiredStatus map[string]any, _ cpSDK.StatusPatchOptions) error {
	r.key = key
	r.payload = desiredStatus
	r.called = true
	return nil
}

type failingStatusApplier struct {
	err error
}

func (f *failingStatusApplier) ApplyStatus(_ context.Context, _ types.NamespacedName, _ map[string]any, _ cpSDK.StatusPatchOptions) error {
	return f.err
}

// makeClusterProfile creates a typed ClusterProfile with source labels.
func makeClusterProfile(name, namespace, srcClusterName, srcClusterNS string) *v1alpha1.ClusterProfile {
	return &v1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				wellknown.LabelSourceClusterName:      srcClusterName,
				wellknown.LabelSourceClusterNamespace: srcClusterNS,
			},
		},
		Spec: v1alpha1.ClusterProfileSpec{
			ClusterManager: v1alpha1.ClusterManager{
				Name: "capi",
			},
		},
	}
}

// makeKubeconfigYAML builds a minimal kubeconfig YAML with the given server and CA.
func makeKubeconfigYAML(server string, caData []byte) []byte {
	caB64 := base64.StdEncoding.EncodeToString(caData)
	return fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    certificate-authority-data: %s
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`, server, caB64)
}

// makeKubeconfigSecret creates a Secret with the kubeconfig convention in testNamespace.
func makeKubeconfigSecret(clusterName string, kubeconfigData []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-kubeconfig",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"value": kubeconfigData,
		},
	}
}

// makeClusterWithEndpoint creates a CAPI Cluster unstructured with controlPlaneEndpoint set in testNamespace.
func makeClusterWithEndpoint(name, host string) *unstructured.Unstructured {
	cluster := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"controlPlaneEndpoint": map[string]any{
				"host": host,
				"port": int64(6443),
			},
			"clusterNetwork": map[string]any{
				"pods": map[string]any{
					"cidrBlocks": []any{"10.10.0.0/16"},
				},
			},
		},
	}}
	cluster.SetGroupVersionKind(capicluster.GVK)
	cluster.SetName(name)
	cluster.SetNamespace(testNamespace)
	cluster.SetLabels(map[string]string{
		"role" + wellknown.LabelDomainSuffix + "/workload-cluster": "true",
	})
	return cluster
}

// requireAccessProviderEntry extracts the first accessProvider entry from the payload.
func requireAccessProviderEntry(payload map[string]any) map[string]any {
	apRaw, ok := payload["accessProviders"]
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "expected accessProviders in payload")
	apList, ok := apRaw.([]any)
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "expected []any for accessProviders")
	gomega.ExpectWithOffset(1, apList).To(gomega.HaveLen(1), "expected 1 accessProvider entry")
	entry, ok := apList[0].(map[string]any)
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "expected map entry")
	return entry
}

// requireClusterInfo extracts the cluster info map from an accessProvider entry.
func requireClusterInfo(entry map[string]any) map[string]any {
	clusterInfo, ok := entry["cluster"].(map[string]any)
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "expected cluster map")
	return clusterInfo
}

// extractAccessProvidersCondition extracts the AccessProvidersReady condition from the payload.
func extractAccessProvidersCondition(payload map[string]any) map[string]any {
	conditionsRaw, ok := payload["conditions"]
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "expected 'conditions' key in payload")
	conditions, ok := conditionsRaw.([]any)
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "expected conditions to be []any")
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == conditionAccessProvidersReady {
			return cond
		}
	}
	ginkgo.Fail(fmt.Sprintf("condition %q not found in payload", conditionAccessProvidersReady))
	return nil
}

var _ = ginkgo.Describe("Reconciler", func() {

	ginkgo.Describe("Reconcile", func() {

		ginkgo.It("should return zero result for not-found cluster", func() {
			r := newTestReconciler()

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "nonexistent-cluster",
				},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(ctrl.Result{}))
		})

		ginkgo.It("should create ClusterProfile eagerly when cluster exists without Secret", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("test-cluster")
			cluster.SetNamespace("default")
			cluster.SetLabels(map[string]string{})

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster)
			r.StatusApplier = applier

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "test-cluster",
				},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			// No Secret -> accessProviders not ready -> requeue.
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly (even without accessProviders).
			cp := &v1alpha1.ClusterProfile{}
			err = r.Client.Get(context.Background(), types.NamespacedName{Name: "test-cluster", Namespace: "default"}, cp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called with conditions showing not ready.
			gomega.Expect(applier.called).To(gomega.BeTrue())
		})

		ginkgo.It("should create ClusterProfile and apply status with not-ready conditions", func() {
			// No endpoint set and no kubeconfig Secret -> accessProviders not ready.
			// ClusterProfile SHOULD be created eagerly; StatusApplier SHOULD be called
			// with conditions showing not-ready state.
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			}}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("pmc-1")
			cluster.SetNamespace("pmc-1")
			cluster.SetLabels(map[string]string{
				wellknown.LabelClusterProfileName:                                     "pmc-1-cp",
				wellknown.LabelClusterProfileNamespace:                                "mesh-system",
				"role" + wellknown.LabelDomainSuffix + "/platform-management-cluster": "true",
			})

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster)
			r.StatusApplier = applier

			result, reconcileErr := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "pmc-1", Name: "pmc-1"},
			})
			gomega.Expect(reconcileErr).NotTo(gomega.HaveOccurred())
			// accessProviders not ready -> expects requeue.
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly.
			cp := &v1alpha1.ClusterProfile{}
			cpKey := types.NamespacedName{Name: "pmc-1-cp", Namespace: "mesh-system"}
			err := r.Client.Get(context.Background(), cpKey, cp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called with not-ready conditions.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			// Verify conditions show not-ready state.
			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonEndpointNotAvailable))

			// accessProviders should NOT be in the payload (endpoint not available).
			_, ok := applier.payload["accessProviders"]
			gomega.Expect(ok).To(gomega.BeFalse(), "expected accessProviders to be absent when endpoint is not available")
		})
	})

	ginkgo.Describe("SafeClusterFields", func() {

		ginkgo.It("should return only safe fields", func() {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(capicluster.GVK)
			u.SetName("my-cluster")
			u.SetNamespace("ns-1")
			u.SetUID("abc-123")
			u.SetLabels(map[string]string{
				"some-other-label": "should-not-appear",
			})

			fields := capicluster.SafeClusterFields(u)

			// Convert to map for easier checking.
			m := make(map[string]any)
			for i := 0; i < len(fields)-1; i += 2 {
				k, ok := fields[i].(string)
				gomega.Expect(ok).To(gomega.BeTrue(), fmt.Sprintf("expected string key at index %d, got %T", i, fields[i]))
				m[k] = fields[i+1]
			}

			gomega.Expect(m["name"]).To(gomega.Equal("my-cluster"))
			gomega.Expect(m["namespace"]).To(gomega.Equal("ns-1"))
			gomega.Expect(m["uid"]).To(gomega.Equal("abc-123"))
			gomega.Expect(m).NotTo(gomega.HaveKey("some-other-label"))
		})
	})

	ginkgo.Describe("ExtractPodCIDRs", func() {

		ginkgo.It("should extract valid pod CIDRs", func() {
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16", "10.11.0.0/16"},
						},
					},
				},
			}}

			cidrs := capicluster.ExtractPodCIDRs(cluster)
			gomega.Expect(cidrs).To(gomega.Equal([]string{"10.10.0.0/16", "10.11.0.0/16"}))
		})

		ginkgo.It("should extract single CIDR", func() {
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			}}

			cidrs := capicluster.ExtractPodCIDRs(cluster)
			gomega.Expect(cidrs).To(gomega.HaveLen(1))
			gomega.Expect(cidrs[0]).To(gomega.Equal("10.10.0.0/16"))
		})

		ginkgo.It("should return nil for missing spec", func() {
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{},
			}}

			cidrs := capicluster.ExtractPodCIDRs(cluster)
			gomega.Expect(cidrs).To(gomega.BeNil())
		})

		ginkgo.It("should return nil for empty cidrBlocks", func() {
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{},
						},
					},
				},
			}}

			cidrs := capicluster.ExtractPodCIDRs(cluster)
			gomega.Expect(cidrs).To(gomega.BeNil())
		})

		ginkgo.It("should return nil when cidrBlocks field is absent", func() {
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{},
					},
				},
			}}

			cidrs := capicluster.ExtractPodCIDRs(cluster)
			gomega.Expect(cidrs).To(gomega.BeNil())
		})
	})

	ginkgo.Describe("ExtractDomainLabels", func() {

		ginkgo.It("should extract domain labels", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetLabels(map[string]string{
				"default-region.aws" + wellknown.LabelDomainSuffix: "ap-northeast-1",
				"environment" + wellknown.LabelDomainSuffix:        "production",
				"cluster.clusterset.k8s.io":                        "fleet-a",
				"some-other-label":                                 "ignored",
			})

			got := capicluster.ExtractDomainLabels(cluster, wellknown.LabelDomainSuffix)
			want := map[string]string{
				"default-region.aws" + wellknown.LabelDomainSuffix: "ap-northeast-1",
				"environment" + wellknown.LabelDomainSuffix:        "production",
			}
			gomega.Expect(got).To(gomega.Equal(want))
		})

		ginkgo.It("should return empty map when no match", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetLabels(map[string]string{
				"cluster.clusterset.k8s.io": "fleet-a",
				"some-label":                "value",
			})

			got := capicluster.ExtractDomainLabels(cluster, wellknown.LabelDomainSuffix)
			gomega.Expect(got).To(gomega.BeEmpty())
		})

		ginkgo.It("should include labels with slash in key", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetLabels(map[string]string{
				"source" + wellknown.LabelDomainSuffix + "/cluster-name":   "my-cluster",
				"role" + wellknown.LabelDomainSuffix + "/workload-cluster": "true",
				"environment" + wellknown.LabelDomainSuffix:                "staging",
				"unrelated.example.com/foo":                                "bar",
			})

			got := capicluster.ExtractDomainLabels(cluster, wellknown.LabelDomainSuffix)
			want := map[string]string{
				"source" + wellknown.LabelDomainSuffix + "/cluster-name":   "my-cluster",
				"role" + wellknown.LabelDomainSuffix + "/workload-cluster": "true",
				"environment" + wellknown.LabelDomainSuffix:                "staging",
			}
			gomega.Expect(got).To(gomega.Equal(want))
		})
	})

	ginkgo.Describe("resolveClusterProfileKey (via listClusterProfilesForCluster)", func() {

		ginkgo.It("should find a matching ClusterProfile", func() {
			cp := makeClusterProfile("bc4cp", "bc4-clusterprofile", "bc4", "bc4")

			r := newTestReconciler(cp)
			list, err := r.listClusterProfilesForCluster(context.Background(), types.NamespacedName{
				Namespace: "bc4",
				Name:      "bc4",
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(list.Items).To(gomega.HaveLen(1))
			gomega.Expect(list.Items[0].Name).To(gomega.Equal("bc4cp"))
			gomega.Expect(list.Items[0].Namespace).To(gomega.Equal("bc4-clusterprofile"))
		})

		ginkgo.It("should return empty list for no matching ClusterProfile", func() {
			r := newTestReconciler()
			list, err := r.listClusterProfilesForCluster(context.Background(), types.NamespacedName{
				Namespace: "default",
				Name:      "my-cluster",
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(list.Items).To(gomega.BeEmpty())
		})

		ginkgo.It("should return multiple matches", func() {
			cp1 := makeClusterProfile("cp1", "ns1", "cluster1", "default")
			cp2 := makeClusterProfile("cp2", "ns2", "cluster1", "default")

			r := newTestReconciler(cp1, cp2)
			list, err := r.listClusterProfilesForCluster(context.Background(), types.NamespacedName{
				Namespace: "default",
				Name:      "cluster1",
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(list.Items).To(gomega.HaveLen(2))
		})
	})

	ginkgo.Describe("mapClusterProfileToCluster", func() {

		ginkgo.It("should enqueue source cluster", func() {
			cp := makeClusterProfile("bc4cp", "bc4-clusterprofile", "bc4", "bc4")

			r := newTestReconciler(cp)
			requests := r.mapClusterProfileToCluster(context.Background(), cp)
			gomega.Expect(requests).To(gomega.HaveLen(1))
			gomega.Expect(requests[0].NamespacedName).To(gomega.Equal(types.NamespacedName{Namespace: "bc4", Name: "bc4"}))
		})

		ginkgo.It("should ignore ClusterProfiles without source labels", func() {
			cp := &v1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bc4cp",
					Namespace: "bc4-clusterprofile",
				},
			}

			r := newTestReconciler(cp)
			requests := r.mapClusterProfileToCluster(context.Background(), cp)
			gomega.Expect(requests).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("desiredClusterProfileKey", func() {

		ginkgo.It("should default to cluster identity", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetName("wl1")
			cluster.SetNamespace(testNamespace)

			r := newTestReconciler()
			key := r.desiredClusterProfileKey(cluster)
			gomega.Expect(key.Name).To(gomega.Equal("wl1"))
			gomega.Expect(key.Namespace).To(gomega.Equal(testNamespace))
		})

		ginkgo.It("should honor override labels", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetName("wl1")
			cluster.SetNamespace(testNamespace)
			cluster.SetLabels(map[string]string{
				wellknown.LabelClusterProfileName:      "wl1-cp",
				wellknown.LabelClusterProfileNamespace: "mesh-system",
			})

			r := newTestReconciler()
			key := r.desiredClusterProfileKey(cluster)
			gomega.Expect(key.Name).To(gomega.Equal("wl1-cp"))
			gomega.Expect(key.Namespace).To(gomega.Equal("mesh-system"))
		})
	})

	ginkgo.Describe("ensureClusterProfileShape", func() {

		ginkgo.It("should adopt desired key object", func() {
			cluster := &unstructured.Unstructured{}
			cluster.SetName("wl1")
			cluster.SetNamespace(testNamespace)

			cp := &v1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wl1",
					Namespace: testNamespace,
					Labels:    map[string]string{},
				},
			}

			r := newTestReconciler()
			changed := r.ensureClusterProfileShape(cp, cluster)
			gomega.Expect(changed).To(gomega.BeTrue())
			gomega.Expect(cp.Labels[wellknown.LabelSourceClusterName]).To(gomega.Equal("wl1"))
			gomega.Expect(cp.Labels[wellknown.LabelSourceClusterNamespace]).To(gomega.Equal(testNamespace))
			gomega.Expect(cp.Spec.ClusterManager.Name).To(gomega.Equal("capi"))
		})
	})

	ginkgo.Describe("OwnedProperties isolation", func() {

		ginkgo.It("should not include Cilium or Istio properties", func() {
			// Verify that the capi-cluster-to-clusterprofile-controller's
			// owned properties never include Cilium-written keys.
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			}}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("test-cluster")
			cluster.SetNamespace("default")
			cluster.SetLabels(map[string]string{
				"cluster.clusterset.k8s.io":                              "fleet-a",
				"default-region.aws" + wellknown.LabelDomainSuffix:       "ap-northeast-1",
				"environment" + wellknown.LabelDomainSuffix:              "staging",
				"source" + wellknown.LabelDomainSuffix + "/cluster-name": "test-cluster",
			})

			// Replicate the owned property computation from the reconciler.
			podCIDRs := capicluster.ExtractPodCIDRs(cluster)
			owned := make(map[string]string)
			if len(podCIDRs) > 0 {
				owned[wellknown.PodCIDRProperty] = podCIDRs[0]
			}
			if cs, ok := cluster.GetLabels()["cluster.clusterset.k8s.io"]; ok && cs != "" {
				owned["cluster.clusterset.k8s.io"] = cs
			}
			maps.Copy(owned, capicluster.ExtractDomainLabels(cluster, wellknown.LabelDomainSuffix))

			// Verify we own what we expect.
			gomega.Expect(owned).To(gomega.HaveKeyWithValue(wellknown.PodCIDRProperty, "10.10.0.0/16"))
			gomega.Expect(owned).To(gomega.HaveKey("cluster.clusterset.k8s.io"))
			gomega.Expect(owned).To(gomega.HaveKey("default-region.aws" + wellknown.LabelDomainSuffix))
			gomega.Expect(owned).To(gomega.HaveKey("environment" + wellknown.LabelDomainSuffix))

			// Verify prefixed domain labels (with "/") ARE included as owned properties.
			gomega.Expect(owned).To(gomega.HaveKey("source" + wellknown.LabelDomainSuffix + "/cluster-name"))

			// Verify we do NOT own Cilium or Istio properties.
			foreignKeys := []string{
				wellknown.SubnetIDProperty,
				wellknown.ClusterIDProperty,
			}
			for _, key := range foreignKeys {
				gomega.Expect(owned).NotTo(gomega.HaveKey(key),
					fmt.Sprintf("capi-cluster-to-clusterprofile-controller must not own %q (foreign property)", key))
			}
		})
	})

	ginkgo.Describe("accessProviders", func() {

		ginkgo.It("should build correct payload with valid Secret", func() {
			caData := []byte("test-ca-data")
			serverURL := "https://10.0.0.1:6443"
			kubeconfigData := makeKubeconfigYAML(serverURL, caData)

			cluster := makeClusterWithEndpoint("wlc-1", "10.0.0.1")
			secret := makeKubeconfigSecret("wlc-1", kubeconfigData)

			applier := &recordingStatusApplier{}
			recorder := record.NewFakeRecorder(10)
			r := newTestReconciler(cluster, secret)
			r.StatusApplier = applier
			r.Recorder = recorder

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "wlc-1"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(applier.called).To(gomega.BeTrue())

			ginkgo.By("verifying accessProviders entry")
			entry := requireAccessProviderEntry(applier.payload)
			gomega.Expect(entry["name"]).To(gomega.Equal("kubeconfig-secretreader"))

			clusterInfo := requireClusterInfo(entry)
			gomega.Expect(clusterInfo["server"]).To(gomega.Equal(serverURL))

			ginkgo.By("verifying CA data base64 encoded")
			caEncoded, ok := clusterInfo["certificate-authority-data"].(string)
			gomega.Expect(ok).To(gomega.BeTrue(), "expected string CA data")
			caDecoded, decodeErr := base64.StdEncoding.DecodeString(caEncoded)
			gomega.Expect(decodeErr).NotTo(gomega.HaveOccurred())
			gomega.Expect(bytes.Equal(caDecoded, caData)).To(gomega.BeTrue())

			ginkgo.By("verifying extensions contain Secret coordinates")
			extensions, ok := clusterInfo["extensions"].([]any)
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(extensions).To(gomega.HaveLen(1))
			ext, ok := extensions[0].(map[string]any)
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(ext["name"]).To(gomega.Equal("client.authentication.k8s.io/exec"))
			extObj, ok := ext["extension"].(map[string]any)
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(extObj["namespace"]).To(gomega.Equal(testNamespace))
			gomega.Expect(extObj["name"]).To(gomega.Equal("wlc-1-kubeconfig"))
			gomega.Expect(extObj["key"]).To(gomega.Equal("value"))

			ginkgo.By("verifying properties in merged payload")
			_, ok = applier.payload["properties"]
			gomega.Expect(ok).To(gomega.BeTrue(), "expected properties in merged payload")

			ginkgo.By("verifying Normal event emitted")
			var event string
			gomega.Eventually(recorder.Events).Should(gomega.Receive(&event))
			gomega.Expect(event).To(gomega.ContainSubstring(eventAccessProviderPopulated))
			gomega.Expect(event).To(gomega.ContainSubstring(serverURL))
		})

		ginkgo.It("should handle endpoint not available", func() {
			// Cluster without controlPlaneEndpoint - endpoint not yet provisioned.
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			}}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("wlc-2")
			cluster.SetNamespace(testNamespace)
			cluster.SetLabels(map[string]string{
				"role" + wellknown.LabelDomainSuffix + "/workload-cluster": "true",
			})

			applier := &recordingStatusApplier{}
			recorder := record.NewFakeRecorder(10)
			r := newTestReconciler(cluster)
			r.StatusApplier = applier
			r.Recorder = recorder

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "wlc-2"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Should requeue since accessProviders are not ready.
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly.
			cp := &v1alpha1.ClusterProfile{}
			err = r.Client.Get(context.Background(), types.NamespacedName{Name: "wlc-2", Namespace: testNamespace}, cp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called with not-ready conditions.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			// Verify conditions show endpoint not available.
			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonEndpointNotAvailable))

			// No event should be emitted for EndpointNotAvailable.
			gomega.Consistently(recorder.Events).ShouldNot(gomega.Receive())
		})

		ginkgo.It("should handle Secret read failure", func() {
			// Cluster with endpoint but no kubeconfig Secret.
			cluster := makeClusterWithEndpoint("wlc-3", "10.0.0.2")

			applier := &recordingStatusApplier{}
			recorder := record.NewFakeRecorder(10)
			r := newTestReconciler(cluster)
			r.StatusApplier = applier
			r.Recorder = recorder

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "wlc-3"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Should requeue since accessProviders are not ready.
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly.
			cp := &v1alpha1.ClusterProfile{}
			err = r.Client.Get(context.Background(), types.NamespacedName{Name: "wlc-3", Namespace: testNamespace}, cp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called with not-ready conditions.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			// Verify conditions show Secret read failure.
			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonSecretReadFailed))
		})

		ginkgo.It("should emit Normal event on first success", func() {
			caData := []byte("ca-for-event-test")
			serverURL := "https://10.0.0.5:6443"
			kubeconfigData := makeKubeconfigYAML(serverURL, caData)

			cluster := makeClusterWithEndpoint("wlc-4", "10.0.0.5")
			secret := makeKubeconfigSecret("wlc-4", kubeconfigData)

			applier := &recordingStatusApplier{}
			recorder := record.NewFakeRecorder(10)
			r := newTestReconciler(cluster, secret)
			r.StatusApplier = applier
			r.Recorder = recorder

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "wlc-4"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify Normal event is emitted on first success.
			var event string
			gomega.Eventually(recorder.Events).Should(gomega.Receive(&event))
			gomega.Expect(event).To(gomega.ContainSubstring("Normal"))
			gomega.Expect(event).To(gomega.ContainSubstring(eventAccessProviderPopulated))
		})

		ginkgo.It("should produce merged payload without conflicts", func() {
			// Verifies that the merged SSA payload contains both
			// properties and accessProviders without field conflicts.
			caData := []byte("ca-for-merge-test")
			serverURL := "https://10.0.0.10:6443"
			kubeconfigData := makeKubeconfigYAML(serverURL, caData)

			cluster := makeClusterWithEndpoint("wlc-5", "10.0.0.10")
			secret := makeKubeconfigSecret("wlc-5", kubeconfigData)

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster, secret)
			r.StatusApplier = applier

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "wlc-5"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(applier.called).To(gomega.BeTrue())

			// Verify all expected top-level keys exist in the merged payload.
			gomega.Expect(applier.payload).To(gomega.HaveKey("properties"))
			gomega.Expect(applier.payload).To(gomega.HaveKey("accessProviders"))
			gomega.Expect(applier.payload).To(gomega.HaveKey("conditions"))

			// Verify these are the only top-level keys (no unexpected overlap).
			for key := range applier.payload {
				gomega.Expect(key).To(gomega.BeElementOf("properties", "accessProviders", "conditions"),
					"unexpected key %q in merged payload", key)
			}
		})
	})

	ginkgo.Describe("AccessProvidersReady condition", func() {

		ginkgo.It("should write Populated condition to status", func() {
			caData := []byte("ca-for-cond-test")
			serverURL := "https://10.0.0.20:6443"
			kubeconfigData := makeKubeconfigYAML(serverURL, caData)

			cluster := makeClusterWithEndpoint("cond-ok", "10.0.0.20")
			secret := makeKubeconfigSecret("cond-ok", kubeconfigData)

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster, secret)
			r.StatusApplier = applier

			before := time.Now().UTC()
			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "cond-ok"},
			})
			after := time.Now().UTC()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(applier.called).To(gomega.BeTrue())

			cond := extractAccessProvidersCondition(applier.payload)

			gomega.Expect(cond["status"]).To(gomega.Equal("True"))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonPopulated))
			gomega.Expect(cond["message"]).NotTo(gomega.BeEmpty())

			// Verify observedGeneration is present.
			gen, ok := cond["observedGeneration"]
			gomega.Expect(ok).To(gomega.BeTrue(), "condition missing observedGeneration")
			// Generation from unstructured defaults to 0.
			gomega.Expect(gen).To(gomega.Equal(int64(0)))

			// Verify lastTransitionTime is valid RFC3339 and within bounds.
			ltt, ok := cond["lastTransitionTime"].(string)
			gomega.Expect(ok).To(gomega.BeTrue(), "condition lastTransitionTime missing or not a string")
			gomega.Expect(ltt).NotTo(gomega.BeEmpty())
			parsed, parseErr := time.Parse(time.RFC3339, ltt)
			gomega.Expect(parseErr).NotTo(gomega.HaveOccurred())
			gomega.Expect(parsed).To(gomega.BeTemporally(">=", before.Truncate(time.Second)))
			gomega.Expect(parsed).To(gomega.BeTemporally("<=", after.Add(time.Second)))
		})

		ginkgo.It("should write EndpointNotAvailable condition to status", func() {
			// Cluster without controlPlaneEndpoint.
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			}}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("cond-noep")
			cluster.SetNamespace(testNamespace)
			cluster.SetLabels(map[string]string{
				"role" + wellknown.LabelDomainSuffix + "/workload-cluster": "true",
			})

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster)
			r.StatusApplier = applier

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "cond-noep"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly.
			cp := &v1alpha1.ClusterProfile{}
			err = r.Client.Get(context.Background(), types.NamespacedName{Name: "cond-noep", Namespace: testNamespace}, cp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called with not-ready conditions.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonEndpointNotAvailable))
		})

		ginkgo.It("should write SecretReadFailed condition to status", func() {
			// Cluster with endpoint but no kubeconfig Secret.
			cluster := makeClusterWithEndpoint("cond-nosec", "10.0.0.30")

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster)
			r.StatusApplier = applier

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "cond-nosec"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly.
			cp := &v1alpha1.ClusterProfile{}
			err = r.Client.Get(context.Background(), types.NamespacedName{Name: "cond-nosec", Namespace: testNamespace}, cp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called with not-ready conditions.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonSecretReadFailed))
		})

		ginkgo.It("should set observedGeneration matching cluster generation", func() {
			caData := []byte("ca-for-gen-test")
			serverURL := "https://10.0.0.40:6443"
			kubeconfigData := makeKubeconfigYAML(serverURL, caData)

			cluster := makeClusterWithEndpoint("cond-gen", "10.0.0.40")
			// Set a non-zero generation on the cluster.
			cluster.SetGeneration(42)
			secret := makeKubeconfigSecret("cond-gen", kubeconfigData)

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster, secret)
			r.StatusApplier = applier

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "cond-gen"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(applier.called).To(gomega.BeTrue())

			cond := extractAccessProvidersCondition(applier.payload)

			gen, ok := cond["observedGeneration"]
			gomega.Expect(ok).To(gomega.BeTrue(), "condition missing observedGeneration")
			gomega.Expect(gen).To(gomega.Equal(int64(42)))
		})

		ginkgo.It("should write lastTransitionTime in RFC3339 format", func() {
			// Cluster with endpoint but no kubeconfig Secret.
			cluster := makeClusterWithEndpoint("cond-rfc", "10.0.0.50")

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster)
			r.StatusApplier = applier

			before := time.Now().UTC()
			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "cond-rfc"},
			})
			after := time.Now().UTC()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile SHOULD be created eagerly.
			cp := &v1alpha1.ClusterProfile{}
			getErr := r.Client.Get(context.Background(), types.NamespacedName{Name: "cond-rfc", Namespace: testNamespace}, cp)
			gomega.Expect(getErr).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			cond := extractAccessProvidersCondition(applier.payload)
			ltt, ok := cond["lastTransitionTime"].(string)
			gomega.Expect(ok).To(gomega.BeTrue(), "expected string lastTransitionTime")
			parsed, parseErr := time.Parse(time.RFC3339, ltt)
			gomega.Expect(parseErr).NotTo(gomega.HaveOccurred())
			gomega.Expect(parsed).To(gomega.BeTemporally(">=", before.Truncate(time.Second)))
			gomega.Expect(parsed).To(gomega.BeTemporally("<=", after.Add(time.Second)))
		})
	})

	ginkgo.Describe("Cluster deletion flow", func() {

		ginkgo.It("should return immediately for a cluster with DeletionTimestamp set", func() {
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{},
			}}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("deleting-cluster")
			cluster.SetNamespace(testNamespace)
			cluster.SetLabels(map[string]string{})
			// A finalizer is required for the fake client to accept a deletionTimestamp.
			cluster.SetFinalizers([]string{"test.finalizer/block-deletion"})

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster)
			r.StatusApplier = applier

			// Simulate deletion by issuing a Delete, which sets DeletionTimestamp
			// on objects with finalizers.
			err := r.Client.Delete(context.Background(), cluster)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "deleting-cluster"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.Equal(ctrl.Result{}))
			gomega.Expect(applier.called).To(gomega.BeFalse(), "StatusApplier should not be called for a deleting cluster")
		})
	})

	ginkgo.Describe("StatusApplier failure", func() {

		ginkgo.It("should return error when StatusApplier.ApplyStatus fails", func() {
			cluster := makeClusterWithEndpoint("applier-fail", "10.0.0.99")
			caData := []byte("ca-for-applier-fail")
			serverURL := "https://10.0.0.99:6443"
			kubeconfigData := makeKubeconfigYAML(serverURL, caData)
			secret := makeKubeconfigSecret("applier-fail", kubeconfigData)

			failApplier := &failingStatusApplier{err: fmt.Errorf("apply status exploded")}
			r := newTestReconciler(cluster, secret)
			r.StatusApplier = failApplier

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "applier-fail"},
			})
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("SSA status patch"))
		})
	})

	ginkgo.Describe("existing ClusterProfile updates", func() {

		ginkgo.It("should still update status when ClusterProfile exists but Secret is missing", func() {
			// When a ClusterProfile already exists but accessProviders become
			// unavailable (e.g., Secret deleted), the status/conditions should
			// still be updated - not just skipped.
			cluster := makeClusterWithEndpoint("existing-cp", "10.0.0.60")

			// Pre-create a typed ClusterProfile to simulate it already existing.
			cp := &v1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-cp",
					Namespace: testNamespace,
					Labels: map[string]string{
						wellknown.LabelSourceClusterName:      "existing-cp",
						wellknown.LabelSourceClusterNamespace: testNamespace,
						v1alpha1.LabelClusterManagerKey:       "capi",
					},
				},
				Spec: v1alpha1.ClusterProfileSpec{
					ClusterManager: v1alpha1.ClusterManager{
						Name: "capi",
					},
				},
			}

			// No kubeconfig Secret - accessProviders will not be ready.
			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster, cp)
			r.StatusApplier = applier

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "existing-cp"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Should requeue since accessProviders are not ready.
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// ClusterProfile should still exist (not deleted).
			got := &v1alpha1.ClusterProfile{}
			err = r.Client.Get(context.Background(), types.NamespacedName{Name: "existing-cp", Namespace: testNamespace}, got)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// StatusApplier SHOULD be called (conditions updated with accessProvidersReady: false).
			gomega.Expect(applier.called).To(gomega.BeTrue())

			// Verify the condition shows accessProviders are not ready.
			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonSecretReadFailed))

			// accessProviders should NOT be in the payload (Secret is missing).
			_, ok := applier.payload["accessProviders"]
			gomega.Expect(ok).To(gomega.BeFalse(), "expected accessProviders to be absent when Secret is not available")
		})

		ginkgo.It("should still update status when ClusterProfile exists but endpoint is unavailable", func() {
			// When a ClusterProfile already exists but the control plane endpoint
			// becomes unavailable, the status/conditions should still be updated.
			cluster := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			}}
			cluster.SetGroupVersionKind(capicluster.GVK)
			cluster.SetName("ep-gone")
			cluster.SetNamespace(testNamespace)
			cluster.SetLabels(map[string]string{
				"role" + wellknown.LabelDomainSuffix + "/workload-cluster": "true",
			})

			// Pre-create a typed ClusterProfile to simulate it already existing.
			cp := &v1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ep-gone",
					Namespace: testNamespace,
					Labels: map[string]string{
						wellknown.LabelSourceClusterName:      "ep-gone",
						wellknown.LabelSourceClusterNamespace: testNamespace,
						v1alpha1.LabelClusterManagerKey:       "capi",
					},
				},
				Spec: v1alpha1.ClusterProfileSpec{
					ClusterManager: v1alpha1.ClusterManager{
						Name: "capi",
					},
				},
			}

			applier := &recordingStatusApplier{}
			r := newTestReconciler(cluster, cp)
			r.StatusApplier = applier

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "ep-gone"},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result.RequeueAfter).To(gomega.Equal(15 * time.Second))

			// StatusApplier SHOULD be called - update conditions on existing ClusterProfile.
			gomega.Expect(applier.called).To(gomega.BeTrue())

			cond := extractAccessProvidersCondition(applier.payload)
			gomega.Expect(cond["status"]).To(gomega.Equal(string(metav1.ConditionFalse)))
			gomega.Expect(cond["reason"]).To(gomega.Equal(accessProvidersReasonEndpointNotAvailable))

			// accessProviders should NOT be in the payload.
			_, ok := applier.payload["accessProviders"]
			gomega.Expect(ok).To(gomega.BeFalse(), "expected accessProviders to be absent when endpoint is not available")
		})
	})
})
