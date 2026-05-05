package auth

import "testing"

func TestTransportString(t *testing.T) {
	if TransportSSH.String() != "ssh" {
		t.Errorf("TransportSSH.String() = %q, want \"ssh\"", TransportSSH.String())
	}
	if TransportHTTPS.String() != "https" {
		t.Errorf("TransportHTTPS.String() = %q, want \"https\"", TransportHTTPS.String())
	}
}
