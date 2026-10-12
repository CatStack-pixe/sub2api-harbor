//go:build unit

package admin

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorBindingRejectsUnimplementedProviders(t *testing.T) {
	for _, provider := range []string{"cline", "command_code"} {
		require.Error(t, binding.Validator.ValidateStruct(channelMonitorCreateRequest{
			Name: "provider-contract", Provider: provider, IntervalSeconds: 30,
		}))
		require.Error(t, binding.Validator.ValidateStruct(channelMonitorUpdateRequest{Provider: &provider}))
	}
	for _, provider := range []string{"openai", "grok", "zhipu", "tierflow", "opencode_go"} {
		require.NoError(t, binding.Validator.ValidateStruct(channelMonitorCreateRequest{
			Name: "provider-contract", Provider: provider, IntervalSeconds: 30,
		}))
		require.NoError(t, binding.Validator.ValidateStruct(channelMonitorUpdateRequest{Provider: &provider}))
	}
}
