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
	"sigs.k8s.io/external-dns/provider"
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
	_, err := NewLinodeProvider(endpoint.NewDomainFilter([]string{"ext-dns-test.zalando.to."}), true)
	require.NoError(t, err)

	_ = os.Unsetenv("LINODE_TOKEN")
	_, err = NewLinodeProvider(endpoint.NewDomainFilter([]string{"ext-dns-test.zalando.to."}), true)
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{".com"}),
		DryRun:       false,
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	// Dummy Data
	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	// With X-Filter, ListDomainRecords is now called with specific filters for each endpoint
	// For creates: checking if foo.com TXT exists, create.bar.io A exists, bar.io A exists
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     12,
		Type:   linodego.RecordTypeTXT,
		Name:   "",
		Target: "txt",
	}}, nil).Once() // foo.com TXT record exists

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		2,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once() // create.bar.io A doesn't exist

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		2,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once() // bar.io A doesn't exist

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
			// This record should be skipped as it already exists
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
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
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
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

// Error Handling Tests

func TestLinodeRecords_ListDomainsError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{}, assert.AnError).Once()

	_, err := provider.Records(context.Background())
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeRecords_ListDomainRecordsError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
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
	).Return([]linodego.DomainRecord{}, assert.AnError).Once()

	_, err := provider.Records(context.Background())
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_FetchZonesError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{}, assert.AnError).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_CreateRecordError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.Anything,
	).Return(&linodego.DomainRecord{}, assert.AnError).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.NoError(t, err) // submitChanges logs errors but doesn't return them

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_UpdateRecordError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     11,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	mockDomainClient.On(
		"UpdateDomainRecord",
		mock.Anything,
		1,
		11,
		mock.Anything,
	).Return(&linodego.DomainRecord{}, assert.AnError).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		UpdateNew: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.NoError(t, err) // submitChanges logs errors but doesn't return them

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_DeleteRecordError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     11,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	mockDomainClient.On(
		"DeleteDomainRecord",
		mock.Anything,
		1,
		11,
	).Return(assert.AnError).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
		}},
	})
	require.NoError(t, err) // submitChanges logs errors but doesn't return them

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_InvalidRecordType(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// The error should occur during getRecordIDFiltered when trying to convert the record type
	// So we don't need to set up ListDomainRecords expectation

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "INVALID",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

// Helper Function Tests

func TestGetWeight(t *testing.T) {
	// Test non-NS record types
	weight := getWeight(linodego.RecordTypeA)
	assert.NotNil(t, weight)
	assert.Equal(t, 1, *weight)

	weight = getWeight(linodego.RecordTypeAAAA)
	assert.NotNil(t, weight)
	assert.Equal(t, 1, *weight)

	weight = getWeight(linodego.RecordTypeCNAME)
	assert.NotNil(t, weight)
	assert.Equal(t, 1, *weight)

	weight = getWeight(linodego.RecordTypeTXT)
	assert.NotNil(t, weight)
	assert.Equal(t, 1, *weight)

	// Test NS record type
	weight = getWeight(linodego.RecordTypeNS)
	assert.NotNil(t, weight)
	assert.Equal(t, 0, *weight)
}

func TestGetPort(t *testing.T) {
	port := getPort()
	assert.NotNil(t, port)
	assert.Equal(t, 0, *port)
}

func TestGetPriority(t *testing.T) {
	priority := getPriority()
	assert.NotNil(t, priority)
	assert.Equal(t, 0, *priority)
}

func TestGetRecordID_NoMatches(t *testing.T) {
	zone := linodego.Domain{Domain: "example.com", ID: 1}
	records := []linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "www", Target: "1.2.3.4"},
	}
	ep := endpoint.Endpoint{DNSName: "api.example.com", RecordType: "A"}

	matched := getRecordID(records, zone, ep)
	assert.Empty(t, matched)
}

func TestGetRecordID_SingleMatch(t *testing.T) {
	zone := linodego.Domain{Domain: "example.com", ID: 1}
	records := []linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "www", Target: "1.2.3.4"},
		{ID: 2, Type: linodego.RecordTypeA, Name: "api", Target: "5.6.7.8"},
	}
	ep := endpoint.Endpoint{DNSName: "api.example.com", RecordType: "A"}

	matched := getRecordID(records, zone, ep)
	assert.Len(t, matched, 1)
	assert.Equal(t, 2, matched[0].ID)
	assert.Equal(t, "api", matched[0].Name)
}

func TestGetRecordID_MultipleMatches(t *testing.T) {
	zone := linodego.Domain{Domain: "example.com", ID: 1}
	records := []linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "www", Target: "1.2.3.4"},
		{ID: 2, Type: linodego.RecordTypeA, Name: "www", Target: "5.6.7.8"},
	}
	ep := endpoint.Endpoint{DNSName: "www.example.com", RecordType: "A"}

	matched := getRecordID(records, zone, ep)
	assert.Len(t, matched, 2)
}

func TestGetRecordID_RootRecord(t *testing.T) {
	zone := linodego.Domain{Domain: "example.com", ID: 1}
	records := []linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "", Target: "1.2.3.4"},
		{ID: 2, Type: linodego.RecordTypeA, Name: "www", Target: "5.6.7.8"},
	}
	ep := endpoint.Endpoint{DNSName: "example.com", RecordType: "A"}

	matched := getRecordID(records, zone, ep)
	assert.Len(t, matched, 1)
	assert.Equal(t, 1, matched[0].ID)
	assert.Equal(t, "", matched[0].Name)
}

func TestEndpointsByZone_NoMatchingZone(t *testing.T) {
	zoneNameIDMapper := provider.ZoneIDName{}
	zoneNameIDMapper.Add("1", "example.com")

	endpoints := []*endpoint.Endpoint{
		{DNSName: "test.other.com", RecordType: "A"},
	}

	result := endpointsByZone(zoneNameIDMapper, endpoints)
	assert.Empty(t, result)
}

func TestEndpointsByZone_SingleZone(t *testing.T) {
	zoneNameIDMapper := provider.ZoneIDName{}
	zoneNameIDMapper.Add("1", "example.com")

	endpoints := []*endpoint.Endpoint{
		{DNSName: "test.example.com", RecordType: "A"},
		{DNSName: "api.example.com", RecordType: "A"},
	}

	result := endpointsByZone(zoneNameIDMapper, endpoints)
	assert.Len(t, result, 1)
	assert.Len(t, result["1"], 2)
}

func TestEndpointsByZone_MultipleZones(t *testing.T) {
	zoneNameIDMapper := provider.ZoneIDName{}
	zoneNameIDMapper.Add("1", "example.com")
	zoneNameIDMapper.Add("2", "other.com")

	endpoints := []*endpoint.Endpoint{
		{DNSName: "test.example.com", RecordType: "A"},
		{DNSName: "api.other.com", RecordType: "A"},
	}

	result := endpointsByZone(zoneNameIDMapper, endpoints)
	assert.Len(t, result, 2)
	assert.Len(t, result["1"], 1)
	assert.Len(t, result["2"], 1)
}

// Filtered Query Tests

func TestFetchRecordsFiltered_Success(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	expectedRecords := []linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "test", Target: "1.2.3.4"},
	}

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts *linodego.ListOptions) bool {
			return opts != nil && opts.Filter != ""
		}),
	).Return(expectedRecords, nil).Once()

	records, err := provider.fetchRecordsFiltered(context.Background(), 1, "test", linodego.RecordTypeA)
	require.NoError(t, err)
	assert.Equal(t, expectedRecords, records)

	mockDomainClient.AssertExpectations(t)
}

func TestFetchRecordsFiltered_Error(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, assert.AnError).Once()

	_, err := provider.fetchRecordsFiltered(context.Background(), 1, "test", linodego.RecordTypeA)
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestGetRecordIDFiltered_Success(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}
	ep := endpoint.Endpoint{DNSName: "test.example.com", RecordType: "A"}

	expectedRecords := []linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "test", Target: "1.2.3.4"},
	}

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return(expectedRecords, nil).Once()

	records, err := provider.getRecordIDFiltered(context.Background(), 1, zone, ep)
	require.NoError(t, err)
	assert.Equal(t, expectedRecords, records)

	mockDomainClient.AssertExpectations(t)
}

func TestGetRecordIDFiltered_ConvertRecordTypeError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}
	ep := endpoint.Endpoint{DNSName: "test.example.com", RecordType: "INVALID"}

	_, err := provider.getRecordIDFiltered(context.Background(), 1, zone, ep)
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestGetRecordIDFiltered_FetchError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}
	ep := endpoint.Endpoint{DNSName: "test.example.com", RecordType: "A"}

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, assert.AnError).Once()

	_, err := provider.getRecordIDFiltered(context.Background(), 1, zone, ep)
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

// submitChanges Tests

func TestSubmitChanges_DryRun(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       true,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}

	changes := LinodeChanges{
		Creates: []LinodeChangeCreate{{
			Domain:  zone,
			Options: linodego.DomainRecordCreateOptions{Type: "A", Name: "test", Target: "1.2.3.4"},
		}},
		Updates: []LinodeChangeUpdate{{
			Domain:       zone,
			DomainRecord: linodego.DomainRecord{ID: 1, Type: "A", Name: "test", Target: "1.2.3.4"},
			Options:      linodego.DomainRecordUpdateOptions{Type: "A", Name: "test", Target: "5.6.7.8"},
		}},
		Deletes: []LinodeChangeDelete{{
			Domain:       zone,
			DomainRecord: linodego.DomainRecord{ID: 2, Type: "A", Name: "old", Target: "9.10.11.12"},
		}},
	}

	// No mock expectations since DryRun should not call the API
	err := provider.submitChanges(context.Background(), changes)
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestSubmitChanges_EmptyChanges(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	changes := LinodeChanges{
		Creates: []LinodeChangeCreate{},
		Updates: []LinodeChangeUpdate{},
		Deletes: []LinodeChangeDelete{},
	}

	err := provider.submitChanges(context.Background(), changes)
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestSubmitChanges_CreateSuccess(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}
	createOpts := linodego.DomainRecordCreateOptions{
		Type:   "A",
		Name:   "test",
		Target: "1.2.3.4",
	}

	changes := LinodeChanges{
		Creates: []LinodeChangeCreate{{
			Domain:  zone,
			Options: createOpts,
		}},
	}

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		createOpts,
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.submitChanges(context.Background(), changes)
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestSubmitChanges_UpdateSuccess(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}
	updateOpts := linodego.DomainRecordUpdateOptions{
		Type:   "A",
		Name:   "test",
		Target: "5.6.7.8",
	}

	changes := LinodeChanges{
		Updates: []LinodeChangeUpdate{{
			Domain:       zone,
			DomainRecord: linodego.DomainRecord{ID: 1, Type: "A", Name: "test", Target: "1.2.3.4"},
			Options:      updateOpts,
		}},
	}

	mockDomainClient.On(
		"UpdateDomainRecord",
		mock.Anything,
		1,
		1,
		updateOpts,
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.submitChanges(context.Background(), changes)
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestSubmitChanges_DeleteSuccess(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	zone := linodego.Domain{Domain: "example.com", ID: 1}

	changes := LinodeChanges{
		Deletes: []LinodeChangeDelete{{
			Domain:       zone,
			DomainRecord: linodego.DomainRecord{ID: 1, Type: "A", Name: "test", Target: "1.2.3.4"},
		}},
	}

	mockDomainClient.On(
		"DeleteDomainRecord",
		mock.Anything,
		1,
		1,
	).Return(nil).Once()

	err := provider.submitChanges(context.Background(), changes)
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

// ApplyChanges Edge Cases

func TestLinodeApplyChanges_UpdateRecordNotFound(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// Return empty records - record to update doesn't exist
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once()

	// Since record doesn't exist, it will create a new one
	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.Anything,
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		UpdateNew: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_DeleteRecordNotFound(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// Return empty records - record to delete doesn't exist
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
		}},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_TTLHandling(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.TTLSec == 600
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
			RecordTTL:  600,
		}},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_DryRun(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       true,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Once()

	// No Create/Update/Delete expectations since it's dry run

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_AllRecordTypes(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// Mock ListDomainRecords for each record type check (6 times)
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, nil).Times(6)

	// Expect CreateDomainRecord for each type
	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.Type == linodego.RecordTypeA
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.Type == linodego.RecordTypeAAAA
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.Type == linodego.RecordTypeCNAME
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.Type == linodego.RecordTypeTXT
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.Type == linodego.RecordTypeSRV
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	mockDomainClient.On(
		"CreateDomainRecord",
		mock.Anything,
		1,
		mock.MatchedBy(func(opts linodego.DomainRecordCreateOptions) bool {
			return opts.Type == linodego.RecordTypeNS && *opts.Weight == 0
		}),
	).Return(&linodego.DomainRecord{}, nil).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			{DNSName: "a.example.com", RecordType: "A", Targets: []string{"1.2.3.4"}},
			{DNSName: "aaaa.example.com", RecordType: "AAAA", Targets: []string{"::1"}},
			{DNSName: "cname.example.com", RecordType: "CNAME", Targets: []string{"target.example.com"}},
			{DNSName: "txt.example.com", RecordType: "TXT", Targets: []string{"text"}},
			{DNSName: "srv.example.com", RecordType: "SRV", Targets: []string{"target"}},
			{DNSName: "ns.example.com", RecordType: "NS", Targets: []string{"ns1.example.com"}},
		},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_RecordAlreadyExists(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	// Return existing record
	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{{
		ID:     1,
		Type:   linodego.RecordTypeA,
		Name:   "test",
		Target: "1.2.3.4",
	}}, nil).Once()

	// No CreateDomainRecord should be called since record already exists

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.NoError(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeApplyChanges_GetRecordIDFilteredError(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, assert.AnError).Once()

	err := provider.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{{
			DNSName:    "test.example.com",
			RecordType: "A",
			Targets:    []string{"1.2.3.4"},
		}},
	})
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

// Records Method Edge Cases

func TestLinodeRecords_EmptyZones(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{}, nil).Once()

	actual, err := provider.Records(context.Background())
	require.NoError(t, err)
	assert.Empty(t, actual)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeRecords_UnsupportedRecordType(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeCAA, Name: "", Target: "ca.example.com"},
		{ID: 2, Type: linodego.RecordTypeA, Name: "", Target: "1.2.3.4"},
	}, nil).Once()

	actual, err := provider.Records(context.Background())
	require.NoError(t, err)
	// Only A record should be returned, CAA is unsupported
	assert.Len(t, actual, 1)
	assert.Equal(t, "A", actual[0].RecordType)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeRecords_TTLPreservation(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "test", Target: "1.2.3.4", TTLSec: 600},
	}, nil).Once()

	actual, err := provider.Records(context.Background())
	require.NoError(t, err)
	assert.Len(t, actual, 1)
	assert.Equal(t, endpoint.TTL(600), actual[0].RecordTTL)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeRecords_RootRecords(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{{Domain: "example.com", ID: 1}}, nil).Once()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{
		{ID: 1, Type: linodego.RecordTypeA, Name: "", Target: "1.2.3.4"},
		{ID: 2, Type: linodego.RecordTypeA, Name: "www", Target: "5.6.7.8"},
	}, nil).Once()

	actual, err := provider.Records(context.Background())
	require.NoError(t, err)
	assert.Len(t, actual, 2)
	assert.Equal(t, "example.com", actual[0].DNSName)
	assert.Equal(t, "www.example.com", actual[1].DNSName)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeRecords_WithDomainFilter(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{"foo.com"}),
		DryRun:       false,
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

	actual, err := provider.Records(context.Background())
	require.NoError(t, err)
	// Should only return records from foo.com (filtered)
	assert.Len(t, actual, 2) // 2 supported records from foo.com
	for _, ep := range actual {
		assert.Contains(t, ep.DNSName, "foo.com")
	}

	mockDomainClient.AssertExpectations(t)
}

// Zones Method Tests

func TestLinodeZones_Success(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)
	assert.Equal(t, createZones(), zones)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeZones_Error(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{}, assert.AnError).Once()

	_, err := provider.Zones(context.Background())
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeZones_EmptyResult(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return([]linodego.Domain{}, nil).Once()

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)
	assert.Empty(t, zones)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeZones_WithFilter(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{".com"}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomains",
		mock.Anything,
		mock.Anything,
	).Return(createZones(), nil).Once()

	zones, err := provider.Zones(context.Background())
	require.NoError(t, err)
	assert.Len(t, zones, 2)
	// Should only return foo.com and baz.com, not bar.io
	for _, zone := range zones {
		assert.True(t, zone.Domain == "foo.com" || zone.Domain == "baz.com")
	}

	mockDomainClient.AssertExpectations(t)
}

// fetchRecords Tests

func TestLinodeFetchRecords_Success(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	expectedRecords := createFooRecords()

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return(expectedRecords, nil).Once()

	records, err := provider.fetchRecords(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, expectedRecords, records)

	mockDomainClient.AssertExpectations(t)
}

func TestLinodeFetchRecords_Error(t *testing.T) {
	mockDomainClient := MockDomainClient{}

	provider := &LinodeProvider{
		Client:       &mockDomainClient,
		domainFilter: endpoint.NewDomainFilter([]string{}),
		DryRun:       false,
	}

	mockDomainClient.On(
		"ListDomainRecords",
		mock.Anything,
		1,
		mock.Anything,
	).Return([]linodego.DomainRecord{}, assert.AnError).Once()

	_, err := provider.fetchRecords(context.Background(), 1)
	require.Error(t, err)

	mockDomainClient.AssertExpectations(t)
}
