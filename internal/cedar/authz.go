package cedar

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	cedarlib "github.com/cedar-policy/cedar-go"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
	"github.com/gcp-hcp/gcp-hcp-backend/internal/desires"
	"github.com/gcp-hcp/gcp-hcp-backend/internal/placement"
)

type AuthzMiddleware struct {
	store     *Store
	validator *JWTValidator
	placement *placement.Client
	statusDB  func(string) (*desires.DBClient, error)
}

func NewAuthzMiddleware(store *Store, validator *JWTValidator, placementClient *placement.Client, statusDB func(string) (*desires.DBClient, error)) *AuthzMiddleware {
	return &AuthzMiddleware{
		store:     store,
		validator: validator,
		placement: placementClient,
		statusDB:  statusDB,
	}
}

func (a *AuthzMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mapping := MapRequest(r)
		if mapping == nil {
			next.ServeHTTP(w, r)
			return
		}

		identity, err := a.validator.ValidateRequest(r)
		if err != nil {
			writeAuthzError(w, http.StatusUnauthorized, "Authentication failed: "+err.Error())
			return
		}

		resolved, err := a.store.ResolvePolicies(r.Context(), mapping.ProjectID)
		if err != nil {
			slog.Error("Failed to resolve policies", "error", err, "project", mapping.ProjectID)
			writeAuthzError(w, http.StatusInternalServerError, "Policy resolution error")
			return
		}
		if resolved == "" {
			slog.Info("No policies configured", "project", mapping.ProjectID, "user", identity.UserID)
			writeAuthzError(w, http.StatusForbidden, "Access denied: no policies configured")
			return
		}

		policies, err := cedarlib.NewPolicyListFromBytes("resolved", []byte(resolved))
		if err != nil {
			slog.Error("Failed to parse resolved policies", "error", err, "project", mapping.ProjectID)
			writeAuthzError(w, http.StatusInternalServerError, "Policy parse error")
			return
		}
		ps := cedarlib.NewPolicySet()
		for i, p := range policies {
			ps.Add(cedarlib.PolicyID(fmt.Sprintf("policy-%d", i)), p)
		}

		var attrs *ResourceAttributes
		if mapping.ResourceType == ResourceTypeManagedHostedCluster && mapping.ResourceID != "" {
			attrs = a.fetchResourceLabels(r, mapping.ProjectID, mapping.ResourceID)
		}

		entities, err := BuildEntityMap(identity.UserID, mapping.ProjectID, identity.Email, mapping.ResourceType, mapping.ResourceID, attrs)
		if err != nil {
			slog.Error("Failed to build entity map", "error", err)
			writeAuthzError(w, http.StatusInternalServerError, "Entity build error")
			return
		}

		resourceType := "HCP::" + mapping.ResourceType
		req := cedarlib.Request{
			Principal: cedarlib.NewEntityUID("HCP::User", cedarlib.String(identity.UserID)),
			Action:    cedarlib.NewEntityUID("HCP::Action", cedarlib.String(mapping.Action)),
			Resource:  cedarlib.NewEntityUID(cedarlib.EntityType(resourceType), cedarlib.String(mapping.ResourceID)),
			Context:   cedarlib.NewRecord(cedarlib.RecordMap{}),
		}

		decision, diag := cedarlib.Authorize(ps, entities, req)
		if decision != cedarlib.Allow {
			slog.Info("Access denied",
				"user", identity.UserID,
				"action", mapping.Action,
				"resource", mapping.ResourceType+"/"+mapping.ResourceID,
				"project", mapping.ProjectID,
				"errors", len(diag.Errors),
			)
			writeAuthzError(w, http.StatusForbidden, "Access denied")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *AuthzMiddleware) fetchResourceLabels(r *http.Request, namespace, name string) *ResourceAttributes {
	if a.placement == nil || a.statusDB == nil {
		return nil
	}

	mc, err := a.placement.GetClusterMapping(r.Context(), namespace, name)
	if err != nil {
		return nil
	}

	docID := desires.NewDocumentID(desires.TaskKey,
		"hcp.gcp.hypershift.openshift.com", "v1alpha1", "managedhostedclusters",
		namespace, name)

	db, err := a.statusDB(mc)
	if err != nil {
		return nil
	}

	rd, err := db.ReadDesires().Get(r.Context(), docID)
	if err != nil || rd.Status.KubeContent == nil {
		return nil
	}

	var cluster hcpv1alpha1.ManagedHostedCluster
	if err := json.Unmarshal(rd.Status.KubeContent.Raw, &cluster); err != nil {
		return nil
	}
	return &ResourceAttributes{Labels: cluster.Labels}
}
