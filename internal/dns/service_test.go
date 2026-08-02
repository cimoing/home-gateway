package dns

import (
	"context"
	"errors"
	"testing"
	"time"

	"home-gateway/internal/cloudflare"
	"home-gateway/internal/config"
)

type fakeProvider struct {
	zone    cloudflare.Zone
	records []cloudflare.Record
	fail    error
}

func (p *fakeProvider) VerifyToken(context.Context) error { return nil }

func (p *fakeProvider) FindZone(_ context.Context, name string) (cloudflare.Zone, error) {
	if p.fail != nil {
		return cloudflare.Zone{}, p.fail
	}
	if p.zone.Name == "" {
		return cloudflare.Zone{}, cloudflare.ErrNotFound
	}
	zone := p.zone
	zone.Name = name
	return zone, nil
}

func (p *fakeProvider) ListRecords(context.Context, string) ([]cloudflare.Record, error) {
	if p.fail != nil {
		return nil, p.fail
	}
	return append([]cloudflare.Record(nil), p.records...), nil
}

func (p *fakeProvider) CreateRecord(
	_ context.Context,
	_ string,
	input cloudflare.RecordInput,
) (cloudflare.Record, error) {
	record := cloudflare.Record{
		ID: "rec-1", Type: input.Type, Name: input.Name, Content: input.Content, TTL: input.TTL,
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
	record := cloudflare.Record{
		ID: recordID, Type: input.Type, Name: input.Name, Content: input.Content, TTL: input.TTL,
	}
	for index := range p.records {
		if p.records[index].ID == recordID {
			p.records[index] = record
			return record, nil
		}
	}
	return record, nil
}

func (p *fakeProvider) DeleteRecord(_ context.Context, _ string, recordID string) error {
	next := p.records[:0]
	for _, record := range p.records {
		if record.ID == recordID {
			continue
		}
		next = append(next, record)
	}
	p.records = next
	return nil
}

func TestDNSRemoteCacheAndIncrementalUpdates(t *testing.T) {
	provider := &fakeProvider{
		zone: cloudflare.Zone{ID: "zone-1", Name: "example.com", Status: "active"},
		records: []cloudflare.Record{
			{ID: "a1", Type: "A", Name: "www.example.com", Content: "1.2.3.4", TTL: 300},
		},
	}
	service := NewService(config.CloudflareConfig{
		Token: "token",
		Zones: []string{"example.com"},
	}, func(string) Provider { return provider })

	ctx := context.Background()
	zones, err := service.ListZones(ctx)
	if err != nil || len(zones) != 1 || zones[0].ProviderZoneID != "zone-1" {
		t.Fatalf("list zones: %+v %v", zones, err)
	}
	records, err := service.ListRecords(ctx, "example.com")
	if err != nil || len(records) != 1 {
		t.Fatalf("list records: %+v %v", records, err)
	}
	created, err := service.CreateRecord(ctx, "example.com", cloudflare.RecordInput{
		Type: "A", Name: "api.example.com", Content: "9.9.9.9", TTL: 120,
	})
	if err != nil || created.ID == "" {
		t.Fatalf("create record: %+v %v", created, err)
	}
	records, err = service.ListRecords(ctx, "example.com")
	if err != nil || len(records) != 2 {
		t.Fatalf("cached after create: %+v %v", records, err)
	}
	if _, err := service.UpdateRecord(ctx, "example.com", created.ID, cloudflare.RecordInput{
		Type: "A", Name: "api.example.com", Content: "8.8.8.8", TTL: 120,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteRecord(ctx, "example.com", created.ID); err != nil {
		t.Fatal(err)
	}
	records, err = service.ListRecords(ctx, "example.com")
	if err != nil || len(records) != 1 {
		t.Fatalf("cached after delete: %+v %v", records, err)
	}

	provider.fail = errors.New("boom")
	if _, err := service.RefreshZone(ctx, "example.com"); !errors.Is(err, ErrProvider) {
		t.Fatalf("expected provider error, got %v", err)
	}
	// Failed refresh must keep previous cache.
	records, err = service.ListRecords(ctx, "example.com")
	if err != nil || len(records) != 1 {
		t.Fatalf("cache retained: %+v %v", records, err)
	}
	_ = time.Now
}
