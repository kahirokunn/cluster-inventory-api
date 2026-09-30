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

package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"sigs.k8s.io/cluster-inventory-api/pkg/addoncert"
)

func main() {
	var cfg addoncert.Config
	flag.StringVar(&cfg.Namespace, "namespace", "", "namespace of the webhook")
	flag.StringVar(&cfg.ServiceName, "service", "", "webhook Service name")
	flag.StringVar(&cfg.CASecretName, "ca-secret", "", "CA Secret name")
	flag.StringVar(&cfg.TLSSecretName, "tls-secret", "", "serving certificate Secret name")
	flag.StringVar(&cfg.WebhookConfigName, "webhook-config", "", "ValidatingWebhookConfiguration name")
	flag.StringVar(&cfg.DeploymentName, "deployment", "", "webhook Deployment name")
	flag.DurationVar(&cfg.CAValidity, "ca-validity", 10*365*24*time.Hour, "new CA validity")
	flag.DurationVar(&cfg.LeafValidity, "leaf-validity", 365*24*time.Hour, "serving certificate validity")
	flag.DurationVar(&cfg.RenewBefore, "renew-before", 90*24*time.Hour, "renew before expiration")
	flag.BoolVar(&cfg.ForceRenew, "force-renew", false, "renew the serving certificate now")
	flag.BoolVar(&cfg.ForceCARotation, "rotate-ca", false, "rotate the CA now")
	flag.Parse()
	missingName := cfg.Namespace == "" || cfg.ServiceName == "" || cfg.CASecretName == "" ||
		cfg.TLSSecretName == "" || cfg.WebhookConfigName == "" || cfg.DeploymentName == ""
	invalidDuration := cfg.CAValidity <= cfg.LeafValidity+cfg.RenewBefore ||
		cfg.LeafValidity <= cfg.RenewBefore || cfg.RenewBefore <= 0
	if missingName || invalidDuration {
		log.Fatal("resource names and valid certificate durations are required")
	}
	scheme := runtime.NewScheme()
	addToScheme := []func(*runtime.Scheme) error{
		corev1.AddToScheme, appsv1.AddToScheme, admissionregistrationv1.AddToScheme,
	}
	for _, add := range addToScheme {
		if err := add(scheme); err != nil {
			log.Fatal(err)
		}
	}
	c, err := client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := addoncert.Rotate(ctx, c, cfg); err != nil {
		log.Fatal(err)
	}
	log.Print("certificate reconciliation complete")
}
