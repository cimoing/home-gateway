package dns

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"home-gateway/internal/cloudflare"
	"home-gateway/internal/credential"
	"home-gateway/internal/database"
)

type fakeProvider struct {
	records  []cloudflare.Record
	listErr  error
	verified bool
}

func (p *fakeProvider) VerifyToken(context.Context) error {
	p.verified = true
	return nil
}

func (p *fakeProvider) FindZone(context.Context, string) (cloudflare.Zone, error) {
	return cloudflare.Zone{ID: "remote-zone", Name: "example.com", Status: "active"}, nil
}

func (p *fakeProvider) ListRecords(context.Context, string) ([]cloudflare.Record, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return append([]cloudflare.Record(nil), p.records...), nil
}

func (p *fakeProvider) CreateRecord(
	_ context.Context,
	_ string,
	input cloudflare.RecordInput,
) (cloudflare.Record, error) {
	record := cloudflare.Record{
		ID: "created", Type: input.Type, Name: input.Name, Content: input.Content,
		TTL: input.TTL, Proxied: input.Proxied, Priority: input.Priority,
		Data: input.Data, Comment: input.Comment,
	}
	p.records = append(p.records, record)
	return record, nil
}

func (p *fakeProvider) UpdateRecord(
	_ context.Context,
	_ string,
	recordID string,
	input cloudflare.RecordInput,
) (cloudflare.Record, error) {
	return cloudflare.Record{
		ID: recordID, Type: input.Type, Name: input.Name, Content: input.Content,
		TTL: input.TTL, Proxied: input.Proxied, Priority: input.Priority,
		Data: input.Data, Comment: input.Comment,
	}, nil
}

func (p *fakeProvider) DeleteRecord(context.Context, string, string) error {
	return nil
}

func TestServiceCredentialZoneAndRemoteAuthoritativeSync(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "dns.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, database.DriverSQLite); err != nil {
		t.Fatal(err)
	}

	encryptor, err := credential.New(bytes.Repeat([]byte{0x33}, 32))
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{records: []cloudflare.Record{
		{ID: "old", Type: "A", Name: "example.com", Content: "192.0.2.1", TTL: 1},
	}}
	service := NewService(db, encryptor, func(string) Provider { return provider })

	stored, err := service.CreateCredential(ctx, "primary", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	if !provider.verified || stored.TokenHint != "oken" || len(stored.TokenCiphertext) != 0 {
		t.Fatalf("unexpected safe credential: %+v", stored)
	}

	zone, err := service.CreateZone(ctx, stored.ID, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	records, err := service.ListRecords(ctx, zone.ID)
	if err != nil || len(records) != 1 || records[0].ProviderRecordID != "old" {
		t.Fatalf("unexpected initial cache %+v: %v", records, err)
	}

	provider.records = []cloudflare.Record{
		{ID: "new", Type: "TXT", Name: "example.com", Content: "new", TTL: 120},
	}
	records, err = service.SyncZone(ctx, zone.ID)
	if err != nil || len(records) != 1 || records[0].ProviderRecordID != "new" {
		t.Fatalf("unexpected synced cache %+v: %v", records, err)
	}

	provider.listErr = errors.New("temporary remote failure")
	if _, err := service.SyncZone(ctx, zone.ID); !errors.Is(err, ErrProvider) {
		t.Fatalf("expected provider error, got %v", err)
	}
	records, err = service.ListRecords(ctx, zone.ID)
	if err != nil || len(records) != 1 || records[0].ProviderRecordID != "new" {
		t.Fatalf("failed sync changed cache %+v: %v", records, err)
	}
}
