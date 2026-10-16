package shipments

import (
	"context"
	"testing"
)

type identityStub struct {
	sent     bool
	verified bool
}

func (s *identityStub) SendLoginCode(context.Context, string, string) error {
	s.sent = true
	return nil
}

func (s *identityStub) VerifyAndCreateSession(context.Context, string, string, string) (Session, error) {
	s.verified = true
	return Session{UserID: "usr_2048", Session: map[string]string{"id": "ses_2048"}}, nil
}

func TestExceptionResolutionDecision(t *testing.T) {
	tests := []struct {
		name           string
		resolution     string
		wantState      string
		wantNextAction string
		wantError      bool
	}{
		{name: "recipient confirms redelivery", resolution: "recipient_confirmed", wantState: "out_for_redelivery", wantNextAction: "redeliver"},
		{name: "operator returns parcel", resolution: "return_to_depot", wantState: "returning", wantNextAction: "return_to_depot"},
		{name: "unknown decision is rejected", resolution: "discard", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			access := NewAccess(&identityStub{})
			shipment, err := access.ResolveException("SHP-2048", tt.resolution)
			if (err != nil) != tt.wantError {
				t.Fatalf("ResolveException() error = %v, wantError %v", err, tt.wantError)
			}
			if !tt.wantError && (shipment.State != tt.wantState || shipment.NextAction != tt.wantNextAction) {
				t.Fatalf("state/action = %s/%s, want %s/%s", shipment.State, shipment.NextAction, tt.wantState, tt.wantNextAction)
			}
			if !tt.wantError && (shipment.Proof == nil || len(shipment.Events) != 2 || shipment.Exception.Resolution != tt.resolution) {
				t.Fatalf("shipment evidence or resolution lost: %+v", shipment)
			}
		})
	}
}

func TestOwnerCheckedBeforeCodeSend(t *testing.T) {
	stub := &identityStub{}
	err := NewAccess(stub).StartLogin(context.Background(), "SHP-2048", "+14155550999", "req-1")
	if err == nil || stub.sent {
		t.Fatalf("owner mismatch error = %v, sent = %v", err, stub.sent)
	}
}
