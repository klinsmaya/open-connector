package store

import (
	"context"
	"errors"
	"time"
)

type Account struct {
	ID       string         `json:"id"`
	UserID   string         `json:"user_id"`
	ConfigID string         `json:"auth_config_id"`
	Config   AccountConfig  `json:"auth_config"`
	Toolkit  AccountToolkit `json:"toolkit"`
	Status   string         `json:"status"`
	Created  time.Time      `json:"created_at"`
	Updated  time.Time      `json:"updated_at"`
}
type AccountConfig struct {
	ID         string `json:"id"`
	AuthScheme string `json:"auth_scheme"`
	Disabled   bool   `json:"is_disabled"`
	Managed    bool   `json:"is_composio_managed"`
}
type AccountToolkit struct {
	Slug string `json:"slug"`
}

func (s *Store) Accounts(ctx context.Context, project string) ([]Account, error) {
	rows, err := s.Pool.Query(ctx, `SELECT c.id,c.subject_id,c.auth_config_id,c.toolkit,ac.auth_type,NOT ac.enabled,
 CASE WHEN c.state='ACTIVE' AND ac.enabled AND u.enabled AND c.toolkit=ac.toolkit AND c.runtime_id=ac.runtime_id THEN 'ACTIVE' ELSE 'INACTIVE' END,c.created_at,c.updated_at
 FROM connection c JOIN auth_config ac ON ac.project_id=c.project_id AND ac.id=c.auth_config_id JOIN subject u ON u.project_id=c.project_id AND u.id=c.subject_id WHERE c.project_id=$1 AND c.state<>'DELETED'
 UNION ALL
 SELECT t.id,t.subject_id,t.auth_config_id,ac.toolkit,ac.auth_type,NOT ac.enabled,
 CASE WHEN t.phase='EXPIRED' OR t.expires_at<=now() THEN 'EXPIRED' WHEN t.phase='FAILED' THEN 'FAILED' WHEN t.phase IN ('PENDING','CREATING','VERIFYING') AND ac.enabled AND u.enabled THEN 'INITIATED' ELSE 'INACTIVE' END,t.created_at,t.updated_at
 FROM connect_transaction t JOIN auth_config ac ON ac.project_id=t.project_id AND ac.id=t.auth_config_id JOIN subject u ON u.project_id=t.project_id AND u.id=t.subject_id WHERE t.project_id=$1 AND NOT EXISTS(SELECT 1 FROM connection c WHERE c.project_id=t.project_id AND c.id=t.id)
 ORDER BY 1 LIMIT 10001`, project)
	if err != nil {
		return nil, errors.New("accounts unavailable")
	}
	defer rows.Close()
	out := make([]Account, 0)
	for rows.Next() {
		var a Account
		if rows.Scan(&a.ID, &a.UserID, &a.ConfigID, &a.Toolkit.Slug, &a.Config.AuthScheme, &a.Config.Disabled, &a.Status, &a.Created, &a.Updated) != nil {
			return nil, errors.New("accounts unavailable")
		}
		a.Config.ID = a.ConfigID
		out = append(out, a)
	}
	if rows.Err() != nil || len(out) > 10000 {
		return nil, errors.New("accounts unavailable")
	}
	return out, nil
}
