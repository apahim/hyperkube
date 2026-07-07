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
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"cloud.google.com/go/firestore"

	"github.com/apahim/hyperkube/api/openapi"
	"github.com/apahim/hyperkube/internal/apiserver"
	"github.com/apahim/hyperkube/internal/cedar"
	"github.com/apahim/hyperkube/internal/desires"
	"github.com/apahim/hyperkube/internal/firestorecache"
	"github.com/apahim/hyperkube/internal/placement"
)

func main() {
	var addr string
	var gcpProject string
	var placementDatabase string
	var cedarDatabase string
	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&gcpProject, "gcp-project", "", "GCP project for Firestore databases")
	flag.StringVar(&placementDatabase, "placement-database", "placement", "Firestore database for placement")
	flag.StringVar(&cedarDatabase, "cedar-database", "cedar", "Firestore database for Cedar policies")
	flag.Parse()

	ctx := context.Background()

	if gcpProject == "" {
		slog.Error("--gcp-project is required")
		os.Exit(1)
	}

	// Placement Firestore client.
	placementFS, err := firestore.NewClientWithDatabase(ctx, gcpProject, placementDatabase)
	if err != nil {
		slog.Error("Failed to create placement Firestore client", "error", err)
		os.Exit(1)
	}
	defer placementFS.Close()
	placementClient := placement.NewClient(placementFS)

	// Per-management-cluster Firestore client cache.
	fsCache := firestorecache.NewCache(gcpProject)
	defer fsCache.Close()

	specsDBFactory := func(mc string) (*desires.DBClient, error) {
		dbID := fmt.Sprintf("mc-%s-specs", mc)
		c, err := fsCache.GetOrCreate(ctx, dbID)
		if err != nil {
			return nil, err
		}
		return desires.NewDBClient(c), nil
	}
	statusDBFactory := func(mc string) (*desires.DBClient, error) {
		dbID := fmt.Sprintf("mc-%s-status", mc)
		c, err := fsCache.GetOrCreate(ctx, dbID)
		if err != nil {
			return nil, err
		}
		return desires.NewDBClient(c), nil
	}

	slog.Info("Firestore initialized", "project", gcpProject, "placementDB", placementDatabase, "cedarDB", cedarDatabase)

	// Cedar Firestore client.
	cedarFS, err := firestore.NewClientWithDatabase(ctx, gcpProject, cedarDatabase)
	if err != nil {
		slog.Error("Failed to create Cedar Firestore client", "error", err)
		os.Exit(1)
	}
	defer cedarFS.Close()

	cedarStore, err := cedar.NewStore(cedarFS)
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

	authzMw := cedar.NewAuthzMiddleware(cedarStore, jwtValidator, placementClient, statusDBFactory)

	h := &apiserver.Handler{
		Placement: placementClient,
		SpecsDB:   specsDBFactory,
		StatusDB:  statusDBFactory,
	}
	authzH := &cedar.AuthzHandler{Store: cedarStore}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1alpha1/namespaces/{namespace}/managedhostedclusters", h.List)
	mux.HandleFunc("POST /v1alpha1/namespaces/{namespace}/managedhostedclusters", h.Create)
	mux.HandleFunc("GET /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}", h.Get)
	mux.HandleFunc("PUT /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}", h.Update)
	mux.HandleFunc("DELETE /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}", h.Delete)
	mux.HandleFunc("GET /v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}/kubeconfig", h.GetKubeConfig)

	mux.HandleFunc("GET /authz/templates", authzH.ListTemplates)
	mux.HandleFunc("GET /authz/templates/{name}", authzH.GetTemplate)
	mux.HandleFunc("GET /authz/namespaces/{namespace}/attachments", authzH.ListAttachments)
	mux.HandleFunc("POST /authz/namespaces/{namespace}/attachments", authzH.CreateAttachment)
	mux.HandleFunc("DELETE /authz/namespaces/{namespace}/attachments/{id}", authzH.DeleteAttachment)

	mux.HandleFunc("GET /authz/namespaces/{namespace}/roles", authzH.ListRoles)
	mux.HandleFunc("GET /authz/namespaces/{namespace}/roles/{name}", authzH.GetRole)

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
