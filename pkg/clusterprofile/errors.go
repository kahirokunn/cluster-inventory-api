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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Sentinel error types for callers that need to distinguish error categories
// without importing k8s.io/apimachinery.
type (
	// ConflictError indicates an SSA conflict (HTTP 409).
	ConflictError struct{ Cause error }
	// ForbiddenError indicates an authorization failure (HTTP 403).
	ForbiddenError struct{ Cause error }
	// NotFoundError indicates the target resource does not exist (HTTP 404).
	NotFoundError struct{ Cause error }
)

func (e *ConflictError) Error() string  { return e.Cause.Error() }
func (e *ConflictError) Unwrap() error  { return e.Cause }
func (e *ForbiddenError) Error() string { return e.Cause.Error() }
func (e *ForbiddenError) Unwrap() error { return e.Cause }
func (e *NotFoundError) Error() string  { return e.Cause.Error() }
func (e *NotFoundError) Unwrap() error  { return e.Cause }

// ClassifyError wraps a Kubernetes API error into one of the sentinel types
// so callers can handle conflict/forbidden/notfound without importing
// apimachinery. If the error doesn't match a known category it is returned
// with added context.
func ClassifyError(err error, msgfmt string, args ...any) error {
	msg := fmt.Sprintf(msgfmt, args...)
	switch {
	case apierrors.IsConflict(err):
		return &ConflictError{Cause: fmt.Errorf("%s: %w", msg, err)}
	case apierrors.IsForbidden(err):
		return &ForbiddenError{Cause: fmt.Errorf("%s: %w", msg, err)}
	case apierrors.IsNotFound(err):
		return &NotFoundError{Cause: fmt.Errorf("%s: %w", msg, err)}
	default:
		return fmt.Errorf("%s: %w", msg, err)
	}
}
