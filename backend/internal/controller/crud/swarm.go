package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

func (c *controller) CreateSwarm(ctx context.Context, req entity.CreateSwarmRequest) (*entity.CreateSwarmResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()

	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.LeaderWorkspaceID == 0 {
		return nil, fmt.Errorf("leaderWorkspaceId is required")
	}
	if len(req.MemberWorkspaceIDs) == 0 {
		return nil, fmt.Errorf("memberWorkspaceIds must include at least the leader")
	}

	isLeaderMember := false
	for _, id := range req.MemberWorkspaceIDs {
		if _, err := c.repository.GetWorkspace(ctx, id, uid); err != nil {
			return nil, fmt.Errorf("member workspace %d: %w", id, err)
		}
		if id == req.LeaderWorkspaceID {
			isLeaderMember = true
		}
	}
	if !isLeaderMember {
		return nil, fmt.Errorf("leaderWorkspaceId must be one of memberWorkspaceIds")
	}

	membersJSON, err := json.Marshal(req.MemberWorkspaceIDs)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	m := model.Swarm{
		ID:                 c.idgen.NextID(),
		CreatedAt:          now,
		UpdatedAt:          now,
		WorkspaceID:        req.WorkspaceID,
		Name:               req.Name,
		LeaderWorkspaceID:  req.LeaderWorkspaceID,
		MemberWorkspaceIDs: datatypes.JSON(membersJSON),
	}

	created, err := c.repository.CreateSwarm(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("create swarm: %w", err)
	}

	return &entity.CreateSwarmResponse{Swarm: fromModelSwarmToEntity(created)}, nil
}

func fromModelSwarmToEntity(m model.Swarm) entity.Swarm {
	var members []int64
	if len(m.MemberWorkspaceIDs) > 0 {
		_ = json.Unmarshal(m.MemberWorkspaceIDs, &members)
	}
	return entity.Swarm{
		ID:                 m.ID,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
		WorkspaceID:        m.WorkspaceID,
		Name:               m.Name,
		LeaderWorkspaceID:  m.LeaderWorkspaceID,
		MemberWorkspaceIDs: members,
	}
}
