package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func hydrateGroupGlobalPrompt(ctx context.Context, sqlq sqlExecutor, groups ...*service.Group) error {
	if sqlq == nil || len(groups) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(groups))
	seen := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if group == nil || group.ID <= 0 {
			continue
		}
		if _, ok := seen[group.ID]; ok {
			continue
		}
		seen[group.ID] = struct{}{}
		ids = append(ids, group.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := sqlq.QueryContext(ctx,
		`SELECT id, global_prompt_enabled, global_prompt FROM groups WHERE id = ANY($1)`,
		pq.Array(ids),
	)
	if err != nil {
		return fmt.Errorf("load group global prompts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[int64]*service.Group, len(ids))
	for _, group := range groups {
		if group != nil {
			byID[group.ID] = group
		}
	}
	for rows.Next() {
		var id int64
		var enabled bool
		var prompt string
		if err := rows.Scan(&id, &enabled, &prompt); err != nil {
			return fmt.Errorf("scan group global prompt: %w", err)
		}
		if err := service.ValidateGroupGlobalPrompt(prompt); err != nil {
			return err
		}
		if group := byID[id]; group != nil {
			group.GlobalPromptEnabled = enabled
			group.GlobalPrompt = prompt
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read group global prompts: %w", err)
	}
	return nil
}

func persistGroupGlobalPrompt(ctx context.Context, sqlq sqlExecutor, group *service.Group) error {
	if sqlq == nil || group == nil || group.ID <= 0 {
		return nil
	}
	if err := service.ValidateGroupGlobalPrompt(group.GlobalPrompt); err != nil {
		return err
	}
	_, err := sqlq.ExecContext(ctx,
		`UPDATE groups SET global_prompt_enabled = $1, global_prompt = $2 WHERE id = $3`,
		group.GlobalPromptEnabled, group.GlobalPrompt, group.ID,
	)
	if err != nil {
		return fmt.Errorf("persist group global prompt: %w", err)
	}
	return nil
}
