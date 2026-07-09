package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// payload is the on-the-wire JSON body delivered to a subscriber.
type payload struct {
	DeliveryID string          `json:"delivery_id"`
	Event      string          `json:"event"`
	EntityType string          `json:"entity_type"`
	ID         int64           `json:"id"`
	Actor      string          `json:"actor"`
	Timestamp  string          `json:"timestamp"`
	Data       json.RawMessage `json:"data,omitempty"`
}

// buildBody renders e as the signed JSON body for one delivery and returns the
// body bytes and the X-Almanaut-Signature value ("sha256=<hex>"). deliveryID is
// stable across retries of the same delivery.
func buildBody(e Event, deliveryID, secret string) ([]byte, string, error) {
	body, err := json.Marshal(payload{
		DeliveryID: deliveryID, Event: e.Action, EntityType: e.Type,
		ID: e.ID, Actor: e.Actor, Timestamp: e.Timestamp, Data: e.Data,
	})
	if err != nil {
		return nil, "", fmt.Errorf("marshal webhook payload: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return body, "sha256=" + hex.EncodeToString(mac.Sum(nil)), nil
}

// newDeliveryID returns a random hex id identifying a single delivery.
func newDeliveryID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("webhook delivery id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
