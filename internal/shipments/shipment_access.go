package shipments

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"time"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

type PhoneIdentity interface {
	SendLoginCode(context.Context, string, string) error
	VerifyAndCreateSession(context.Context, string, string, string) (Session, error)
}

type Session struct {
	UserID  string `json:"user_id"`
	Session any    `json:"session"`
}

type Event struct {
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
	Location   string    `json:"location"`
}

type ProofOfDelivery struct {
	ObjectKey   string    `json:"object_key"`
	ContentType string    `json:"content_type"`
	CapturedAt  time.Time `json:"captured_at"`
}

type Exception struct {
	Code       string `json:"code"`
	Detail     string `json:"detail"`
	Resolution string `json:"resolution,omitempty"`
}

type Shipment struct {
	ID         string           `json:"id"`
	Phone      string           `json:"-"`
	State      string           `json:"state"`
	Events     []Event          `json:"events"`
	Proof      *ProofOfDelivery `json:"proof_of_delivery,omitempty"`
	Exception  *Exception       `json:"exception,omitempty"`
	NextAction string           `json:"next_action"`
}

type Access struct {
	identity PhoneIdentity
	mu       sync.Mutex
	data     map[string]Shipment
}

func NewAccess(identity PhoneIdentity) *Access {
	stamp := time.Date(2026, 9, 23, 8, 15, 0, 0, time.UTC)
	return &Access{identity: identity, data: map[string]Shipment{
		"SHP-2048": {
			ID: "SHP-2048", Phone: "+14155550123", State: "exception",
			Events: []Event{
				{Kind: "picked_up", OccurredAt: stamp.Add(-4 * time.Hour), Location: "SHA-01"},
				{Kind: "delivery_attempted", OccurredAt: stamp, Location: "SHA-17"},
			},
			Proof:      &ProofOfDelivery{ObjectKey: "pod/SHP-2048/attempt.jpg", ContentType: "image/jpeg", CapturedAt: stamp},
			Exception:  &Exception{Code: "RECIPIENT_UNAVAILABLE", Detail: "No recipient at delivery point"},
			NextAction: "contact_recipient",
		},
	}}
}

func (a *Access) StartLogin(ctx context.Context, shipmentID, phone, requestID string) error {
	if _, err := a.ownedShipment(shipmentID, phone); err != nil {
		return err
	}
	return a.identity.SendLoginCode(ctx, phone, requestID)
}

func (a *Access) VerifyLogin(ctx context.Context, shipmentID, phone, code, requestID string) (Session, Shipment, error) {
	shipment, err := a.ownedShipment(shipmentID, phone)
	if err != nil {
		return Session{}, Shipment{}, err
	}
	session, err := a.identity.VerifyAndCreateSession(ctx, phone, code, requestID)
	if err != nil {
		return Session{}, Shipment{}, err
	}
	return session, shipment, nil
}

func (a *Access) ResolveException(shipmentID, resolution string) (Shipment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	shipment, found := a.data[shipmentID]
	if !found {
		return Shipment{}, errors.New("shipment not found")
	}
	if shipment.Exception == nil {
		return Shipment{}, errors.New("shipment has no open exception")
	}
	if resolution != "recipient_confirmed" && resolution != "return_to_depot" {
		return Shipment{}, errors.New("resolution must be recipient_confirmed or return_to_depot")
	}
	shipment.Exception.Resolution = resolution
	if resolution == "recipient_confirmed" {
		shipment.State = "out_for_redelivery"
		shipment.NextAction = "redeliver"
	} else {
		shipment.State = "returning"
		shipment.NextAction = "return_to_depot"
	}
	a.data[shipmentID] = shipment
	return shipment, nil
}

func (a *Access) ownedShipment(id, phone string) (Shipment, error) {
	if !phonePattern.MatchString(phone) {
		return Shipment{}, errors.New("phone must use E.164 format")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	shipment, found := a.data[id]
	if !found || shipment.Phone != phone {
		return Shipment{}, errors.New("shipment and phone do not match")
	}
	return shipment, nil
}
