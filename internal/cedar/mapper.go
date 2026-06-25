package cedar

import (
	"net/http"
	"regexp"
)

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
		pattern:      regexp.MustCompile(`^/v1alpha1/namespaces/([^/]+)/managedhostedclusters/([^/]+)$`),
		methodAction: map[string]string{"GET": "ReadManagedHostedCluster", "PUT": "WriteManagedHostedCluster", "DELETE": "WriteManagedHostedCluster"},
		hasResource:  true,
	},
	{
		pattern:      regexp.MustCompile(`^/v1alpha1/namespaces/([^/]+)/managedhostedclusters$`),
		methodAction: map[string]string{"GET": "ReadManagedHostedCluster", "POST": "WriteManagedHostedCluster"},
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
			m.ResourceType = "ManagedHostedCluster"
			m.ResourceID = matches[2]
		} else {
			m.ResourceType = "Project"
			m.ResourceID = matches[1]
		}
		return m
	}
	return nil
}
