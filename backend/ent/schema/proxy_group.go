package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// ProxyGroup holds the schema definition for independently managed proxy groups.
type ProxyGroup struct {
	ent.Schema
}

func (ProxyGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "proxy_groups"},
	}
}

func (ProxyGroup) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (ProxyGroup) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty(),
	}
}

func (ProxyGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("proxies", Proxy.Type).
			Ref("proxy_group"),
	}
}
