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
	"errors"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	api "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

const (
	testManager    = "example.io/manager"
	newClassName   = "new"
	oldClassName   = "old"
	otherClassName = "other"
	testFleetNS    = "fleet"
	testSharedNS   = "shared"
)

func TestAddonUpdate(t *testing.T) {
	classes := []client.Object{
		classObject(oldClassName, testFleetNS, testManager),
		classObject(newClassName, testSharedNS, testManager),
		classObject(otherClassName, testSharedNS, "example.io/other"),
	}
	reader := testReader(t, classes...)
	base := sampleAddon()
	bound := base.DeepCopy()
	bound.Status.ClassRef = &api.AddonClassStatusReference{
		Name: oldClassName, Namespace: testFleetNS, ControllerName: testManager,
	}
	tests := []struct {
		name      string
		old, next api.Addon
		allow     bool
	}{
		{"same manager across namespaces", *bound, changedClass(bound, newClassName, testSharedNS), true},
		{"different manager", *bound, changedClass(bound, otherClassName, testSharedNS), false},
		{"missing new Class", *bound, changedClass(bound, "missing", testSharedNS), false},
		{
			"missing old Class", changedClass(bound, "missing", testFleetNS),
			changedClass(bound, newClassName, testSharedNS), false,
		},
		{"same resolved Class", *bound, changedClass(bound, oldClassName, testFleetNS), true},
		{"unbound with finalizer", withFinalizer(base), changedClass(&base, newClassName, testSharedNS), false},
		{"unbound without finalizer", base, changedClass(&base, newClassName, testSharedNS), true},
		{
			"recorded manager conflicts", withManager(*bound, "example.io/different"),
			changedClass(bound, newClassName, testSharedNS), false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := (Handlers{Reader: reader}).ValidateAddonUpdate(context.Background(), updateRequest(tt.old, tt.next, ""))
			if response.Allowed != tt.allow {
				t.Fatalf("allowed = %v, want %v: %v", response.Allowed, tt.allow, response.Result)
			}
		})
	}
	statusRequest := updateRequest(*bound, changedClass(bound, otherClassName, testSharedNS), "status")
	response := (Handlers{Reader: reader}).ValidateAddonUpdate(context.Background(), statusRequest)
	if !response.Allowed {
		t.Fatalf("status update was rejected: %v", response.Result)
	}
	missingClass := changedClass(bound, "missing", testFleetNS)
	orphan := *missingClass.DeepCopy()
	orphan.Spec.DeletionPolicy = api.AddonDeletionPolicyOrphan
	response = (Handlers{Reader: testReader(t)}).ValidateAddonUpdate(
		context.Background(), updateRequest(missingClass, orphan, ""))
	if !response.Allowed {
		t.Fatalf("policy change without a rebase was rejected while Class is missing: %v", response.Result)
	}
}

func TestAddonClassDelete(t *testing.T) {
	base := sampleAddon()
	bound := *base.DeepCopy()
	bound.Status.ClassRef = &api.AddonClassStatusReference{
		Name: oldClassName, Namespace: testFleetNS, ControllerName: testManager,
	}
	rebased := *bound.DeepCopy()
	rebased.Spec.ClassRef = api.AddonClassReference{Name: newClassName, Namespace: testSharedNS}
	crossNamespace := *base.DeepCopy()
	crossNamespace.Spec.ClassRef = api.AddonClassReference{Name: newClassName, Namespace: testSharedNS}
	crossNamespace.Status.ClassRef = &api.AddonClassStatusReference{
		Name: newClassName, Namespace: testSharedNS, ControllerName: testManager,
	}
	unacceptedCrossNamespace := changedClass(&base, newClassName, testSharedNS)
	deleting := *bound.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	deleting.Finalizers = []string{"example.io/finalizer"}
	tests := []struct {
		name                      string
		addon                     api.Addon
		namespace, class, manager string
		allow                     bool
	}{
		{"unaccepted local spec", base, testFleetNS, oldClassName, testManager, true},
		{"unaccepted shared spec", unacceptedCrossNamespace, testSharedNS, newClassName, testManager, true},
		{"accepted local binding", bound, testFleetNS, oldClassName, testManager, false},
		{"same name elsewhere", bound, testSharedNS, oldClassName, testManager, true},
		{"old status during rebase", rebased, testFleetNS, oldClassName, testManager, false},
		{"new spec during rebase", rebased, testSharedNS, newClassName, testManager, true},
		{"accepted cross namespace binding", crossNamespace, testSharedNS, newClassName, testManager, false},
		{"replacement with different manager", bound, testFleetNS, oldClassName, "example.io/other", true},
		{"deleting Addon remains bound", deleting, testFleetNS, oldClassName, testManager, false},
		{"unrelated Class", bound, testFleetNS, "unrelated", testManager, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := testReader(t, &tt.addon)
			request := deleteRequest(tt.namespace, tt.class, tt.manager)
			response := (Handlers{Reader: reader}).ValidateAddonClassDelete(context.Background(), request)
			if response.Allowed != tt.allow {
				t.Fatalf("allowed = %v, want %v: %v", response.Allowed, tt.allow, response.Result)
			}
		})
	}
	request := deleteRequest(testFleetNS, oldClassName, testManager)
	response := (Handlers{Reader: failingListReader{Reader: testReader(t)}}).
		ValidateAddonClassDelete(context.Background(), request)
	if response.Allowed || response.Result.Code != 500 {
		t.Fatalf("list failure did not fail closed: %v", response.Result)
	}
	paged := &pagedReader{}
	response = (Handlers{Reader: paged}).ValidateAddonClassDelete(context.Background(), request)
	if response.Allowed || paged.calls != 2 {
		t.Fatalf("reference on second list page was missed: response=%v calls=%d", response.Result, paged.calls)
	}
	request.OldObject.Raw = []byte("{")
	response = (Handlers{Reader: testReader(t)}).ValidateAddonClassDelete(context.Background(), request)
	if response.Allowed || response.Result.Code != 400 {
		t.Fatalf("malformed old Class was accepted: %v", response.Result)
	}
}

func testReader(t *testing.T, objects ...client.Object) client.Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func sampleAddon() api.Addon {
	return api.Addon{
		ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: testFleetNS},
		Spec:       api.AddonSpec{ClassRef: api.AddonClassReference{Name: oldClassName}},
	}
}

func classObject(name, namespace, manager string) *api.AddonClass {
	return &api.AddonClass{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       api.AddonClassSpec{ControllerName: manager},
	}
}

func changedClass(source *api.Addon, name, namespace string) api.Addon {
	next := *source.DeepCopy()
	next.Spec.ClassRef = api.AddonClassReference{Name: name, Namespace: namespace}
	return next
}

func withFinalizer(addon api.Addon) api.Addon {
	addon.Finalizers = []string{"example.io/finalizer"}
	return addon
}

func withManager(addon api.Addon, manager string) api.Addon {
	next := *addon.DeepCopy()
	next.Status.ClassRef.ControllerName = manager
	return next
}

func updateRequest(old, next api.Addon, subresource string) admission.Request {
	oldJSON, _ := json.Marshal(old)
	nextJSON, _ := json.Marshal(next)
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update, SubResource: subresource,
		OldObject: runtime.RawExtension{Raw: oldJSON}, Object: runtime.RawExtension{Raw: nextJSON},
	}}
}

func deleteRequest(namespace, name, manager string) admission.Request {
	oldJSON, _ := json.Marshal(classObject(name, namespace, manager))
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Delete, Namespace: namespace, Name: name,
		OldObject: runtime.RawExtension{Raw: oldJSON},
	}}
}

type failingListReader struct{ client.Reader }

func (failingListReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("unavailable")
}

type pagedReader struct {
	client.Reader
	calls int
}

func (p *pagedReader) List(_ context.Context, objects client.ObjectList, options ...client.ListOption) error {
	list := objects.(*api.AddonList)
	opts := &client.ListOptions{}
	for _, option := range options {
		option.ApplyToList(opts)
	}
	if p.calls == 0 {
		list.Continue = "next-page"
	} else if opts.Continue == "next-page" {
		list.Items = []api.Addon{{
			ObjectMeta: metav1.ObjectMeta{Name: "second-page", Namespace: testFleetNS},
			Spec:       api.AddonSpec{ClassRef: api.AddonClassReference{Name: oldClassName}},
			Status: api.AddonStatus{ClassRef: &api.AddonClassStatusReference{
				Name: oldClassName, Namespace: testFleetNS, ControllerName: testManager,
			}},
		}}
	} else {
		return errors.New("missing continuation token")
	}
	p.calls++
	return nil
}
