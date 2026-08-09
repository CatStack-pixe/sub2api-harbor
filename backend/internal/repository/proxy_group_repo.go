package repository

import (
	"context"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/ent/proxygroup"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type proxyGroupRepository struct {
	client *dbent.Client
}

func NewProxyGroupRepository(client *dbent.Client) service.ProxyGroupRepository {
	return &proxyGroupRepository{client: client}
}

func (r *proxyGroupRepository) List(ctx context.Context) ([]service.ProxyGroup, error) {
	groups, err := r.client.ProxyGroup.Query().
		Order(dbent.Asc(proxygroup.FieldName), dbent.Asc(proxygroup.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	counts, err := r.loadCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.ProxyGroup, 0, len(groups))
	for _, group := range groups {
		item := proxyGroupEntityToService(group)
		if count, ok := counts[group.ID]; ok {
			item.TotalCount = count.total
			item.ActiveCount = count.active
			item.InactiveCount = count.inactive
		}
		out = append(out, *item)
	}
	return out, nil
}

func (r *proxyGroupRepository) GetByID(ctx context.Context, id int64) (*service.ProxyGroup, error) {
	group, err := r.client.ProxyGroup.Get(ctx, id)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	return proxyGroupEntityToService(group), nil
}

func (r *proxyGroupRepository) GetByName(ctx context.Context, name string) (*service.ProxyGroup, error) {
	group, err := r.client.ProxyGroup.Query().
		Where(proxygroup.NameEqualFold(name)).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return proxyGroupEntityToService(group), nil
}

func (r *proxyGroupRepository) Create(ctx context.Context, group *service.ProxyGroup) error {
	created, err := r.client.ProxyGroup.Create().
		SetName(group.Name).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrProxyGroupExists)
	}
	applyProxyGroupEntity(group, created)
	return nil
}

func (r *proxyGroupRepository) Update(ctx context.Context, group *service.ProxyGroup) error {
	updated, err := r.client.ProxyGroup.UpdateOneID(group.ID).
		SetName(group.Name).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrProxyGroupNotFound, service.ErrProxyGroupExists)
	}
	applyProxyGroupEntity(group, updated)
	return nil
}

func (r *proxyGroupRepository) DeleteAndUnassign(ctx context.Context, id int64) error {
	client, tx, err := r.transactionClient(ctx)
	if err != nil {
		return err
	}
	if tx != nil {
		defer func() { _ = tx.Rollback() }()
	}

	if _, err := client.ProxyGroup.Get(ctx, id); err != nil {
		return translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	if _, err := client.Proxy.Update().
		Where(proxy.ProxyGroupIDEQ(id)).
		ClearProxyGroupID().
		SetUpdatedAt(time.Now()).
		Save(ctx); err != nil {
		return err
	}
	if err := client.ProxyGroup.DeleteOneID(id).Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
	}
	if tx != nil {
		return tx.Commit()
	}
	return nil
}

func (r *proxyGroupRepository) BatchAssign(ctx context.Context, proxyIDs []int64, proxyGroupID *int64) (int64, error) {
	if len(proxyIDs) == 0 {
		return 0, nil
	}
	client, tx, err := r.transactionClient(ctx)
	if err != nil {
		return 0, err
	}
	if tx != nil {
		defer func() { _ = tx.Rollback() }()
	}

	if proxyGroupID != nil {
		if _, err := client.ProxyGroup.Get(ctx, *proxyGroupID); err != nil {
			return 0, translatePersistenceError(err, service.ErrProxyGroupNotFound, nil)
		}
	}
	found, err := client.Proxy.Query().
		Where(proxy.IDIn(proxyIDs...)).
		Select(proxy.FieldID).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(found) != len(proxyIDs) {
		return 0, service.ErrProxyNotFound
	}

	update := client.Proxy.Update().
		Where(proxy.IDIn(proxyIDs...)).
		SetUpdatedAt(time.Now())
	if proxyGroupID == nil {
		update.ClearProxyGroupID()
	} else {
		update.SetProxyGroupID(*proxyGroupID)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return 0, err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
	}
	return int64(updated), nil
}

func (r *proxyGroupRepository) transactionClient(ctx context.Context) (*dbent.Client, *dbent.Tx, error) {
	client := clientFromContext(ctx, r.client)
	tx, err := client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return client, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return tx.Client(), tx, nil
}

type proxyGroupCounts struct {
	total    int64
	active   int64
	inactive int64
}

func (r *proxyGroupRepository) loadCounts(ctx context.Context) (map[int64]proxyGroupCounts, error) {
	rows, err := r.client.QueryContext(ctx, `
		SELECT proxy_group_id,
		       COUNT(*) AS total_count,
		       COUNT(*) FILTER (WHERE status = 'active') AS active_count,
		       COUNT(*) FILTER (WHERE status = 'inactive') AS inactive_count
		FROM proxies
		WHERE proxy_group_id IS NOT NULL
		  AND deleted_at IS NULL
		GROUP BY proxy_group_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make(map[int64]proxyGroupCounts)
	for rows.Next() {
		var id int64
		var count proxyGroupCounts
		if err := rows.Scan(&id, &count.total, &count.active, &count.inactive); err != nil {
			return nil, err
		}
		out[id] = count
	}
	return out, rows.Err()
}

func proxyGroupEntityToService(group *dbent.ProxyGroup) *service.ProxyGroup {
	if group == nil {
		return nil
	}
	return &service.ProxyGroup{
		ID:        group.ID,
		Name:      group.Name,
		CreatedAt: group.CreatedAt,
		UpdatedAt: group.UpdatedAt,
	}
}

func applyProxyGroupEntity(dst *service.ProxyGroup, src *dbent.ProxyGroup) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.Name = src.Name
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
