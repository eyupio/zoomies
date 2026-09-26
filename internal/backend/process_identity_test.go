package backend

import (
	"os"
	"testing"
)

func TestProcessBirthIdentityRejectsReusedPID(t *testing.T) {
	id, err := processIdentity(os.Getpid())
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("process birth identity needs matching PID namespace and proc mount")
		}
		t.Fatal(err)
	}

	if id == "" {
		t.Fatal("empty process identity")
	}
	meta := processMeta{PID: os.Getpid(), ProcessIdentity: id}
	if err := verifyProcessIdentity(meta, meta.PID); err != nil {
		t.Fatal(err)
	}
	meta.ProcessIdentity = "old-process"
	if err := verifyProcessIdentity(meta, meta.PID); err == nil {
		t.Fatal("reused PID trusted")
	}
}
