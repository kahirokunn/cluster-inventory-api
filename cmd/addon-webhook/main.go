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
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	api "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/cluster-inventory-api/pkg/addonwebhook"
)

func main() {
	var certDir string
	var port, probePort int
	flag.StringVar(&certDir, "cert-dir", "/tls", "directory containing tls.crt and tls.key")
	flag.IntVar(&port, "port", 9443, "TLS admission webhook port")
	flag.IntVar(&probePort, "probe-port", 8081, "HTTP health probe port")
	flag.Parse()

	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		log.Fatal(err)
	}
	reader, err := client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		log.Fatal(err)
	}
	handlers := addonwebhook.Handlers{Reader: reader}
	server := webhook.NewServer(webhook.Options{Port: port, CertDir: certDir})
	server.Register("/validate-addon-update", &admission.Webhook{
		Handler: admission.HandlerFunc(handlers.ValidateAddonUpdate),
	})
	server.Register("/validate-addonclass-delete", &admission.Webhook{
		Handler: admission.HandlerFunc(handlers.ValidateAddonClassDelete),
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	probes := &http.Server{Addr: fmtPort(probePort), ReadHeaderTimeout: 5 * time.Second}
	probeMux := http.NewServeMux()
	probeMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	probeMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := server.StartedChecker()(r); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	probes.Handler = probeMux
	go func() {
		if err := probes.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("probe server: %v", err)
			stop()
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = probes.Shutdown(shutdownCtx)
	}()
	if err := server.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

func fmtPort(port int) string {
	return ":" + strconv.Itoa(port)
}
