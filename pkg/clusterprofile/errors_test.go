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

package clusterprofile

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestConflictError(t *testing.T) {
	cause := fmt.Errorf("underlying conflict")
	err := &ConflictError{Cause: cause}

	if err.Error() != "underlying conflict" {
		t.Errorf("Error() = %q, want %q", err.Error(), "underlying conflict")
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap() did not return the original cause")
	}
}

func TestForbiddenError(t *testing.T) {
	cause := fmt.Errorf("underlying forbidden")
	err := &ForbiddenError{Cause: cause}

	if err.Error() != "underlying forbidden" {
		t.Errorf("Error() = %q, want %q", err.Error(), "underlying forbidden")
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap() did not return the original cause")
	}
}

func TestNotFoundError(t *testing.T) {
	cause := fmt.Errorf("underlying not found")
	err := &NotFoundError{Cause: cause}

	if err.Error() != "underlying not found" {
		t.Errorf("Error() = %q, want %q", err.Error(), "underlying not found")
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap() did not return the original cause")
	}
}

func TestClassifyError(t *testing.T) {
	gr := schema.GroupResource{Group: "test", Resource: "things"}

	t.Run("conflict error", func(t *testing.T) {
		apiErr := apierrors.NewConflict(gr, "my-thing", fmt.Errorf("conflict"))
		got := ClassifyError(apiErr, "action %s", "test")

		var conflictErr *ConflictError
		if !errors.As(got, &conflictErr) {
			t.Fatalf("expected *ConflictError, got %T: %v", got, got)
		}
		if conflictErr.Unwrap() == nil {
			t.Error("expected wrapped cause")
		}
	})

	t.Run("forbidden error", func(t *testing.T) {
		apiErr := &apierrors.StatusError{
			ErrStatus: metav1.Status{
				Status:  metav1.StatusFailure,
				Code:    http.StatusForbidden,
				Reason:  metav1.StatusReasonForbidden,
				Message: "forbidden",
			},
		}
		got := ClassifyError(apiErr, "action %s", "test")

		var forbiddenErr *ForbiddenError
		if !errors.As(got, &forbiddenErr) {
			t.Fatalf("expected *ForbiddenError, got %T: %v", got, got)
		}
	})

	t.Run("not found error", func(t *testing.T) {
		apiErr := apierrors.NewNotFound(gr, "my-thing")
		got := ClassifyError(apiErr, "action %s", "test")

		var notFoundErr *NotFoundError
		if !errors.As(got, &notFoundErr) {
			t.Fatalf("expected *NotFoundError, got %T: %v", got, got)
		}
	})

	t.Run("other error - wrapped with context", func(t *testing.T) {
		plainErr := fmt.Errorf("something else")
		got := ClassifyError(plainErr, "doing %s", "stuff")

		// Should not be any of the sentinel types.
		var conflictErr *ConflictError
		var forbiddenErr *ForbiddenError
		var notFoundErr *NotFoundError

		if errors.As(got, &conflictErr) {
			t.Error("should not be ConflictError")
		}
		if errors.As(got, &forbiddenErr) {
			t.Error("should not be ForbiddenError")
		}
		if errors.As(got, &notFoundErr) {
			t.Error("should not be NotFoundError")
		}

		// Should wrap the original error.
		if !errors.Is(got, plainErr) {
			t.Error("expected wrapped original error to be retrievable via errors.Is")
		}

		// Should contain the context message.
		if got.Error() != "doing stuff: something else" {
			t.Errorf("Error() = %q, want %q", got.Error(), "doing stuff: something else")
		}
	})
}
