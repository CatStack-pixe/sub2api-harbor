package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrProxyGroupNotFound = infraerrors.NotFound("PROXY_GROUP_NOT_FOUND", "proxy group not found")
	ErrProxyGroupExists   = infraerrors.Conflict("PROXY_GROUP_EXISTS", "proxy group name already exists")
)

type ProxyGroup struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	TotalCount    int64     `json:"total_count"`
	ActiveCount   int64     `json:"active_count"`
	InactiveCount int64     `json:"inactive_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ProxyListFilters struct {
	Protocol     string
	Status       string
	Search       string
	ProxyGroupID *int64
	Ungrouped    bool
}

// ProxyGroupAdminService keeps proxy-group operations separate from the broad
// AdminService contract so unrelated handlers and test doubles remain stable.
type ProxyGroupAdminService interface {
	ListProxyGroups(ctx context.Context) ([]ProxyGroup, error)
	CreateProxyGroup(ctx context.Context, name string) (*ProxyGroup, error)
	UpdateProxyGroup(ctx context.Context, id int64, name string) (*ProxyGroup, error)
	DeleteProxyGroup(ctx context.Context, id int64) error
	GetOrCreateProxyGroupByName(ctx context.Context, name string) (*ProxyGroup, error)
	BatchGroupProxies(ctx context.Context, ids []int64, proxyGroupID *int64) (int64, error)
}

type ProxyGroupRepository interface {
	List(ctx context.Context) ([]ProxyGroup, error)
	GetByID(ctx context.Context, id int64) (*ProxyGroup, error)
	GetByName(ctx context.Context, name string) (*ProxyGroup, error)
	Create(ctx context.Context, group *ProxyGroup) error
	Update(ctx context.Context, group *ProxyGroup) error
	DeleteAndUnassign(ctx context.Context, id int64) error
	BatchAssign(ctx context.Context, proxyIDs []int64, proxyGroupID *int64) (int64, error)
}
