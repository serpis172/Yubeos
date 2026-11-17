package priv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Request{JSONRPC: "2.0", ID: 7, Method: "a.b", Meta: Meta{ReqID: "r1", Actor: "alice"}}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out Request
	if err := ReadFrame(&buf, &out); err != nil || out.ID != 7 || out.Meta.Actor != "alice" {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestReadFrameRejectsBadLengthsBeforeAllocating(t *testing.T) {
	for _, n := range []uint32{0, MaxFrame + 1, 0xffffffff} {
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], n)
		var v Request
		err := ReadFrame(bytes.NewReader(hdr[:]), &v)
		var pe *Error
		if !errors.As(err, &pe) || pe.Code != CodeTooLarge {
			t.Errorf("length %d: got %v", n, err)
		}
	}
	// truncated body
	var v Request
	if err := ReadFrame(bytes.NewReader([]byte{0, 0, 0, 10, 'x'}), &v); err == nil {
		t.Error("truncated frame must error")
	}
}

func TestWriteFrameRefusesOversizedPayloads(t *testing.T) {
	err := WriteFrame(&bytes.Buffer{}, map[string]string{"x": strings.Repeat("a", MaxFrame)})
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != CodeTooLarge {
		t.Fatalf("got %v", err)
	}
}

func TestErrorFormatting(t *testing.T) {
	e := Errorf("some-code", "bad %d", 3)
	if e.Error() != "some-code: bad 3" || e.Code != "some-code" {
		t.Fatalf("%v", e)
	}
}
