package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestClientTokenZoneAndRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")

		switch {
		case request.URL.Path == "/user/tokens/verify":
			writeEnvelope(writer, map[string]string{"status": "active"}, nil)
		case request.URL.Path == "/zones":
			writeEnvelope(writer, []Zone{{ID: "zone-1", Name: "example.com", Status: "active"}}, nil)
		case request.URL.Path == "/zones/zone-1/dns_records" && request.Method == http.MethodGet:
			page, _ := strconv.Atoi(request.URL.Query().Get("page"))
			if page == 1 {
				writeEnvelope(
					writer,
					[]Record{{ID: "record-1", Type: "A", Name: "example.com", Content: "192.0.2.1", TTL: 1}},
					map[string]int{"page": 1, "total_pages": 2},
				)
			} else {
				writeEnvelope(
					writer,
					[]Record{{ID: "record-2", Type: "TXT", Name: "example.com", Content: "hello", TTL: 120}},
					map[string]int{"page": 2, "total_pages": 2},
				)
			}
		case request.URL.Path == "/zones/zone-1/dns_records" && request.Method == http.MethodPost:
			writeEnvelope(writer, Record{ID: "record-3", Type: "A", Name: "www.example.com", Content: "192.0.2.2", TTL: 1}, nil)
		case request.URL.Path == "/zones/zone-1/dns_records/record-3" && request.Method == http.MethodPut:
			writeEnvelope(writer, Record{ID: "record-3", Type: "A", Name: "www.example.com", Content: "192.0.2.3", TTL: 1}, nil)
		case request.URL.Path == "/zones/zone-1/dns_records/record-3" && request.Method == http.MethodDelete:
			writeEnvelope(writer, map[string]string{"id": "record-3"}, nil)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	if err := client.VerifyToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	zone, err := client.FindZone(context.Background(), "EXAMPLE.com")
	if err != nil || zone.ID != "zone-1" {
		t.Fatalf("unexpected zone %+v: %v", zone, err)
	}
	records, err := client.ListRecords(context.Background(), zone.ID)
	if err != nil || len(records) != 2 {
		t.Fatalf("unexpected records %+v: %v", records, err)
	}

	created, err := client.CreateRecord(context.Background(), zone.ID, RecordInput{
		Type: "A", Name: "www.example.com", Content: "192.0.2.2", TTL: 1,
	})
	if err != nil || created.ID != "record-3" {
		t.Fatalf("unexpected created record %+v: %v", created, err)
	}
	updated, err := client.UpdateRecord(context.Background(), zone.ID, created.ID, RecordInput{
		Type: "A", Name: "www.example.com", Content: "192.0.2.3", TTL: 1,
	})
	if err != nil || !strings.HasSuffix(updated.Content, ".3") {
		t.Fatalf("unexpected updated record %+v: %v", updated, err)
	}
	if err := client.DeleteRecord(context.Background(), zone.ID, created.ID); err != nil {
		t.Fatal(err)
	}
}

func writeEnvelope(writer http.ResponseWriter, result any, resultInfo any) {
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"success":     true,
		"errors":      []any{},
		"result":      result,
		"result_info": resultInfo,
	})
}
