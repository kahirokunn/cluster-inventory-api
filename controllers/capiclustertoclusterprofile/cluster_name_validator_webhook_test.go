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
	"encoding/json"
	"net/http"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/cluster-inventory-api/pkg/capicluster"
)

func newClusterWebhookTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	return s
}

func newClusterObj(name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(capicluster.GVK)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	return obj
}

func newClusterAdmissionRequest(op admissionv1.Operation, cluster *unstructured.Unstructured) admission.Request {
	raw, _ := json.Marshal(cluster)
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Operation: op,
			Object: runtime.RawExtension{
				Raw: raw,
			},
			Namespace: cluster.GetNamespace(),
			Name:      cluster.GetName(),
		},
	}
}

var _ = ginkgo.Describe("ClusterNameValidator webhook", func() {
	ginkgo.It("should allow CREATE with a unique name", func() {
		scheme := newClusterWebhookTestScheme()
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns1")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Create, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeTrue(),
			"expected allowed, got denied: %s", resp.Result.Message)
	})

	ginkgo.It("should deny CREATE with duplicate name in different namespace", func() {
		scheme := newClusterWebhookTestScheme()

		existing := newClusterObj("foo", "ns1")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existing).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns2")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Create, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeFalse(),
			"expected denied for duplicate name in different namespace, got allowed")
		gomega.Expect(resp.Result.Code).To(gomega.Equal(int32(http.StatusForbidden)))
	})

	ginkgo.It("should allow CREATE with same name in same namespace", func() {
		scheme := newClusterWebhookTestScheme()

		existing := newClusterObj("foo", "ns1")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existing).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns1")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Create, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeTrue(),
			"expected allowed (same namespace), got denied: %s", resp.Result.Message)
	})

	ginkgo.It("should allow UPDATE with no duplicate", func() {
		scheme := newClusterWebhookTestScheme()

		existing := newClusterObj("foo", "ns1")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existing).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns1")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Update, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeTrue(),
			"expected UPDATE to be allowed (no duplicate), got denied: %s", resp.Result.Message)
	})

	ginkgo.It("should deny UPDATE with legacy duplicate", func() {
		scheme := newClusterWebhookTestScheme()

		existing1 := newClusterObj("foo", "ns1")
		existing2 := newClusterObj("foo", "ns2")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existing1, existing2).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns1")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Update, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeFalse(),
			"expected UPDATE denied for legacy duplicate name, got allowed")
		gomega.Expect(resp.Result.Code).To(gomega.Equal(int32(http.StatusForbidden)))
	})

	ginkgo.It("should always allow DELETE", func() {
		scheme := newClusterWebhookTestScheme()
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns1")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Delete, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeTrue(),
			"expected DELETE to be allowed, got denied: %s", resp.Result.Message)
	})

	ginkgo.It("should allow CREATE when multiple existing clusters have no name conflict", func() {
		scheme := newClusterWebhookTestScheme()

		existing1 := newClusterObj("bar", "ns1")
		existing2 := newClusterObj("baz", "ns2")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existing1, existing2).
			Build()

		v := NewClusterNameValidator(c, scheme)

		cluster := newClusterObj("foo", "ns3")
		resp := v.Handle(context.Background(), newClusterAdmissionRequest(admissionv1.Create, cluster))
		gomega.Expect(resp.Allowed).To(gomega.BeTrue(),
			"expected allowed (no name conflict), got denied: %s", resp.Result.Message)
	})

	ginkgo.It("should deny CREATE when request body is malformed (decoder failure)", func() {
		scheme := newClusterWebhookTestScheme()
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		v := NewClusterNameValidator(c, scheme)

		// Build a request with invalid raw JSON that the decoder cannot parse.
		badReq := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UID:       "test-uid",
				Operation: admissionv1.Create,
				Object: runtime.RawExtension{
					Raw: []byte(`{this is not valid json}`),
				},
				Namespace: "ns1",
				Name:      "bad-cluster",
			},
		}

		resp := v.Handle(context.Background(), badReq)
		gomega.Expect(resp.Allowed).To(gomega.BeFalse(),
			"expected denied for malformed request body")
		gomega.Expect(resp.Result.Code).To(gomega.Equal(int32(http.StatusBadRequest)))
	})
})
