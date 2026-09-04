// Package builtin provides built-in plugins for the CloudAI Fusion scheduler framework.
package builtin

import (
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

func init() {
	// Register NVLink-aware topology scoring plugin
	// This integrates the nvlink_placer framework into the main scheduler
	reg.MustRegister("nvlink-topology-scorer", func() (plugin.Plugin, error) {
		return NewNVLinkScorePlugin(), nil
	})
}
