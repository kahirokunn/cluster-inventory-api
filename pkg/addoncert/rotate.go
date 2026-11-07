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
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config names the Helm-managed resources used for CronJob certificate renewal.
type Config struct {
	Namespace, ServiceName, CASecretName, TLSSecretName, WebhookConfigName, DeploymentName string
	CAValidity, LeafValidity, RenewBefore                                                  time.Duration
	ForceRenew, ForceCARotation                                                            bool
}

// Rotate renews the serving certificate and, when needed, the CA. It widens
// webhook trust before replacing a CA and narrows it only after the serving
// Deployment has rolled out with the new certificate.
func Rotate(ctx context.Context, c client.Client, cfg Config) error {
	caKey := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.CASecretName}
	tlsKey := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.TLSSecretName}
	var caSecret corev1.Secret
	if err := c.Get(ctx, caKey, &caSecret); err != nil {
		return fmt.Errorf("get CA Secret: %w", err)
	}
	caCert, caSigner, err := parseCA(caSecret.Data["ca.crt"], caSecret.Data["ca.key"])
	if err != nil {
		return fmt.Errorf("parse CA Secret: %w", err)
	}
	var tlsSecret corev1.Secret
	err = c.Get(ctx, tlsKey, &tlsSecret)
	missingTLS := apierrors.IsNotFound(err)
	if err != nil && !missingTLS {
		return fmt.Errorf("get serving Secret: %w", err)
	}
	currentLeafValid := !missingTLS && servingCertificateValid(
		tlsSecret.Data[corev1.TLSCertKey], tlsSecret.Data[corev1.TLSPrivateKeyKey], caCert, cfg)
	rotateCA := cfg.ForceCARotation || time.Now().Add(cfg.LeafValidity+cfg.RenewBefore).After(caCert.NotAfter)
	rolledOut := false
	if rotateCA {
		newCert, newSigner, certPEM, keyPEM, err := newCA(cfg.CAValidity)
		if err != nil {
			return err
		}
		stagedBundle := append(append([]byte{}, caSecret.Data["ca.crt"]...), certPEM...)
		if err := setCABundle(ctx, c, cfg.WebhookConfigName, stagedBundle); err != nil {
			return fmt.Errorf("stage CA trust: %w", err)
		}
		caData := map[string][]byte{"ca.crt": certPEM, "ca.key": keyPEM}
		if err := updateSecret(ctx, c, caKey, corev1.SecretTypeOpaque, caData); err != nil {
			return fmt.Errorf("store new CA: %w", err)
		}
		caCert, caSigner = newCert, newSigner
		caSecret.Data["ca.crt"] = certPEM
		currentLeafValid = false
	}
	if cfg.ForceRenew || !currentLeafValid || rotateCA {
		certPEM, keyPEM, err := newServingCertificate(caCert, caSigner, cfg)
		if err != nil {
			return err
		}
		if err := updateSecret(ctx, c, tlsKey, corev1.SecretTypeTLS, map[string][]byte{
			"ca.crt": caSecret.Data["ca.crt"], corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM,
		}); err != nil {
			return fmt.Errorf("store serving certificate: %w", err)
		}
		if err := rollout(ctx, c, cfg.Namespace, cfg.DeploymentName); err != nil {
			return fmt.Errorf("wait for serving certificate rollout: %w", err)
		}
		rolledOut = true
	}
	// Also complete an interrupted rotation that left both roots trusted.
	var webhooks admissionregistrationv1.ValidatingWebhookConfiguration
	if err := c.Get(ctx, types.NamespacedName{Name: cfg.WebhookConfigName}, &webhooks); err != nil {
		return fmt.Errorf("read CA trust: %w", err)
	}
	for _, hook := range webhooks.Webhooks {
		if !bytes.Equal(hook.ClientConfig.CABundle, caSecret.Data["ca.crt"]) && !rolledOut {
			if err := rollout(ctx, c, cfg.Namespace, cfg.DeploymentName); err != nil {
				return fmt.Errorf("wait for resumed CA rollout: %w", err)
			}
			break
		}
	}
	if err := setCABundle(ctx, c, cfg.WebhookConfigName, caSecret.Data["ca.crt"]); err != nil {
		return fmt.Errorf("finalize CA trust: %w", err)
	}
	return nil
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, crypto.Signer, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("CA certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	if !cert.IsCA {
		return nil, nil, fmt.Errorf("certificate is not a CA")
	}
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("CA key is not PEM")
	}
	var key any
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	}
	if err != nil {
		return nil, nil, err
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, nil, fmt.Errorf("CA key does not support signing")
	}
	publicKey, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil || !bytes.Equal(publicKey, cert.RawSubjectPublicKeyInfo) {
		return nil, nil, fmt.Errorf("CA key does not match certificate")
	}
	return cert, signer, nil
}

func servingCertificateValid(certPEM, keyPEM []byte, ca *x509.Certificate, cfg Config) bool {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return false
	}
	now := time.Now()
	if leaf.CheckSignatureFrom(ca) != nil || now.Before(leaf.NotBefore) || now.Add(cfg.RenewBefore).After(leaf.NotAfter) {
		return false
	}
	return leaf.VerifyHostname(cfg.ServiceName+"."+cfg.Namespace+".svc") == nil
}

func newCA(validity time.Duration) (*x509.Certificate, crypto.Signer, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "Addon webhook CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(validity), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return cert, key, certPEM, keyPEM, nil
}

func newServingCertificate(ca *x509.Certificate, caKey crypto.Signer, cfg Config) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	serviceDNS := cfg.ServiceName + "." + cfg.Namespace
	dns := []string{cfg.ServiceName, serviceDNS, serviceDNS + ".svc", serviceDNS + ".svc.cluster.local"}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: dns[2]}, DNSNames: dns,
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(cfg.LeafValidity),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, key.Public(), caKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func updateSecret(
	ctx context.Context, c client.Client, key types.NamespacedName,
	secretType corev1.SecretType, data map[string][]byte,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var secret corev1.Secret
		err := c.Get(ctx, key, &secret)
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
				Type:       secretType, Data: data,
			})
		}
		if err != nil {
			return err
		}
		secret.Type = secretType
		secret.Data = data
		return c.Update(ctx, &secret)
	})
}

func setCABundle(ctx context.Context, c client.Client, name string, bundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var config admissionregistrationv1.ValidatingWebhookConfiguration
		if err := c.Get(ctx, types.NamespacedName{Name: name}, &config); err != nil {
			return err
		}
		for i := range config.Webhooks {
			config.Webhooks[i].ClientConfig.CABundle = bundle
		}
		return c.Update(ctx, &config)
	})
}

func rollout(ctx context.Context, c client.Client, namespace, name string) error {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var deployment appsv1.Deployment
		if err := c.Get(ctx, key, &deployment); err != nil {
			return err
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		deployment.Spec.Template.Annotations["multicluster.x-k8s.io/certificate-restarted-at"] =
			time.Now().UTC().Format(time.RFC3339Nano)
		return c.Update(ctx, &deployment)
	}); err != nil {
		return err
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var deployment appsv1.Deployment
		if err := c.Get(ctx, key, &deployment); err != nil {
			return err
		}
		want := int32(1)
		if deployment.Spec.Replicas != nil {
			want = *deployment.Spec.Replicas
		}
		status := deployment.Status
		if status.ObservedGeneration >= deployment.Generation && status.UpdatedReplicas == want &&
			status.ReadyReplicas == want && status.AvailableReplicas == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
