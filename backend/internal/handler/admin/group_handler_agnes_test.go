package admin

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestGroupRequestsAcceptAgnesPlatform(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		req := CreateGroupRequest{Name: "agnes-group", Platform: "agnes"}
		require.NoError(t, binding.Validator.ValidateStruct(req))
	})

	t.Run("update", func(t *testing.T) {
		req := UpdateGroupRequest{Platform: "agnes"}
		require.NoError(t, binding.Validator.ValidateStruct(req))
	})
}
