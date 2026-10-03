package store

import (
	"context"
	"errors"
)

// CatalogEntry is a configured project capability, not the global provider list.
// Only a trusted provisioning/reconciliation path may assert capability facts.
type CatalogEntry struct {
	ID       string
	Toolkit  string
	Name     string
	AuthType string
	Enabled  bool
	Usage    int64
}

func (s *Store) Catalog(ctx context.Context, projectID string) ([]CatalogEntry, error) {
	rows, err := s.Pool.Query(ctx, `SELECT ac.id,ac.toolkit,ac.display_name,ac.auth_type,ac.enabled,
 (SELECT count(*) FROM execution e JOIN connection c ON c.project_id=e.project_id AND c.id=e.connection_id WHERE e.project_id=ac.project_id AND c.toolkit=ac.toolkit AND e.state='SUCCEEDED')
 FROM auth_config ac
 WHERE project_id=$1 AND cardinality(approved_actions)>0
 AND array_position(approved_actions,NULL) IS NULL
 AND NOT EXISTS (SELECT 1 FROM unnest(approved_actions) action WHERE btrim(action)='' OR strpos(action,'*')>0)
 AND capabilities @> '{"auth_configured":true,"runtime_verified":true}'::jsonb
 ORDER BY id LIMIT 10001`, projectID)
	if err != nil {
		return nil, errors.New("catalog unavailable")
	}
	defer rows.Close()
	out := make([]CatalogEntry, 0)
	for rows.Next() {
		var entry CatalogEntry
		if err = rows.Scan(&entry.ID, &entry.Toolkit, &entry.Name, &entry.AuthType, &entry.Enabled, &entry.Usage); err != nil {
			return nil, errors.New("catalog unavailable")
		}
		out = append(out, entry)
	}
	if rows.Err() != nil || len(out) > 10000 {
		return nil, errors.New("catalog unavailable")
	}
	return out, nil
}
