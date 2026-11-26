package credstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
)

// Register adds the auth.* methods to the helper's registry.
func Register(srv *privd.Server, st *Store) {
	srv.Register(privd.Method{Name: "auth.password.verify", Class: privd.Read, Handle: func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}](raw)
		if err != nil {
			return nil, err
		}
		ok, epoch, err := st.Verify(ctx, p.Username, p.Password)
		if err != nil {
			return nil, err
		}
		return struct {
			OK    bool  `json:"ok"`
			Epoch int64 `json:"epoch"`
		}{ok, epoch}, nil
	}})

	srv.Register(privd.Method{Name: "auth.epoch.get", Class: privd.Read, Handle: func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[struct {
			Username string `json:"username"`
		}](raw)
		if err != nil {
			return nil, err
		}
		e, err := st.Epoch(ctx, p.Username)
		return struct {
			Epoch int64 `json:"epoch"`
		}{e}, err
	}})

	srv.Register(privd.Method{Name: "auth.password.change", Class: privd.Mutate, Handle: func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[struct {
			Username        string `json:"username"`
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}](raw)
		if err != nil {
			return nil, err
		}
		return struct{}{}, st.Change(ctx, p.Username, p.CurrentPassword, p.NewPassword)
	}})

	// Only root (yubeosctl) may mint setup links: a compromised daemon cannot create its own way in.
	srv.Register(privd.Method{Name: "auth.setup.create", Class: privd.Mutate, RootOnly: true, Handle: func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[struct {
			Username   string `json:"username"`
			Purpose    string `json:"purpose"`
			TTLMinutes int    `json:"ttlMinutes"`
		}](raw)
		if err != nil {
			return nil, err
		}
		if p.TTLMinutes == 0 {
			p.TTLMinutes = 60
		}
		if p.TTLMinutes < 1 || p.TTLMinutes > 1440 {
			return nil, priv.Errorf(priv.CodeInvalidParams, "ttlMinutes must be 1..1440")
		}
		tok, exp, err := st.CreateSetupLink(ctx, p.Username, p.Purpose, time.Duration(p.TTLMinutes)*time.Minute)
		if err != nil {
			return nil, err
		}
		return struct {
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expiresAt"`
		}{tok, exp.UnixMilli()}, nil
	}})

	srv.Register(privd.Method{Name: "auth.setup.consume", Class: privd.Mutate, Handle: func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[struct {
			Token       string `json:"token"`
			NewPassword string `json:"newPassword"`
		}](raw)
		if err != nil {
			return nil, err
		}
		u, err := st.ConsumeSetupLink(ctx, p.Token, p.NewPassword)
		if err != nil {
			return nil, err
		}
		return struct {
			Username string `json:"username"`
		}{u}, nil
	}})

	registerSecondFactors(srv, st)
}
