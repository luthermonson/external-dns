/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package linode

import (
	"context"
	"os"
	"testing"

	"github.com/linode/linodego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

type MockDomainClient struct {
	mock.Mock
}

func (m *MockDomainClient) ListDomainRecords(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error) {
	args := m.Called(ctx, domainID, opts)
	return args.Get(0).([]linodego.DomainRecord), args.Error(1)
}

func (m *MockDomainClient) ListDomains(ctx context.Context, opts *linodego.ListOptions) ([]linodego.Domain, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).([]linodego.Domain), args.Error(1)
}

func (m *MockDomainClient) CreateDomainRecord(ctx context.Context, domainID int, opts linodego.DomainRecordCreateOptions) (*linodego.DomainRecord, error) {
	args := m.Called(ctx, domainID, opts)
	return args.Get(0).(*linodego.DomainRecord), args.Error(1)
}

func (m *MockDomainClient) DeleteDomainRecord(ctx context.Context, domainID int, recordID int) error {
	args := m.Called(ctx, domainID, recordID)
	return args.Error(0)
}

func (m *MockDomainClient) UpdateDomainRecord(ctx context.Context, domainID int, recordID int, opts linodego.DomainRecordUpdateOptions) (*linodego.DomainRecord, error) {
	args := m.Called(ctx, domainID, recordID, opts)
	return args.Get(0).(*linodego.DomainRecord), args.Error(1)
}

func createZones() []linodego.Domain {
	return []linodego.Domain{
		{ID: 1, Domain: "foo.com"},
		{ID: 2, Domain: "bar.io"},
		{ID: 3, Domain: "baz.com"},
	}
}

func createFooRecords() []linodego.DomainRecord {
	return []linodego.DomainRecord{{
		ID:     11,
		Type:   linodego.RecordTypeA,
		Name:   "",
		Target: "targetFoo",
	}, {
		ID:     12,
		Type:   linodego.RecordTypeTXT,
		Name:   "",
		Target: "txt",
	}, {
		ID:     13,
		Type:   linodego.RecordTypeCAA,
		Name:   "foo.com",
		Target: "",
	}}
}

func createBarRecords() []linodego.DomainRecord {
	return []linodego.DomainRecord{}
}

func createBazRecords() []linodego.DomainRecord {
	return []linodego.DomainRecord{{
		ID:     31,
		Type:   linodego.RecordTypeA,
		Name:   "",
		Target: "targetBaz",
	}, {
		ID:     32,
		Type:   linodego.RecordTypeTXT,
		Name:   "",
		Target: "txt",
	}, {
		ID:     33,
		Type:   linodego.RecordTypeA,
		Name:   "api",
		Target: "targetBaz",
	}, {
		ID:     34,
		Type:   linodego.RecordTypeTXT,
		Name:   "api",
		Target: "txt",
	}}
}

func TestLinodeConvertRecordType(t *testing.T) {
	record, err := convertRecordType("A")
	require.NoError(t, err)
	assert.Equal(t, linodego.RecordTypeA, record)

	record, err = convertRecordType("AAAA")
	require.NoError(t, err)
	assert.Equal(t, linodego.RecordTypeAAAA, record)

	record, err = convertRecordType("CNAME")
	require.NoError(t, err)
	assert.Equal(t, linodego.RecordTypeCNAME, record)

	record, err = convertRecordType("TXT")
	require.NoError(t, err)
	assert.Equal(t, linodego.RecordTypeTXT, record)

	record, err = convertRecordType("SRV")
	require.NoError(t, err)
	assert.Equal(t, linodego.RecordTypeSRV, record)

	record, err = convertRecordType("NS")
	require.NoError(t, err)
	assert.Equal(t, linodego.RecordTypeNS, record)

	_, err = convertRecordType("INVALID")
	require.Error(t, err)
}

func TestNewLinodeProvider(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	_, err := NewLinodeProvider(endpoint.NewDomainFilter([]string{"ext-dns-test.zalando.to."}), []string{}, []string{}, "noop", true)
	require.NoError(t, err)

	_ = os.Unsetenv("LINODE_TOKEN")
	_, err = NewLinodeProvider(endpoint.NewDomainFilter([]string{"ext-dns-test.zalando.to."}), []string{}, []string{}, "noop", true)
	require.Error(t, err)
}

func TestLinodeStripRecordName(t *testing.T) {
	assert.Equal(t, "api", getStrippedRecordName(linodego.Domain{
		Domain: "example.com",
	}, endpoint.Endpoint{
		DNSName: "api.example.com",
	}))

	assert.Empty(t, getStrippedRecordName(linodego.Domain{
		Domain: "example.com",
	}, endpoint.Endpoint{
		DNSName: "example.com",
	}))
}

func TestLinodeFetchZonesNoFilters(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	expected := createZones()
	actual, err := provider.fetchZones(context.Background())
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
	assert.Equal(t, expected, actual)
}

func TestLinodeFetchZonesWithFilter(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{".com"}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	expected := []linodego.Domain{
		{ID: 1, Domain: "foo.com"},
		{ID: 3, Domain: "baz.com"},
	}
	actual, err := provider.fetchZones(context.Background())
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
	assert.Equal(t, expected, actual)
}

func TestLinodeGetStrippedRecordName(t *testing.T) {
	assert.Empty(t, getStrippedRecordName(linodego.Domain{
		Domain: "foo.com",
	}, endpoint.Endpoint{
		DNSName: "foo.com",
	}))

	assert.Equal(t, "api", getStrippedRecordName(linodego.Domain{
		Domain: "foo.com",
	}, endpoint.Endpoint{
		DNSName: "api.foo.com",
	}))
}

func TestLinodeRecords(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return(createFooRecords(), nil).Once()
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		2,
		mock.Anything,
	).Return(createBarRecords(), nil).Once()
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		3,
		mock.Anything,
	).Return(createBazRecords(), nil).Once()

	actual, err := provider.Records(context.Background())
	require.NoError(t, err)

	expected := []*endpoint.Endpoint{
		{DNSName: "foo.com", Targets: []string{"targetFoo"}, RecordType: "A", RecordTTL: 0, Labels: endpoint.NewLabels()},
		{DNSName: "foo.com", Targets: []string{"txt"}, RecordType: "TXT", RecordTTL: 0, Labels: endpoint.NewLabels()},
		{DNSName: "baz.com", Targets: []string{"targetBaz"}, RecordType: "A", RecordTTL: 0, Labels: endpoint.NewLabels()},
		{DNSName: "baz.com", Targets: []string{"txt"}, RecordType: "TXT", RecordTTL: 0, Labels: endpoint.NewLabels()},
		{DNSName: "api.baz.com", Targets: []string{"targetBaz"}, RecordType: "A", RecordTTL: 0, Labels: endpoint.NewLabels()},
		{DNSName: "api.baz.com", Targets: []string{"txt"}, RecordType: "TXT", RecordTTL: 0, Labels: endpoint.NewLabels()},
	}

	mockDomainClient.AssertExpectations(t)
	assert.Equal(t, expected, actual)
}

func TestLinodeApplyChanges(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	// Dummy Data
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	// For updates: checking if foo.com A exists
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     11,
		Type:   linodego.RecordTypeA,
		Name:   "",
		Target: "targetFoo",
	}}, nil).Once()

	// For deletes: checking if api.baz.com A exists and api.baz.com TXT exists
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		3,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     33,
		Type:   linodego.RecordTypeA,
		Name:   "api",
		Target: "targetBaz",
	}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		3,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     34,
		Type:   linodego.RecordTypeTXT,
		Name:   "api",
		Target: "txt",
	}}, nil).Once()

	// Apply Actions
	mockDomainClient.On(
		"DeleteDomainRecord",
		mock.Anything,
		3,
		33,
	).Return(nil).Once()

	mockDomainClient.On(
		"DeleteDomainRecord",
		mock.Anything,
		3,
		34,
	).Return(nil).Once()

	mockDomainClient.On(
		"UpdateDomainRecord",
		mock.Anything,
		1,
		11,
		linodego.DomainRecordUpdateOptions{
			Type: "A", Name: "", Target: "targetFoo",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeA), Port: getPort(), TTLSec: 300,
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		2,
		linodego.DomainRecordCreateOptions{
			Type: "A", Name: "create", Target: "targetBar",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeA), Port: getPort(), TTLSec: 0,
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		2,
		linodego.DomainRecordCreateOptions{
			Type: "A", Name: "", Target: "targetBar",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeA), Port: getPort(), TTLSec: 0,
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		linodego.DomainRecordCreateOptions{
			Type: "TXT", Name: "", Target: "txt",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeTXT), Port: getPort(), TTLSec: 0,
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "create.bar.io",
			RecordType: "A",
			Targets:    []string{"targetBar"},
		}, {
			DNSName:    "bar.io",
			RecordType: "A",
			Targets:    []string{"targetBar"},
		}, {
			DNSName:    "foo.com",
			RecordType: "TXT",
			Targets:    []string{"txt"},
		}},
		Delete: []*endpoint.Endpoint{{
			DNSName:    "api.baz.com",
			RecordType: "A",
		}, {
			DNSName:    "api.baz.com",
			RecordType: "TXT",
		}},
		UpdateNew: []*endpoint.Endpoint{{
			DNSName:    "foo.com",
			RecordType: "A",
			RecordTTL:  300,
			Targets:    []string{"targetFoo"},
		}},
		UpdateOld: []*endpoint.Endpoint{},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChangesTargetAdded(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	// Dummy Data
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// With X-Filter, query for existing A record with empty name
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{ID: 11, Name: "", Type: "A", Target: "targetA"}}, nil).Once()

	// Apply Actions
	mockDomainClient.On(
		"UpdateDomainRecord",
		mock.Anything,
		1,
		11,
		linodego.DomainRecordUpdateOptions{
			Type: "A", Name: "", Target: "targetA",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeA), Port: getPort(),
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		linodego.DomainRecordCreateOptions{
			Type: "A", Name: "", Target: "targetB",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeA), Port: getPort(),
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		// From 1 target to 2
		UpdateNew: []*endpoint.Endpoint{{
			DNSName:    "example.com",
			RecordType: "A",
			Targets:    []string{"targetA", "targetB"},
		}},
		UpdateOld: []*endpoint.Endpoint{},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChangesTargetRemoved(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	// Dummy Data
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// With X-Filter, query for existing A record with empty name
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{ID: 11, Name: "", Type: "A", Target: "targetA"}, {ID: 12, Type: "A", Name: "", Target: "targetB"}}, nil).Once()

	// Apply Actions
	mockDomainClient.On(
		"UpdateDomainRecord",
		mock.Anything,
		1,
		12,
		linodego.DomainRecordUpdateOptions{
			Type: "A", Name: "", Target: "targetB",
			Priority: getPriority(), Weight: getWeight(linodego.RecordTypeA), Port: getPort(),
		},
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"DeleteDomainRecord",
		mock.Anything,
		1,
		11,
	).Return(nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		// From 2 targets to 1
		UpdateNew: []*endpoint.Endpoint{{
			DNSName:    "example.com",
			RecordType: "A",
			Targets:    []string{"targetB"},
		}},
		UpdateOld: []*endpoint.Endpoint{},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChangesNoChanges(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		DryRun:                false,
	}

	// Dummy Data
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// No ListDomainRecords calls expected since there are no changes

	err := provider.ApplyChanges(context.Background(), &plan.Changes{})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithManagedTypes(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "CNAME"},
		excludeDNSRecordTypes: []string{},
		registry:              "noop",
		DryRun:                false,
	}

	// Expect ListDomainRecords to be called with X-Filter for A and CNAME types
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify that the X-Filter is set and contains the expected record types
			if opts == nil || opts.Filter == "" {
				return false
			}
			// The filter should contain both A and CNAME
			return opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithExcludedTypes(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "AAAA", "CNAME", "TXT"},
		excludeDNSRecordTypes: []string{"TXT"},
		registry:              "noop",
		DryRun:                false,
	}

	// Expect ListDomainRecords to be called with X-Filter excluding TXT
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify that the X-Filter is set
			if opts == nil || opts.Filter == "" {
				return false
			}
			// The filter should not include TXT since it's excluded
			return opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithAllSupportedTypes(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "AAAA", "CNAME", "SRV", "TXT", "NS"},
		excludeDNSRecordTypes: []string{},
		registry:              "noop",
		DryRun:                false,
	}

	// When all supported types are managed, no filter should be applied (empty filter string)
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify that no X-Filter is set when all types are managed
			return opts != nil && opts.Filter == ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithTXTRegistryAddsTXT(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "CNAME"},
		excludeDNSRecordTypes: []string{},
		registry:              "txt",
		DryRun:                false,
	}

	// TXT should be added to the filter automatically when using TXT registry
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify that the X-Filter includes TXT even though it wasn't in managedRecordTypes
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 0)

	mockDomainClient.AssertExpectations(t)
}

// Domain Filtering X-Filter Tests

func TestLinodeFetchZonesWithSingleExactDomain(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{"foo.com"}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		domainFilterData: dfd{
			Include: []string{"foo.com"},
		},
		DryRun: false,
	}

	// Should use X-Filter with single exact match
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter is set for exact domain match
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.Domain{{ID: 1, Domain: "foo.com"}}, nil).Once()

	zones, err := provider.fetchZones(context.Background())
	require.NoError(t, err)
	assert.Len(t, zones, 1)
	assert.Equal(t, "foo.com", zones[0].Domain)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchZonesWithMultipleExactDomains(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{"foo.com", "bar.io"}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		domainFilterData: dfd{
			Include: []string{"foo.com", "bar.io"},
		},
		DryRun: false,
	}

	// Should use X-Filter with OR for multiple domains
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter is set with multiple domains
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.Domain{
		{ID: 1, Domain: "foo.com"},
		{ID: 2, Domain: "bar.io"},
	}, nil).Once()

	zones, err := provider.fetchZones(context.Background())
	require.NoError(t, err)
	assert.Len(t, zones, 2)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchZonesWithExcludeDomains(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{"foo.com", "baz.com"}), // Include some domains
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		domainFilterData: dfd{
			Include: []string{"foo.com", "baz.com"},
			Exclude: []string{"bar.io"},
		},
		DryRun: false,
	}

	// With both include and exclude and multiple includes, it can't be expressed in X-Filter
	// Should fall back to fetching all zones client-side
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Complex filter falls back to empty filter
			return opts != nil && opts.Filter == ""
		}),
	).Return(createZones(), nil).Once()

	zones, err := provider.fetchZones(context.Background())
	require.NoError(t, err)
	// Should filter to only foo.com and baz.com (excluding bar.io) client-side
	assert.Len(t, zones, 2)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchZonesWithSimpleExclude(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{"foo.com"}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		domainFilterData: dfd{
			Include: []string{"foo.com"},
			Exclude: []string{"bar.io"},
		},
		DryRun: false,
	}

	// Single include with excludes can be expressed in X-Filter
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter is set
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.Domain{
		{ID: 1, Domain: "foo.com"},
	}, nil).Once()

	zones, err := provider.fetchZones(context.Background())
	require.NoError(t, err)
	assert.Len(t, zones, 1)
	assert.Equal(t, "foo.com", zones[0].Domain)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchZonesWithRegexFilter(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{".com"}), // wildcard triggers regex
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		domainFilterData: dfd{
			Include: []string{".com"},
		},
		DryRun: false,
	}

	// Should NOT use X-Filter due to wildcard, fetch all and apply client-side
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify NO X-Filter is set due to wildcard
			return opts != nil && opts.Filter == ""
		}),
	).Return(createZones(), nil).Once()

	zones, err := provider.fetchZones(context.Background())
	require.NoError(t, err)
	// Should filter to only .com domains client-side
	assert.Len(t, zones, 2)

	mockDomainClient.AssertExpectations(t)
}

// Record Type Filter Edge Cases

func TestLinodeFetchRecordsWithEmptyManagedTypes(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{},
		excludeDNSRecordTypes: []string{},
		registry:              "noop",
		DryRun:                false,
	}

	// Empty managed types should fetch all records (no X-Filter)
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify NO X-Filter when managed types is empty
			return opts != nil && opts.Filter == ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithSingleManagedType(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A"},
		excludeDNSRecordTypes: []string{},
		registry:              "noop",
		DryRun:                false,
	}

	// Single type should use simple equality X-Filter
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter is set for single type
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithAllTypesExcluded(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "TXT"},
		excludeDNSRecordTypes: []string{"A", "TXT"},
		registry:              "noop",
		DryRun:                false,
	}

	// All types excluded should return empty filter
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify NO X-Filter when all types excluded
			return opts != nil && opts.Filter == ""
		}),
	).Return([]linodego.DomainRecord{}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 0)

	mockDomainClient.AssertExpectations(t)
}

// Registry + Managed Types Interaction Tests

func TestLinodeFetchRecordsWithTXTRegistryAndTXTInManaged(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "TXT"},
		excludeDNSRecordTypes: []string{},
		registry:              "txt",
		DryRun:                false,
	}

	// TXT already in managed types, should not duplicate
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter includes A and TXT
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithTXTRegistryAndTXTExcluded(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "TXT"},
		excludeDNSRecordTypes: []string{"TXT"},
		registry:              "txt",
		DryRun:                false,
	}

	// TXT is excluded but needed for registry - exclude takes precedence
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter only includes A (TXT excluded)
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 1)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecordsWithDynamoDBRegistry(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:                &mockDomainClient,
		domainFilter:          endpoint.NewDomainFilter([]string{}),
		managedRecordTypes:    []string{"A", "CNAME"},
		excludeDNSRecordTypes: []string{},
		registry:              "dynamodb",
		DryRun:                false,
	}

	// DynamoDB registry should also add TXT
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			// Verify X-Filter includes A, CNAME, and TXT
			return opts != nil && opts.Filter != ""
		}),
	).Return([]linodego.DomainRecord{}, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, records, 0)

	mockDomainClient.AssertExpectations(t)
}

// Constructor Validation Tests

func TestNewLinodeProviderWithTXTRegistry(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	defer os.Unsetenv("LINODE_TOKEN")

	provider, err := NewLinodeProvider(
		endpoint.NewDomainFilter([]string{}),
		[]string{"A", "CNAME"},
		[]string{},
		"txt",
		false,
	)
	require.NoError(t, err)

	// TXT should be automatically added
	assert.Contains(t, provider.managedRecordTypes, "TXT")
	assert.Contains(t, provider.managedRecordTypes, "A")
	assert.Contains(t, provider.managedRecordTypes, "CNAME")
}

func TestNewLinodeProviderWithDynamoDBRegistry(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	defer os.Unsetenv("LINODE_TOKEN")

	provider, err := NewLinodeProvider(
		endpoint.NewDomainFilter([]string{}),
		[]string{"A", "CNAME"},
		[]string{},
		"dynamodb",
		false,
	)
	require.NoError(t, err)

	// TXT should be automatically added for DynamoDB too
	assert.Contains(t, provider.managedRecordTypes, "TXT")
	assert.Contains(t, provider.managedRecordTypes, "A")
	assert.Contains(t, provider.managedRecordTypes, "CNAME")
}

func TestNewLinodeProviderWithNoopRegistry(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	defer os.Unsetenv("LINODE_TOKEN")

	provider, err := NewLinodeProvider(
		endpoint.NewDomainFilter([]string{}),
		[]string{"A", "CNAME"},
		[]string{},
		"noop",
		false,
	)
	require.NoError(t, err)

	// TXT should NOT be added for noop registry
	assert.NotContains(t, provider.managedRecordTypes, "TXT")
	assert.Contains(t, provider.managedRecordTypes, "A")
	assert.Contains(t, provider.managedRecordTypes, "CNAME")
}

func TestNewLinodeProviderWithLowercaseRecordTypes(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	defer os.Unsetenv("LINODE_TOKEN")

	provider, err := NewLinodeProvider(
		endpoint.NewDomainFilter([]string{}),
		[]string{"a", "cname", "txt"},
		[]string{},
		"noop",
		false,
	)
	require.NoError(t, err)

	// Record types should be uppercased
	assert.Contains(t, provider.managedRecordTypes, "A")
	assert.Contains(t, provider.managedRecordTypes, "CNAME")
	assert.Contains(t, provider.managedRecordTypes, "TXT")
	assert.NotContains(t, provider.managedRecordTypes, "a")
	assert.NotContains(t, provider.managedRecordTypes, "cname")
}

func TestNewLinodeProviderWithLowercaseExcludedTypes(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	defer os.Unsetenv("LINODE_TOKEN")

	provider, err := NewLinodeProvider(
		endpoint.NewDomainFilter([]string{}),
		[]string{"A", "CNAME", "TXT"},
		[]string{"txt", "ns"},
		"noop",
		false,
	)
	require.NoError(t, err)

	// Excluded types should be uppercased
	assert.Contains(t, provider.excludeDNSRecordTypes, "TXT")
	assert.Contains(t, provider.excludeDNSRecordTypes, "NS")
	assert.NotContains(t, provider.excludeDNSRecordTypes, "txt")
	assert.NotContains(t, provider.excludeDNSRecordTypes, "ns")
}

func TestNewLinodeProviderWithTXTAlreadyInManagedTypes(t *testing.T) {
	_ = os.Setenv("LINODE_TOKEN", "xxxxxxxxxxxxxxxxx")
	defer os.Unsetenv("LINODE_TOKEN")

	provider, err := NewLinodeProvider(
		endpoint.NewDomainFilter([]string{}),
		[]string{"A", "TXT"},
		[]string{},
		"txt",
		false,
	)
	require.NoError(t, err)

	// TXT should not be duplicated
	txtCount := 0
	for _, recordType := range provider.managedRecordTypes {
		if recordType == "TXT" {
			txtCount++
		}
	}
	assert.Equal(t, 1, txtCount, "TXT should only appear once in managedRecordTypes")
}
