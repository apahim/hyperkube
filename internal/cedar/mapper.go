package cedar

import (
	"net/http"
	"regexp"
)

const ResourceTypeManagedHostedCluster = "ManagedHostedCluster"

type CedarMapping struct {
	Action       string
	ResourceType string
	ResourceID   string
	ProjectID    string
}

type routeEntry struct {
	pattern      *regexp.Regexp
	methodAction map[string]string
	hasResource  bool
}

var routes = []routeEntry{
	{
		pattern:      regexp.MustCompile(`^/v1alpha1/namespaces/([^/]+)/managedhostedclusters/([^/]+)/kubeconfig$`),
		methodAction: map[string]string{"GET": "GetKubeConfig"},
		hasResource:  true,
	},
	{
		pattern:      regexp.MustCompile(`^/v1alpha1/namespaces/([^/]+)/managedhostedclusters/([^/]+)$`),
		methodAction: map[string]string{"GET": "GetManagedHostedCluster", "PUT": "UpdateManagedHostedCluster", "DELETE": "DeleteManagedHostedCluster"},
		hasResource:  true,
	},
	{
		pattern:      regexp.MustCompile(`^/v1alpha1/namespaces/([^/]+)/managedhostedclusters$`),
		methodAction: map[string]string{"GET": "ListManagedHostedClusters", "POST": "CreateManagedHostedCluster"},
		hasResource:  false,
	},
	{
		pattern:      regexp.MustCompile(`^/authz/namespaces/([^/]+)/`),
		methodAction: map[string]string{"GET": "ManagePolicies", "POST": "ManagePolicies", "PUT": "ManagePolicies", "DELETE": "ManagePolicies"},
		hasResource:  false,
	},
}

func MapRequest(r *http.Request) *CedarMapping {
	for _, route := range routes {
		matches := route.pattern.FindStringSubmatch(r.URL.Path)
		if matches == nil {
			continue
		}
		action, ok := route.methodAction[r.Method]
		if !ok {
			continue
		}
		m := &CedarMapping{
			Action:    action,
			ProjectID: matches[1],
		}
		if route.hasResource {
			m.ResourceType = ResourceTypeManagedHostedCluster
			m.ResourceID = matches[2]
		} else {
			m.ResourceType = "Project"
			m.ResourceID = matches[1]
		}
		return m
	}
	return nil
}
