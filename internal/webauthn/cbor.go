package webauthn

import (
	"encoding/binary"
	"errors"
)

// A tiny CBOR decoder: only what attestation objects and COSE keys use (definite lengths, no floats, no tags).
var errCBOR = errors.New("webauthn: malformed CBOR")

type cborReader struct {
	b     []byte
	depth int
}

// decodeCBOR returns int64 | []byte | string | []any | map[any]any | bool | nil, and the unread rest.
func decodeCBOR(b []byte) (any, []byte, error) {
	r := &cborReader{b: b}
	v, err := r.next()
	return v, r.b, err
}

func (r *cborReader) take(n uint64) ([]byte, error) {
	if n > uint64(len(r.b)) {
		return nil, errCBOR
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out, nil
}

func (r *cborReader) head() (major byte, arg uint64, err error) {
	h, err := r.take(1)
	if err != nil {
		return 0, 0, err
	}
	major, info := h[0]>>5, h[0]&0x1f
	switch {
	case info < 24:
		return major, uint64(info), nil
	case info == 24:
		x, err := r.take(1)
		if err != nil {
			return 0, 0, err
		}
		return major, uint64(x[0]), nil
	case info == 25:
		x, err := r.take(2)
		if err != nil {
			return 0, 0, err
		}
		return major, uint64(binary.BigEndian.Uint16(x)), nil
	case info == 26:
		x, err := r.take(4)
		if err != nil {
			return 0, 0, err
		}
		return major, uint64(binary.BigEndian.Uint32(x)), nil
	case info == 27:
		x, err := r.take(8)
		if err != nil {
			return 0, 0, err
		}
		return major, binary.BigEndian.Uint64(x), nil
	}
	return 0, 0, errCBOR // indefinite lengths are not accepted
}

func (r *cborReader) next() (any, error) {
	if r.depth > 8 {
		return nil, errCBOR
	}
	major, arg, err := r.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case 0:
		if arg > 1<<62 {
			return nil, errCBOR
		}
		return int64(arg), nil
	case 1:
		if arg > 1<<62 {
			return nil, errCBOR
		}
		return -1 - int64(arg), nil
	case 2:
		b, err := r.take(arg)
		return append([]byte(nil), b...), err
	case 3:
		b, err := r.take(arg)
		return string(b), err
	case 4, 5:
		if arg > 64 {
			return nil, errCBOR
		}
		r.depth++
		defer func() { r.depth-- }()
		if major == 4 {
			out := make([]any, 0, arg)
			for i := uint64(0); i < arg; i++ {
				v, err := r.next()
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			return out, nil
		}
		m := make(map[any]any, arg)
		for i := uint64(0); i < arg; i++ {
			k, err := r.next()
			if err != nil {
				return nil, err
			}
			v, err := r.next()
			if err != nil {
				return nil, err
			}
			m[normKey(k)] = v
		}
		return m, nil
	case 7:
		switch arg {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22:
			return nil, nil
		}
	}
	return nil, errCBOR
}

// normKey folds integer keys to int64 so lookups are uniform.
func normKey(k any) any {
	if u, ok := k.(uint64); ok {
		return int64(u)
	}
	return k
}
