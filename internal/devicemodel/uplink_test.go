package devicemodel

import "testing"

func TestUplinkHintPrecedesLLDPCandidate(t *testing.T) {
	s := Snapshot{UplinkHint: 2, Ports: []Port{{Index: 1, Up: true, Neighbor: &Neighbor{IsBridge: true, IsRouter: true}}, {Index: 2, Up: true}}}
	if got := s.UplinkPort(); got != 2 {
		t.Errorf("uplink=%d, want hint 2", got)
	}
}
