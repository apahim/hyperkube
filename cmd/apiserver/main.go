/*
Copyright 2026.

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
	"flag"
	"log/slog"
	"net/http"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gcp-hcp/gcp-hcp-backend/api/openapi"
	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
	"github.com/gcp-hcp/gcp-hcp-backend/internal/apiserver"
	"github.com/gcp-hcp/gcp-hcp-backend/internal/cedar"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(hcpv1alpha1.AddToScheme(scheme))
}

func main() {
	var addr string
	var cedarPolicyNamespace string
	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&cedarPolicyNamespace, "cedar-policy-namespace", "cedar-policies", "Namespace for Cedar policy ConfigMaps")
	flag.Parse()

	cfg := ctrl.GetConfigOrDie()

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		slog.Error("Failed to create Kubernetes client", "error", err)
		os.Exit(1)
	}

	cedarStore, err := cedar.NewStore(k8sClient, cedarPolicyNamespace)
	if err != nil {
		slog.Error("Failed to initialize Cedar store", "error", err)
		os.Exit(1)
	}

	jwtValidator := cedar.NewJWTValidator()

	validationMw, err := apiserver.NewValidationMiddleware(openapi.Spec)
	if err != nil {
		slog.Error("Failed to initialize validation middleware", "error", err)
		os.Exit(1)
	}

	authzMw := cedar.NewAuthzMiddleware(cedarStore, jwtValidator)

	h := &apiserver.Handler{Client: k8sClient}
	authzH := &cedar.AuthzHandler{Store: cedarStore}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1alpha1/namespaces/{namespace}/managedhostedclusters", h.List)
	mux.HandleFunc("POST /v1alpha1/namespaces/{namespace}/managedhostedclusters", h.Create)
	mux.HandleFunc("GET /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}", h.Get)
	mux.HandleFunc("PUT /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}", h.Update)
	mux.HandleFunc("DELETE /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}", h.Delete)

	mux.HandleFunc("GET /authz/templates", authzH.ListTemplates)
	mux.HandleFunc("GET /authz/templates/{name}", authzH.GetTemplate)
	mux.HandleFunc("GET /authz/namespaces/{namespace}/attachments", authzH.ListAttachments)
	mux.HandleFunc("POST /authz/namespaces/{namespace}/attachments", authzH.CreateAttachment)
	mux.HandleFunc("DELETE /authz/namespaces/{namespace}/attachments/{id}", authzH.DeleteAttachment)

	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openapi.Spec)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	handler := apiserver.RequestLogging(validationMw.Wrap(authzMw.Wrap(mux)))

	slog.Info("Starting API server", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
