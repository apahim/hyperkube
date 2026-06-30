package cedar

import (
	"fmt"
	"log/slog"
	"net/http"

	cedarlib "github.com/cedar-policy/cedar-go"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
)

type AuthzMiddleware struct {
	store     *Store
	validator *JWTValidator
	k8sClient client.Client
}

func NewAuthzMiddleware(store *Store, validator *JWTValidator, k8sClient client.Client) *AuthzMiddleware {
	return &AuthzMiddleware{
		store:     store,
		validator: validator,
		k8sClient: k8sClient,
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
			var cluster hcpv1alpha1.ManagedHostedCluster
			if err := a.k8sClient.Get(r.Context(), client.ObjectKey{
				Namespace: mapping.ProjectID,
				Name:      mapping.ResourceID,
			}, &cluster); err == nil {
				attrs = &ResourceAttributes{Labels: cluster.Labels}
			}
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
