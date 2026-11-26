package credstore

import (
	"context"
	"encoding/json"

	"github.com/serpis172/Yubeos/internal/priv"
	"github.com/serpis172/Yubeos/internal/privd"
)

// registerSecondFactors adds the auth.totp.*, auth.recovery.* and auth.factors RPC methods. Verification methods
// are Mutate: they advance the replay counter or consume a recovery code.
func registerSecondFactors(srv *privd.Server, st *Store) {
	type user struct {
		Username string `json:"username"`
	}
	type userCode struct {
		Username string `json:"username"`
		Code     string `json:"code"`
	}
	reg := func(name string, class privd.Class, h func(ctx context.Context, raw json.RawMessage) (any, error)) {
		srv.Register(privd.Method{Name: name, Class: class, Handle: func(ctx context.Context, _ priv.Meta, raw json.RawMessage) (any, error) { return h(ctx, raw) }})
	}
	reg("auth.factors", privd.Read, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[user](raw)
		if err != nil {
			return nil, err
		}
		t, left, err := st.Factors(ctx, p.Username)
		return struct {
			TOTP         bool `json:"totp"`
			RecoveryLeft int  `json:"recoveryLeft"`
		}{t, left}, err
	})
	reg("auth.totp.begin", privd.Mutate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[user](raw)
		if err != nil {
			return nil, err
		}
		sec, err := st.TOTPBegin(ctx, p.Username)
		return struct {
			Secret string `json:"secret"`
		}{sec}, err
	})
	reg("auth.totp.confirm", privd.Mutate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userCode](raw)
		if err != nil {
			return nil, err
		}
		codes, err := st.TOTPConfirm(ctx, p.Username, p.Code)
		return struct {
			Codes []string `json:"codes"`
		}{codes}, err
	})
	reg("auth.totp.verify", privd.Mutate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userCode](raw)
		if err != nil {
			return nil, err
		}
		ok, err := st.TOTPVerify(ctx, p.Username, p.Code)
		return struct {
			OK bool `json:"ok"`
		}{ok}, err
	})
	reg("auth.totp.remove", privd.Mutate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[user](raw)
		if err != nil {
			return nil, err
		}
		return struct{}{}, st.TOTPRemove(ctx, p.Username)
	})
	reg("auth.recovery.verify", privd.Mutate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[userCode](raw)
		if err != nil {
			return nil, err
		}
		ok, left, err := st.RecoveryVerify(ctx, p.Username, p.Code)
		return struct {
			OK   bool `json:"ok"`
			Left int  `json:"left"`
		}{ok, left}, err
	})
	reg("auth.recovery.regenerate", privd.Mutate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		p, err := privd.Decode[user](raw)
		if err != nil {
			return nil, err
		}
		codes, err := st.RecoveryRegenerate(ctx, p.Username)
		return struct {
			Codes []string `json:"codes"`
		}{codes}, err
	})
}
