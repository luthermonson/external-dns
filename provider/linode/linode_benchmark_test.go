package linode

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/linode/linodego"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

// BenchmarkMockDomainClient is a mock that supports functional returns for dynamic behavior
type BenchmarkMockDomainClient struct {
	mock.Mock
}

func (m *BenchmarkMockDomainClient) ListDomainRecords(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error) {
	args := m.Called(ctx, domainID, opts)
	if fn, ok := args.Get(0).(func(context.Context, int, *linodego.ListOptions) ([]linodego.DomainRecord, error)); ok {
		return fn(ctx, domainID, opts)
	}
	return args.Get(0).([]linodego.DomainRecord), args.Error(1)
}

func (m *BenchmarkMockDomainClient) ListDomains(ctx context.Context, opts *linodego.ListOptions) ([]linodego.Domain, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).([]linodego.Domain), args.Error(1)
}

func (m *BenchmarkMockDomainClient) CreateDomainRecord(ctx context.Context, domainID int, opts linodego.DomainRecordCreateOptions) (*linodego.DomainRecord, error) {
	args := m.Called(ctx, domainID, opts)
	return args.Get(0).(*linodego.DomainRecord), args.Error(1)
}

func (m *BenchmarkMockDomainClient) DeleteDomainRecord(ctx context.Context, domainID int, recordID int) error {
	args := m.Called(ctx, domainID, recordID)
	return args.Error(0)
}

func (m *BenchmarkMockDomainClient) UpdateDomainRecord(ctx context.Context, domainID int, recordID int, opts linodego.DomainRecordUpdateOptions) (*linodego.DomainRecord, error) {
	args := m.Called(ctx, domainID, recordID, opts)
	return args.Get(0).(*linodego.DomainRecord), args.Error(1)
}

func BenchmarkApplyChanges(b *testing.B) {
	// Discard logs to avoid spamming output and affecting benchmark performance
	log.SetOutput(io.Discard)

	// Configuration for the benchmark
	// Scaled down to 5000 per zone (500k total) to ensure benchmark completes within reasonable time
	numZones := 100
	recordsPerZone := 5000

	// Pre-generate zones and records to avoid benchmarking setup time
	zones := make([]linodego.Domain, numZones)
	for i := 0; i < numZones; i++ {
		zones[i] = linodego.Domain{
			ID:     i,
			Domain: fmt.Sprintf("zone-%d.com", i),
		}
	}

	// Create a mock client
	mockDomainClient := &BenchmarkMockDomainClient{}
	provider := &LinodeProvider{
		Client:       mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		recordFilter: endpoint.NewRecordFilter([]string{}),
		DryRun:       false,
	}

	// Setup mock expectations
	// ListDomains will be called once per ApplyChanges
	mockDomainClient.On("ListDomains", mock.Anything, mock.Anything).Return(zones, nil)

	// ListDomainRecords filtered will be called for each change
	// We'll mock it to return empty list (for creates) or existing record (for updates/deletes)
	// optimizing by using a generic return to avoid 50k*100 mock calls setup overhead
	mockDomainClient.On("ListDomainRecords", mock.Anything, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error) {
			// Simulate finding records for updates/deletes, not finding for creates
			if opts != nil && opts.Filter != "" {
				// Simple heuristic: if we're testing "Create", assume not found
				// Realistically we'd parse the filter, but for benchmark throughput this is sufficient
				// providing the benchmarks are run separately or with cleared mocks
				return []linodego.DomainRecord{}, nil
			}
			return []linodego.DomainRecord{}, nil
		},
	)

	// Mock action calls
	mockDomainClient.On("CreateDomainRecord", mock.Anything, mock.Anything, mock.Anything).Return(&linodego.DomainRecord{}, nil)
	mockDomainClient.On("UpdateDomainRecord", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&linodego.DomainRecord{}, nil)
	mockDomainClient.On("DeleteDomainRecord", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Helper to generate endpoints
	generateEndpoints := func(count int, prefix string) []*endpoint.Endpoint {
		eps := make([]*endpoint.Endpoint, 0, count*numZones)
		for z := 0; z < numZones; z++ {
			domain := zones[z].Domain
			for i := 0; i < count; i++ {
				eps = append(eps, &endpoint.Endpoint{
					DNSName:    fmt.Sprintf("%s-%d.%s", prefix, i, domain),
					RecordType: "A",
					Targets:    []string{"1.2.3.4"},
					RecordTTL:  300,
				})
			}
		}
		return eps
	}

	// Pre-generate changes
	creates := generateEndpoints(recordsPerZone, "create")
	updates := generateEndpoints(recordsPerZone, "update")
	deletes := generateEndpoints(recordsPerZone, "delete")

	// 1. Benchmark Creates
	b.Run("Creates", func(b *testing.B) {
		changes := &plan.Changes{
			Create: creates,
		}

		// Reset mocks for optimized behavior (filtered lookups)
		mockDomainClient.ExpectedCalls = nil
		mockDomainClient.On("ListDomains", mock.Anything, mock.Anything).Return(zones, nil)
		// Note: We removed the ListDomainRecords expectation here because we are removing the check in the code
		mockDomainClient.On("CreateDomainRecord", mock.Anything, mock.Anything, mock.Anything).Return(&linodego.DomainRecord{}, nil)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = provider.ApplyChanges(context.Background(), changes)
		}
	})

	// 2. Benchmark Updates
	// For updates, we need the mock to return existing records
	// Resetting mocks for specific behavior
	mockDomainClient.ExpectedCalls = nil
	mockDomainClient.On("ListDomains", mock.Anything, mock.Anything).Return(zones, nil)
	mockDomainClient.On("ListDomainRecords", mock.Anything, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error) {
			// Return a mock record found
			return []linodego.DomainRecord{{
				ID:     1,
				Name:   "mock",
				Type:   linodego.RecordTypeA,
				Target: "1.2.3.4",
			}}, nil
		},
	)
	mockDomainClient.On("UpdateDomainRecord", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&linodego.DomainRecord{}, nil)

	b.Run("Updates", func(b *testing.B) {
		changes := &plan.Changes{
			UpdateNew: updates,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = provider.ApplyChanges(context.Background(), changes)
		}
	})

	// 3. Benchmark Deletes
	mockDomainClient.ExpectedCalls = nil
	mockDomainClient.On("ListDomains", mock.Anything, mock.Anything).Return(zones, nil)
	mockDomainClient.On("ListDomainRecords", mock.Anything, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error) {
			return []linodego.DomainRecord{{
				ID:     1,
				Name:   "mock",
				Type:   linodego.RecordTypeA,
				Target: "1.2.3.4",
			}}, nil
		},
	)
	mockDomainClient.On("DeleteDomainRecord", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	b.Run("Deletes", func(b *testing.B) {
		changes := &plan.Changes{
			Delete: deletes,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = provider.ApplyChanges(context.Background(), changes)
		}
	})

	// 4. Benchmark All (Creates, Updates, Deletes mixed)
	// We need a smarter mock that returns found/not found based on context, or just returns something generic
	// For this mixed bench, let's assume all lookups succeed (find a record), which means:
	// - Creates will fail duplicate check (skip) or we need to simulate not finding them.
	// Let's refine the mock strategy:
	// "create" prefix -> not found
	// "update"/"delete" prefix -> found

	mockDomainClient.ExpectedCalls = nil
	mockDomainClient.On("ListDomains", mock.Anything, mock.Anything).Return(zones, nil)
	mockDomainClient.On("ListDomainRecords", mock.Anything, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error) {
			if opts != nil && opts.Filter != "" {
				// Extract name from filter (simple string check for speed in bench)
				// The filter format is `{"name": "%s", "type": "%s"}`
				// We can just check if the filter string contains "create"
				// Note: In real app, we'd parse JSON, but this is a high-perf benchmark mock
				if contains(opts.Filter, "create") {
					return []linodego.DomainRecord{}, nil
				}
			}
			return []linodego.DomainRecord{{
				ID:     1,
				Name:   "mock",
				Type:   linodego.RecordTypeA,
				Target: "1.2.3.4",
			}}, nil
		},
	)
	mockDomainClient.On("CreateDomainRecord", mock.Anything, mock.Anything, mock.Anything).Return(&linodego.DomainRecord{}, nil)
	mockDomainClient.On("UpdateDomainRecord", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&linodego.DomainRecord{}, nil)
	mockDomainClient.On("DeleteDomainRecord", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	b.Run("AllMixed", func(b *testing.B) {
		changes := &plan.Changes{
			Create:    creates,
			UpdateNew: updates,
			Delete:    deletes,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = provider.ApplyChanges(context.Background(), changes)
		}
	})
}

// Simple string contains helper for the benchmark mock
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// endpointsByZone is a helper copied from linode.go for benchmarking purposes
// since it is not exported.
func endpointsByZoneBenchmark(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) map[string][]endpoint.Endpoint {
	endpointsByZone := make(map[string][]endpoint.Endpoint)

	for _, ep := range endpoints {
		zoneID, _ := zoneNameIDMapper.FindZone(ep.DNSName)
		if zoneID == "" {
			continue
		}
		endpointsByZone[zoneID] = append(endpointsByZone[zoneID], *ep)
	}

	return endpointsByZone
}
