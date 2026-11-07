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

package addoncert

import (
	"testing"
	"time"
)

func TestServingCertificateRenewal(t *testing.T) {
	cfg := Config{
		Namespace: "addon-system", ServiceName: "addon-webhook",
		LeafValidity: 365 * 24 * time.Hour, RenewBefore: 90 * 24 * time.Hour,
	}
	ca, key, caPEM, keyPEM, err := newCA(10 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if parsedCA, parsedKey, err := parseCA(caPEM, keyPEM); err != nil || parsedCA == nil || parsedKey == nil {
		t.Fatalf("generated CA cannot be read: %v", err)
	}
	certPEM, certKeyPEM, err := newServingCertificate(ca, key, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !servingCertificateValid(certPEM, certKeyPEM, ca, cfg) {
		t.Fatal("new serving certificate should be trusted and have the Service DNS name")
	}
	otherService := cfg
	otherService.ServiceName = "different-webhook"
	if servingCertificateValid(certPEM, certKeyPEM, ca, otherService) {
		t.Fatal("a certificate for another Service was accepted")
	}
	_, _, otherCA, otherKey, err := newCA(10 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	parsedOther, _, err := parseCA(otherCA, otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if servingCertificateValid(certPEM, certKeyPEM, parsedOther, cfg) {
		t.Fatal("a certificate signed by another CA was accepted")
	}
	tooEarly := cfg
	tooEarly.RenewBefore = cfg.LeafValidity
	if servingCertificateValid(certPEM, certKeyPEM, ca, tooEarly) {
		t.Fatal("a certificate in its renewal window was accepted")
	}
}
