package routing

import "context"

func (s Store) Enabled(ctx context.Context, id string) (bool, error) {
	var enabled bool
	e := s.DB.QueryRow(ctx, "SELECT enabled FROM provider_settings WHERE id=$1", id).Scan(&enabled)
	return enabled, e
}
