package rooms

import (
	"context"
	"strconv"
	"time"

	"0xbot.org/api/internal/data"
)

// Administrator authority can only be attached by a trusted session boundary,
// after checking admin audience, allowlist and recent MFA. It is not an OAuth scope.
type administratorKey struct{}

func WithAdministrator(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, administratorKey{}, userID)
}
func administrator(ctx context.Context) string {
	v, _ := ctx.Value(administratorKey{}).(string)
	return v
}

type GuestPlan struct {
	AgentID         string `json:"agent_id,omitempty"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	SystemPrompt    string `json:"system_prompt"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

// StudioPlan is an immutable, private planning snapshot, NOT an execution grant.
// Credentials, arbitrary URLs and executable commands are deliberately unsupported.
type StudioPlan struct {
	Version        int         `json:"version"`
	CommonPrompt   string      `json:"common_prompt"`
	BudgetMicroUSD string      `json:"budget_micro_usd"`
	Guests         []GuestPlan `json:"guests"`
}
type StudioCreateInput struct {
	CreateInput
	Plan StudioPlan `json:"plan"`
}
type StudioDetail struct {
	Snapshot        Snapshot    `json:"snapshot"`
	Plan            *StudioPlan `json:"plan"`
	ExecutionStatus string      `json:"execution_status"`
}
type StudioReader interface {
	StudioPlan(context.Context, string) (*StudioPlan, error)
}

func (p StudioPlan) Validate() error {
	budget, e := strconv.ParseUint(p.BudgetMicroUSD, 10, 64)
	if e != nil || budget == 0 || budget > 1_000_000_000 || strconv.FormatUint(budget, 10) != p.BudgetMicroUSD || p.Version != 1 || !Text(p.CommonPrompt, 8000) || len(p.Guests) < 1 || len(p.Guests) > 5 {
		return data.ErrInput
	}
	for _, g := range p.Guests {
		if g.AgentID != "" || !Text(g.Name, 60) || !Text(g.Role, 200) || !Text(g.Provider, 100) || !Text(g.Model, 100) || !Text(g.SystemPrompt, 8000) || g.MaxOutputTokens < 1 || g.MaxOutputTokens > 4096 {
			return data.ErrInput
		}
	}
	return nil
}
func (s *Service) CreateStudio(ctx context.Context, in StudioCreateInput) (Result, error) {
	if administrator(ctx) == "" {
		return Result{}, ErrPermission
	}
	if e := in.Plan.Validate(); e != nil {
		return Result{}, e
	}
	return s.mutate(ctx, Command{Action: "create", Topic: in.Topic, Name: in.Name, MaxRounds: in.MaxRounds, IdempotencyKey: in.IdempotencyKey, Plan: &in.Plan})
}
func (s *Service) Studio(ctx context.Context, in RoomInput) (StudioDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if administrator(ctx) == "" {
		return StudioDetail{}, ErrPermission
	}
	r, e := s.Get(ctx, in)
	if e != nil {
		return StudioDetail{}, e
	}
	p, ok := s.Provider.(StudioReader)
	if !ok {
		return StudioDetail{}, data.ErrUnavailable
	}
	plan, e := p.StudioPlan(ctx, in.RoomID)
	return StudioDetail{Snapshot: r.Data, Plan: plan, ExecutionStatus: "external_runner_required"}, e
}
