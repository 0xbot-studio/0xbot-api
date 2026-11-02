package identity

import (
	"0xbot.org/api/internal/platform/token"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrAuth = errors.New("authentication_required")
var ErrConflict = errors.New("identity_already_linked")

type User struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	WorkspaceID   string     `json:"workspace_id"`
	WorkspaceName string     `json:"workspace_name"`
	Identities    []Identity `json:"identities"`
	Admin         bool       `json:"admin"`
	MFA           bool       `json:"mfa"`
}
type Identity struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
}
type Session struct {
	User          User
	Hash          string
	Audience      string
	AdminVerified *time.Time
	CreatedAt     time.Time
}
type Challenge struct {
	ID, Address, Message, Origin string
	BindingUser                  *string
	Expires                      time.Time
}
type OAuthState struct {
	BrowserHash, Verifier, Origin string
	BindingUser                   *string
}
type Store struct{ DB *pgxpool.Pool }

func (s Store) SaveChallenge(ctx context.Context, c Challenge) error {
	_, e := s.DB.Exec(ctx, "INSERT INTO auth_challenges(id,address,message,origin,binding_user,expires_at) VALUES($1,$2,$3,$4,$5,$6)", c.ID, c.Address, c.Message, c.Origin, c.BindingUser, c.Expires)
	return e
}
func (s Store) Challenge(ctx context.Context, id string) (Challenge, error) {
	var c Challenge
	e := s.DB.QueryRow(ctx, "SELECT id,address,message,origin,binding_user,expires_at FROM auth_challenges WHERE id=$1 AND expires_at>now()", id).Scan(&c.ID, &c.Address, &c.Message, &c.Origin, &c.BindingUser, &c.Expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, ErrAuth
	}
	return c, e
}
func (s Store) ConsumeChallenge(ctx context.Context, id string) error {
	tag, e := s.DB.Exec(ctx, "DELETE FROM auth_challenges WHERE id=$1 AND expires_at>now()", id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrAuth
	}
	return nil
}
func (s Store) FindOrCreate(ctx context.Context, kind, subject, name string, binding *string) (string, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	// Serialize registration/linking for this external identity, including the absent-row case.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", kind+":"+subject); e != nil {
		return "", e
	}
	var id string
	e = tx.QueryRow(ctx, "SELECT user_id FROM identities WHERE kind=$1 AND subject=$2", kind, subject).Scan(&id)
	if e == nil {
		if binding != nil && id != *binding {
			return "", ErrConflict
		}
		return id, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return "", e
	}
	if binding != nil {
		id = *binding
	} else {
		id = token.New()
		if _, e = tx.Exec(ctx, "INSERT INTO users(id,display_name) VALUES($1,$2)", id, name); e != nil {
			return "", e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO workspaces(id,owner_id,name) VALUES($1,$2,$3)", token.New(), id, "Personal workspace"); e != nil {
			return "", e
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO identities(kind,subject,user_id) VALUES($1,$2,$3)", kind, subject, id); e != nil {
		return "", e
	}
	return id, tx.Commit(ctx)
}
func (s Store) CreateSession(ctx context.Context, user, audience string) (string, error) {
	raw := token.New()
	_, e := s.DB.Exec(ctx, "INSERT INTO sessions(token_hash,user_id,audience,expires_at) VALUES($1,$2,$3,now()+interval '7 days')", token.Hash(raw), user, audience)
	return raw, e
}
func (s Store) Session(ctx context.Context, raw string) (Session, error) {
	v := Session{Hash: token.Hash(raw)}
	e := s.DB.QueryRow(ctx, "SELECT u.id,u.display_name,w.id,w.name,s.audience,s.admin_verified_at,s.created_at FROM sessions s JOIN users u ON u.id=s.user_id JOIN workspaces w ON w.owner_id=u.id WHERE s.token_hash=$1 AND s.expires_at>now()", v.Hash).Scan(&v.User.ID, &v.User.Name, &v.User.WorkspaceID, &v.User.WorkspaceName, &v.Audience, &v.AdminVerified, &v.CreatedAt)
	if e != nil {
		return v, ErrAuth
	}
	rows, e := s.DB.Query(ctx, "SELECT kind,subject FROM identities WHERE user_id=$1 ORDER BY kind,subject", v.User.ID)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	v.User.Identities = []Identity{}
	for rows.Next() {
		var i Identity
		if e = rows.Scan(&i.Kind, &i.Subject); e != nil {
			return v, e
		}
		v.User.Identities = append(v.User.Identities, i)
	}
	return v, rows.Err()
}
func (s Store) Revoke(ctx context.Context, hash string) error {
	_, e := s.DB.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", hash)
	return e
}
func (s Store) MFA(ctx context.Context, hash string) error {
	_, e := s.DB.Exec(ctx, "UPDATE sessions SET admin_verified_at=now() WHERE token_hash=$1", hash)
	return e
}
func (s Store) SaveOAuth(ctx context.Context, state string, v OAuthState) error {
	_, e := s.DB.Exec(ctx, "INSERT INTO oauth_states(state_hash,browser_hash,verifier,origin,binding_user,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '5 minutes')", token.Hash(state), v.BrowserHash, v.Verifier, v.Origin, v.BindingUser)
	return e
}
func (s Store) ConsumeOAuth(ctx context.Context, state, browser string) (OAuthState, error) {
	var v OAuthState
	e := s.DB.QueryRow(ctx, "DELETE FROM oauth_states WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now() RETURNING browser_hash,verifier,origin,binding_user", token.Hash(state), token.Hash(browser)).Scan(&v.BrowserHash, &v.Verifier, &v.Origin, &v.BindingUser)
	if e != nil {
		return v, ErrAuth
	}
	return v, nil
}
func (s Store) Allow(ctx context.Context, key string, limit int, period time.Duration) (bool, error) {
	var count int
	e := s.DB.QueryRow(ctx, "INSERT INTO rate_windows(key,count,expires_at) VALUES($1,1,now()+$2*interval '1 second') ON CONFLICT(key) DO UPDATE SET count=CASE WHEN rate_windows.expires_at<=now() THEN 1 ELSE LEAST(rate_windows.count+1,$3+1) END,expires_at=CASE WHEN rate_windows.expires_at<=now() THEN EXCLUDED.expires_at ELSE rate_windows.expires_at END RETURNING count", key, int(period.Seconds()), limit).Scan(&count)
	return count <= limit, e
}
func (s Store) Cleanup(ctx context.Context) error {
	_, e := s.DB.Exec(ctx, "DELETE FROM auth_challenges WHERE expires_at<now(); DELETE FROM oauth_states WHERE expires_at<now(); DELETE FROM sessions WHERE expires_at<now(); DELETE FROM rate_windows WHERE expires_at<now()-interval '1 hour'")
	return e
}
