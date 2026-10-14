package device

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPendingProfileUpdatesWithoutChangingIdentity(t *testing.T) {
	desc := testDesc()
	s := NewSession(desc, "http://192.0.2.1:8080/inform", State{}, nil, time.Now())
	updated := desc
	updated.Model = "S248500"
	updated.ModelDisplay = "profile"
	if err := s.SetPendingProfile(updated); err != nil {
		t.Fatal(err)
	}
	if got := payload(t, s)["model"]; got != updated.Model {
		t.Errorf("model=%v", got)
	}
	updated.MAC = "02:00:00:00:00:01"
	if err := s.SetPendingProfile(updated); err == nil {
		t.Fatal("identity change accepted")
	}
}

func TestAdoptedProfileIsPersistedAndCannotAutoChange(t *testing.T) {
	desc := testDesc()
	store := &Store{Path: filepath.Join(t.TempDir(), "device.json")}
	s := NewSession(desc, "http://192.0.2.1:8080/inform", State{Adopted: true}, store, time.Now())
	if err := store.Save(s.st); err != nil {
		t.Fatal(err)
	}
	st, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.Model != desc.Model {
		t.Errorf("persisted model=%q", st.Model)
	}
	desc.Model = "S248500"
	if err := s.SetPendingProfile(desc); err == nil {
		t.Fatal("adopted profile changed automatically")
	}
}
