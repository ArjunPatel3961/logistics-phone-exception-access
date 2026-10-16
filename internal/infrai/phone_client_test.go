package infrai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSendLoginCodeUsesOneBoundary(t *testing.T) {
	var paths []string
	var recipients []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got == "" {
			t.Fatal("Idempotency-Key is empty")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if phone, ok := body["phone"].(string); ok {
			recipients = append(recipients, phone)
		}
		if to, ok := body["to"].(string); ok {
			recipients = append(recipients, to)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	client, err := NewClient("test-key")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	client.http = server.Client()
	if err := client.SendLoginCode(context.Background(), "+14155550123", "request-7"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/v1/auth/phone/send_code", "/v1/sms/otp"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if want := []string{"+14155550123", "+14155550123"}; !reflect.DeepEqual(recipients, want) {
		t.Fatalf("recipients = %v, want %v", recipients, want)
	}
}

func TestEnvelopeErrorWinsOverHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"ok":false,"data":null,"error":{"code":"INPUT_REJECTED","message":"check input"},"metadata":{}}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-key")
	client.baseURL = server.URL
	client.http = server.Client()
	_, err := client.post(context.Background(), "/v1/sms/otp", map[string]string{"to": "+14155550123"}, "request-8")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != "INPUT_REJECTED" || apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("error = %#v", err)
	}
}
