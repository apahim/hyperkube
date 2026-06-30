package cedar

import (
	"encoding/json"
	"fmt"

	cedarlib "github.com/cedar-policy/cedar-go"
)

type ResourceAttributes struct {
	Labels map[string]string
}

type entityJSON struct {
	UID     entityUIDJSON   `json:"uid"`
	Attrs   map[string]any  `json:"attrs"`
	Parents []entityUIDJSON `json:"parents"`
}

type entityUIDJSON struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func BuildEntityMap(userID, projectID, email, resourceType, resourceID string, attrs *ResourceAttributes) (cedarlib.EntityMap, error) {
	entities := []entityJSON{
		{
			UID:   entityUIDJSON{Type: "HCP::Project", ID: projectID},
			Attrs: map[string]any{},
		},
		{
			UID:     entityUIDJSON{Type: "HCP::User", ID: userID},
			Attrs:   map[string]any{"email": email},
			Parents: []entityUIDJSON{{Type: "HCP::Project", ID: projectID}},
		},
	}

	if resourceType == ResourceTypeManagedHostedCluster {
		clusterAttrs := map[string]any{}
		if attrs != nil && attrs.Labels != nil {
			labels := make(map[string]any, len(attrs.Labels))
			for k, v := range attrs.Labels {
				labels[k] = v
			}
			clusterAttrs["labels"] = labels
		} else {
			clusterAttrs["labels"] = map[string]any{}
		}
		entities = append(entities, entityJSON{
			UID:     entityUIDJSON{Type: "HCP::ManagedHostedCluster", ID: resourceID},
			Attrs:   clusterAttrs,
			Parents: []entityUIDJSON{{Type: "HCP::Project", ID: projectID}},
		})
	}

	data, err := json.Marshal(entities)
	if err != nil {
		return nil, fmt.Errorf("marshaling entities: %w", err)
	}

	var em cedarlib.EntityMap
	if err := json.Unmarshal(data, &em); err != nil {
		return nil, fmt.Errorf("unmarshaling entity map: %w", err)
	}
	return em, nil
}
