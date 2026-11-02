package padinrooms

import (
	"0xbot.org/api/internal/rooms"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// StudioPlan is only exposed through the administrator use case, never public Get.
func (p Provider) StudioPlan(ctx context.Context, id string) (*rooms.StudioPlan, error) {
	var raw []byte
	err := p.DB.QueryRow(ctx, "SELECT plan FROM padin_room_plans WHERE room_id=$1", id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var plan rooms.StudioPlan
	err = json.Unmarshal(raw, &plan)
	return &plan, err
}
