package privd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/version"
)

// RegisterSystem adds system.* methods: a liveness/version probe used by `yubeosctl doctor`.
func RegisterSystem(s *Server) {
	s.Register(Method{Name: "system.ping", Class: Read, Handle: func(context.Context, priv.Meta, json.RawMessage) (any, error) {
		return struct {
			Version string `json:"version"`
			Time    int64  `json:"time"`
		}{version.Version, time.Now().UnixMilli()}, nil
	}})
}
