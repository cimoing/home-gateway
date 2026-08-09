package storage

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/hirochachacha/go-smb2"
)

func TestIsSMBConnErrorRecognizesGoSMB2TransportEOF(t *testing.T) {
	wrapped := &os.PathError{
		Op:   "stat",
		Path: "bt",
		Err:  &smb2.TransportError{Err: io.EOF},
	}
	if !isSMBConnError(wrapped) {
		t.Fatalf("expected reconnectable error, got %#v (%v)", wrapped, wrapped)
	}
	if !isSMBConnError(errors.New("stat bt: connection error: EOF")) {
		t.Fatal("expected string-form connection EOF to be reconnectable")
	}
	if isSMBConnError(os.ErrNotExist) {
		t.Fatal("not-exist must not be treated as connection error")
	}
}
