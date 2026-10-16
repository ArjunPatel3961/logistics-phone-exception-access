package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"example.com/logistics-phone-access/internal/infrai"
	"example.com/logistics-phone-access/internal/shipments"
)

type loginRequest struct {
	ShipmentID string `json:"shipment_id"`
	Phone      string `json:"phone"`
	Code       string `json:"code,omitempty"`
	RequestID  string `json:"request_id"`
}

type resolutionRequest struct {
	Resolution string `json:"resolution"`
}

func main() {
	client, err := infrai.NewClient(os.Getenv("INFRAI_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	access := shipments.NewAccess(phoneIdentityAdapter{client: client})
	mux := http.NewServeMux()

	mux.HandleFunc("POST /login/code", func(w http.ResponseWriter, r *http.Request) {
		var input loginRequest
		if err := decode(r, &input); err != nil || strings.TrimSpace(input.RequestID) == "" {
			writeError(w, http.StatusBadRequest, errors.New("valid JSON and request_id are required"))
			return
		}
		if err := access.StartLogin(r.Context(), input.ShipmentID, input.Phone, input.RequestID); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "code_sent", "shipment_id": input.ShipmentID})
	})

	mux.HandleFunc("POST /login/verify", func(w http.ResponseWriter, r *http.Request) {
		var input loginRequest
		if err := decode(r, &input); err != nil || input.Code == "" || strings.TrimSpace(input.RequestID) == "" {
			writeError(w, http.StatusBadRequest, errors.New("valid JSON, code, and request_id are required"))
			return
		}
		session, shipment, err := access.VerifyLogin(r.Context(), input.ShipmentID, input.Phone, input.Code, input.RequestID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": session, "shipment": shipment})
	})

	mux.HandleFunc("POST /shipments/{id}/exception/resolve", func(w http.ResponseWriter, r *http.Request) {
		var input resolutionRequest
		if err := decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		shipment, err := access.ResolveException(r.PathValue("id"), input.Resolution)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, shipment)
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("dispatch access listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

type phoneIdentityAdapter struct{ client *infrai.Client }

func (a phoneIdentityAdapter) SendLoginCode(ctx context.Context, phone, requestID string) error {
	return a.client.SendLoginCode(ctx, phone, requestID)
}

func (a phoneIdentityAdapter) VerifyAndCreateSession(ctx context.Context, phone, code, requestID string) (shipments.Session, error) {
	result, err := a.client.VerifyAndCreateSession(ctx, phone, code, requestID)
	if err != nil {
		return shipments.Session{}, err
	}
	return shipments.Session{UserID: result.UserID, Session: result.Session}, nil
}

func decode(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		writeError(w, apiErr.Status, err)
		return
	}
	writeError(w, http.StatusBadGateway, err)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
