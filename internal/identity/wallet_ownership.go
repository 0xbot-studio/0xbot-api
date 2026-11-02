package identity

import "context"

// Every call resolves current verified identity ownership; workspace IDs are not wallet addresses.
func (s Store) OwnsWallet(ctx context.Context, workspace, address string) (bool, error) {
	var owns bool
	e := s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM identities i JOIN workspaces w ON w.owner_id=i.user_id WHERE w.id=$1 AND i.kind='wallet' AND i.subject=$2)", workspace, address).Scan(&owns)
	return owns, e
}
