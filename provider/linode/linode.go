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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/linode/linodego"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"

	"sigs.k8s.io/external-dns/registry"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"

	"sigs.k8s.io/external-dns/pkg/apis/externaldns"
)

// LinodeDomainClient interface to ease testing
type LinodeDomainClient interface {
	ListDomainRecords(ctx context.Context, domainID int, opts *linodego.ListOptions) ([]linodego.DomainRecord, error)
	ListDomains(ctx context.Context, opts *linodego.ListOptions) ([]linodego.Domain, error)
	CreateDomainRecord(ctx context.Context, domainID int, domainrecord linodego.DomainRecordCreateOptions) (*linodego.DomainRecord, error)
	DeleteDomainRecord(ctx context.Context, domainID int, id int) error
	UpdateDomainRecord(ctx context.Context, domainID int, id int, domainrecord linodego.DomainRecordUpdateOptions) (*linodego.DomainRecord, error)
}

// LinodeProvider is an implementation of Provider for Digital Ocean's DNS.
type LinodeProvider struct {
	provider.BaseProvider
	Client                LinodeDomainClient
	domainFilter          *endpoint.DomainFilter
	managedRecordTypes    []string
	excludeDNSRecordTypes []string
	registry              string
	domainFilterData      dfd
	DryRun                bool
}

// LinodeChanges All API calls calculated from the plan
type LinodeChanges struct {
	Creates []LinodeChangeCreate
	Deletes []LinodeChangeDelete
	Updates []LinodeChangeUpdate
}

// LinodeChangeCreate Linode Domain Record Creates
type LinodeChangeCreate struct {
	Domain  linodego.Domain
	Options linodego.DomainRecordCreateOptions
}

// LinodeChangeUpdate Linode Domain Record Updates
type LinodeChangeUpdate struct {
	Domain       linodego.Domain
	DomainRecord linodego.DomainRecord
	Options      linodego.DomainRecordUpdateOptions
}

// LinodeChangeDelete Linode Domain Record Deletes
type LinodeChangeDelete struct {
	Domain       linodego.Domain
	DomainRecord linodego.DomainRecord
}

type dfd struct {
	Include      []string `json:"include,omitempty"`
	Exclude      []string `json:"exclude,omitempty"`
	RegexInclude string   `json:"regexInclude,omitempty"`
	RegexExclude string   `json:"regexExclude,omitempty"`
}

// NewLinodeProvider initializes a new Linode DNS based Provider.
func NewLinodeProvider(domainFilter *endpoint.DomainFilter, managedRecordTypes, excludedRecordTypes []string, registryConfig string, dryRun bool) (*LinodeProvider, error) {
	token, ok := os.LookupEnv("LINODE_TOKEN")
	if !ok {
		return nil, fmt.Errorf("no token found")
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	linodeClient := linodego.NewClient(oauth2Client)
	linodeClient.SetUserAgent(fmt.Sprintf("%s linodego/%s", externaldns.UserAgent(), linodego.Version))

	var upperManaged []string
	if len(managedRecordTypes) > 0 {
		for _, recordType := range managedRecordTypes { // confirm all input was uppercase
			upperManaged = append(upperManaged, strings.ToUpper(recordType))
		}

		// Both registries require TXT records, TXT to lookup ownership and dynamodb to migrate
		if registryConfig != registry.NOOP && !slices.Contains(upperManaged, "TXT") {
			upperManaged = append(upperManaged, "TXT")
		}
	}

	var upperExcluded []string
	if len(excludedRecordTypes) > 0 {
		upperExcluded = make([]string, 0, len(excludedRecordTypes))
		for _, recordType := range excludedRecordTypes {
			upperExcluded = append(upperExcluded, strings.ToUpper(recordType))
		}
	}

	var domainFilterData dfd
	if domainFilter != nil {
		// Extract both includes and excludes by marshaling and unmarshaling the domain filter because no exported members
		filterData, err := domainFilter.MarshalJSON()
		if err != nil {
			log.WithFields(log.Fields{
				"filterData": string(filterData),
			}).Debug("Error unmarshaling domain filter data.")
			return nil, err
		}

		log.WithFields(log.Fields{
			"filterData": string(filterData),
		}).Debug("Domain filter data.")

		if err := json.Unmarshal(filterData, &domainFilterData); err != nil {
			log.WithFields(log.Fields{
				"domainFilterData": domainFilterData,
			}).Debug("Error unmarshaling domain filter data.")
			return nil, err
		}
	}

	return &LinodeProvider{
		Client:                &linodeClient,
		domainFilter:          domainFilter,
		managedRecordTypes:    upperManaged,
		excludeDNSRecordTypes: upperExcluded,
		registry:              registryConfig,
		domainFilterData:      domainFilterData,
		DryRun:                dryRun,
	}, nil
}

// Zones return the list of hosted zones.
func (p *LinodeProvider) Zones(ctx context.Context) ([]linodego.Domain, error) {
	zones, err := p.fetchZones(ctx)
	if err != nil {
		return nil, err
	}

	return zones, nil
}

// Records returns the list of records in a given zone.
func (p *LinodeProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint

	for _, zone := range zones {
		records, err := p.fetchRecords(ctx, zone.ID)
		if err != nil {
			return nil, err
		}

		for _, r := range records {
			if provider.SupportedRecordType(string(r.Type)) {
				name := fmt.Sprintf("%s.%s", r.Name, zone.Domain)

				// root name is identified by the empty string and should be
				// translated to zone name for the endpoint entry.
				if r.Name == "" {
					name = zone.Domain
				}

				endpoints = append(endpoints, endpoint.NewEndpointWithTTL(name, string(r.Type), endpoint.TTL(r.TTLSec), r.Target))
			}
		}
	}

	return endpoints, nil
}

func (p *LinodeProvider) fetchRecords(ctx context.Context, domainID int) ([]linodego.DomainRecord, error) {
	// Build X-Filter for record types if configured
	filterStr, err := p.buildRecordTypeFilter()
	if err != nil {
		return nil, err
	}

	opts := linodego.NewListOptions(0, filterStr)
	records, err := p.Client.ListDomainRecords(ctx, domainID, opts)
	if err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{
		"domainID": domainID,
		"total":    len(records),
		"filter":   filterStr,
	}).Debug("Fetched records for domain.")

	return records, nil
}

// fetchRecordsFiltered fetches domain records filtered by name and type using X-Filter headers
// This significantly reduces API calls and network overhead by filtering server-side
func (p *LinodeProvider) fetchRecordsFiltered(ctx context.Context, domainID int, name string, recordType linodego.DomainRecordType) ([]linodego.DomainRecord, error) {
	// Use X-Filter to filter records on the Linode API side
	// Format: {"name": "value", "type": "value"}
	filterStr := fmt.Sprintf(`{"name": "%s", "type": "%s"}`, name, recordType)

	opts := linodego.NewListOptions(0, filterStr)
	records, err := p.Client.ListDomainRecords(ctx, domainID, opts)
	if err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{
		"domainID": domainID,
		"name":     name,
	}).Debug("Fetched records with x-filter for domain.")

	return records, nil
}

func (p *LinodeProvider) fetchZones(ctx context.Context) ([]linodego.Domain, error) {
	log.Debug("Fetching zones.")
	var zones []linodego.Domain

	// Build X-Filter combining includes and excludes
	filterStr, err := p.buildDomainFilter()
	if err != nil {
		return nil, err
	}

	if filterStr != "" {
		// We were able to express the domain filter as an X-Filter to the API and return a limited set of data
		log.Debug("Fetching zones using filter.")
		opts := linodego.NewListOptions(0, filterStr)
		zones, err = p.Client.ListDomains(ctx, opts)
		if err != nil {
			return nil, err
		}
	} else {
		// There is a regex or a wildcard or no filter at all, pull everything and attempt the domain filter match
		log.Debug("Unable to filter, fetching all zones and applying domain filter.")
		allZones, err := p.Client.ListDomains(ctx, linodego.NewListOptions(0, ""))
		if err != nil {
			return nil, err
		}

		// domain filter match will apply
		for _, zone := range allZones {
			if p.domainFilter == nil || p.domainFilter.Match(zone.Domain) {
				zones = append(zones, zone)
			}
		}
	}

	// Apply domain filter matching (handles wildcards and regex that can't be expressed in X-Filter)
	log.WithFields(log.Fields{
		"total": len(zones),
	}).Debug("Fetched zones on account.")

	return zones, nil
}

// buildDomainFilter constructs an X-Filter string combining includes and excludes
// Returns empty string if the filter contains wildcards or regex that can't be expressed in X-Filter
func (p *LinodeProvider) buildDomainFilter() (string, error) {
	if p.domainFilter == nil || !p.domainFilter.IsConfigured() {
		return "", nil
	}

	log.Debug("Domain filter is configured, trying to build domain X-Filter.")

	// If regex filters are present, we can't express them in X-Filter
	if p.domainFilterData.RegexInclude != "" || p.domainFilterData.RegexExclude != "" {
		log.WithFields(log.Fields{
			"regexInclude": p.domainFilterData.RegexInclude,
			"regexExclude": p.domainFilterData.RegexExclude,
		}).Debug("Can't express regex as an X-Filter and they take precedence in external-dns, return empty to fetch all.")
		return "", nil
	}

	// Check if includes contain wildcards or patterns
	for _, f := range p.domainFilterData.Include {
		if strings.HasPrefix(f, ".") || strings.Contains(f, "*") {
			log.WithFields(log.Fields{
				"include": p.domainFilterData.Include,
			}).Debug("Can't express the include wildcards as an X-Filter, return empty to fetch all.")
			return "", nil
		}
	}

	// Check if excludes contain wildcards or patterns
	for _, f := range p.domainFilterData.Exclude {
		if strings.HasPrefix(f, ".") || strings.Contains(f, "*") {
			log.WithFields(log.Fields{
				"exclude": p.domainFilterData.Exclude,
			}).Debug("Can't express exclude wildcards as an X-Filter, return empty to fetch all.")
			return "", nil
		}
	}

	var filter linodego.Filter
	includeCount := len(p.domainFilterData.Include)
	excludeCount := len(p.domainFilterData.Exclude)

	// No filters at all
	if includeCount == 0 && excludeCount == 0 {
		return "", nil
	}

	// Build the filter based on combinations of includes and excludes
	if includeCount > 0 && excludeCount > 0 {
		// Both includes and excludes: use +and combining all conditions
		// The logic is: (domain=A OR domain=B OR ...) AND (domain!=X) AND (domain!=Y) AND ...
		// We flatten this to: domain=A AND domain!=X AND domain!=Y OR domain=B AND domain!=X AND domain!=Y ...
		// For simplicity with X-Filter limitations, we use: +and with includes as +or, then +neq for each exclude

		filter.Operator = "+and"

		// Add all include conditions using OR
		if includeCount == 1 {
			filter.Children = append(filter.Children, &linodego.Comp{
				Column:   "domain",
				Operator: linodego.Eq,
				Value:    p.domainFilterData.Include[0],
			})
		} else {
			// Multiple includes - we need to add them with OR logic
			// But since we can't nest filters, we'll need to use a different approach
			// Add each include as a separate condition and rely on the +or at the parent level
			filter.Operator = "+or"
			for _, domain := range p.domainFilterData.Include {
				filter.Children = append(filter.Children, &linodego.Comp{
					Column:   "domain",
					Operator: linodego.Eq,
					Value:    domain,
				})
			}
			// Note: This means we can't combine complex includes with excludes in X-Filter
			// Fall back to client-side filtering via Match()
			log.Debug("Complex include+exclude filter detected, falling back to client-side filtering")
			return "", nil
		}

		// Add exclude conditions
		for _, domain := range p.domainFilterData.Exclude {
			filter.Children = append(filter.Children, &linodego.Comp{
				Column:   "domain",
				Operator: linodego.Neq,
				Value:    domain,
			})
		}
	} else if includeCount > 0 {
		// Only includes
		if includeCount == 1 {
			filter.AddField(linodego.Eq, "domain", p.domainFilterData.Include[0])
		} else {
			filter.Operator = "+or"
			for _, domain := range p.domainFilterData.Include {
				filter.Children = append(filter.Children, &linodego.Comp{
					Column:   "domain",
					Operator: linodego.Eq,
					Value:    domain,
				})
			}
		}
	} else {
		// Only excludes: use +and with all +neq conditions
		if excludeCount == 1 {
			filter.AddField(linodego.Neq, "domain", p.domainFilterData.Exclude[0])
		} else {
			filter.Operator = "+and"
			for _, domain := range p.domainFilterData.Exclude {
				filter.Children = append(filter.Children, &linodego.Comp{
					Column:   "domain",
					Operator: linodego.Neq,
					Value:    domain,
				})
			}
		}
	}

	xFilter, err := filter.MarshalJSON()
	if err != nil {
		return "", err
	}

	log.WithFields(log.Fields{
		"xFilter": string(xFilter),
	}).Debug("X-Filter Contents.")

	return string(xFilter), nil
}

// buildRecordTypeFilter constructs an X-Filter string for record types based on managedRecordTypes and excludeDNSRecordTypes
// Returns empty string if no filtering is needed
func (p *LinodeProvider) buildRecordTypeFilter() (string, error) {
	var filteredTypes []string
	for _, recordType := range p.managedRecordTypes {
		// If config excludes the same dns that is included
		if slices.Contains(p.excludeDNSRecordTypes, recordType) {
			continue
		}
		filteredTypes = append(filteredTypes, recordType)
	}

	// If no types remain after filtering, return empty string (fetch all records)
	if len(filteredTypes) == 0 {
		return "", nil
	}

	// If filteredTypes is just a list of all supported types then no filter
	allSupportedTypes := provider.GetSupportedRecordTypes()
	if len(filteredTypes) == len(allSupportedTypes) {
		slices.Sort(allSupportedTypes)
		slices.Sort(filteredTypes)

		if slices.Equal(allSupportedTypes, filteredTypes) {
			return "", nil
		}
	}

	// managed/excluded don't cancel each other out and managed is not the same as all supported, build a filter
	var filter linodego.Filter

	// Build filter based on number of types
	if len(filteredTypes) == 1 {
		// Single type - simple equality
		filter.AddField(linodego.Eq, "type", filteredTypes[0])
	} else {
		// Multiple types - use OR
		filter.Operator = "+or"
		for _, recordType := range filteredTypes {
			filter.Children = append(filter.Children, &linodego.Comp{
				Column:   "type",
				Operator: linodego.Eq,
				Value:    recordType,
			})
		}
	}

	filterBytes, err := filter.MarshalJSON()
	if err != nil {
		return "", err
	}

	return string(filterBytes), nil
}

// submitChanges takes a zone and a collection of Changes and sends them as a single transaction.
func (p *LinodeProvider) submitChanges(ctx context.Context, changes LinodeChanges) error {
	for _, change := range changes.Creates {
		logFields := log.Fields{
			"record":   change.Options.Name,
			"type":     change.Options.Type,
			"action":   "Create",
			"zoneName": change.Domain.Domain,
			"zoneID":   change.Domain.ID,
		}

		log.WithFields(logFields).Info("Creating record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would create record.")
		} else if _, err := p.Client.CreateDomainRecord(ctx, change.Domain.ID, change.Options); err != nil {
			// convert to linodego.Error and log the correct reason for the error
			apiErr, ok := err.(*linodego.Error)
			if !ok {
				log.WithFields(logFields).Errorf("Failed to Create record: %v", err)
				continue
			}
			// log the reason for the failure from the api response
			log.WithFields(logFields).Errorf("Failed to Create record: %v", apiErr.Message)
		}
	}

	for _, change := range changes.Deletes {
		logFields := log.Fields{
			"record":   change.DomainRecord.Name,
			"type":     change.DomainRecord.Type,
			"action":   "Delete",
			"zoneName": change.Domain.Domain,
			"zoneID":   change.Domain.ID,
		}

		log.WithFields(logFields).Info("Deleting record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would delete record.")
		} else if err := p.Client.DeleteDomainRecord(ctx, change.Domain.ID, change.DomainRecord.ID); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Delete record: %v",
				err,
			)
		}
	}

	for _, change := range changes.Updates {
		logFields := log.Fields{
			"record":   change.Options.Name,
			"type":     change.Options.Type,
			"action":   "Update",
			"zoneName": change.Domain.Domain,
			"zoneID":   change.Domain.ID,
		}

		log.WithFields(logFields).Info("Updating record.")

		if p.DryRun {
			log.WithFields(logFields).Info("Would update record.")
		} else if _, err := p.Client.UpdateDomainRecord(ctx, change.Domain.ID, change.DomainRecord.ID, change.Options); err != nil {
			log.WithFields(logFields).Errorf(
				"Failed to Update record: %v",
				err,
			)
		}
	}

	return nil
}

func getWeight(recordType linodego.DomainRecordType) *int {
	weight := 1

	// NS records do not support having weight
	if recordType == linodego.RecordTypeNS {
		weight = 0
	}
	return &weight
}

func getPort() *int {
	port := 0
	return &port
}

func getPriority() *int {
	priority := 0
	return &priority
}

// ApplyChanges applies a given set of changes in a given zone.
func (p *LinodeProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	log.WithFields(log.Fields{
		"Create":    len(changes.Create),
		"UpdateOld": len(changes.UpdateOld),
		"UpdateNew": len(changes.UpdateNew),
		"Deletes":   len(changes.Delete),
	}).Debug("Apply Changes.")
	zones, err := p.fetchZones(ctx)
	if err != nil {
		return err
	}

	zonesByID := make(map[string]linodego.Domain)
	zoneNameIDMapper := provider.ZoneIDName{}

	for _, z := range zones {
		zoneNameIDMapper.Add(strconv.Itoa(z.ID), z.Domain)
		zonesByID[strconv.Itoa(z.ID)] = z
	}

	createsByZone := endpointsByZone(zoneNameIDMapper, changes.Create)
	updatesByZone := endpointsByZone(zoneNameIDMapper, changes.UpdateNew)
	deletesByZone := endpointsByZone(zoneNameIDMapper, changes.Delete)

	var linodeCreates []LinodeChangeCreate
	var linodeUpdates []LinodeChangeUpdate
	var linodeDeletes []LinodeChangeDelete

	// Generate Creates
	for zoneID, creates := range createsByZone {
		zone := zonesByID[zoneID]

		if len(creates) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Domain,
			}).Debug("Skipping Zone, no creates found.")
			continue
		}

		for _, ep := range creates {
			recordType, err := convertRecordType(ep.RecordType)
			if err != nil {
				return err
			}

			for _, target := range ep.Targets {
				linodeCreates = append(linodeCreates, LinodeChangeCreate{
					Domain: zone,
					Options: linodego.DomainRecordCreateOptions{
						Target:   target,
						Name:     getStrippedRecordName(zone, ep),
						Type:     recordType,
						Weight:   getWeight(recordType),
						Port:     getPort(),
						Priority: getPriority(),
						TTLSec:   int(ep.RecordTTL),
					},
				})
			}
		}
	}

	// Generate Updates
	for zoneID, updates := range updatesByZone {
		zone := zonesByID[zoneID]

		if len(updates) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Domain,
			}).Debug("Skipping Zone, no updates found.")
			continue
		}

		for _, ep := range updates {
			// Use filtered query to find existing records
			matchedRecords, err := p.getRecordIDFiltered(ctx, zone.ID, zone, ep)
			if err != nil {
				return err
			}

			if len(matchedRecords) == 0 {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"dnsName":    ep.DNSName,
					"zoneName":   zone.Domain,
					"recordType": ep.RecordType,
				}).Warn("Update Records not found.")
			}

			recordType, err := convertRecordType(ep.RecordType)
			if err != nil {
				return err
			}

			matchedRecordsByTarget := make(map[string]linodego.DomainRecord)

			for _, record := range matchedRecords {
				matchedRecordsByTarget[record.Target] = record
			}

			for _, target := range ep.Targets {
				if record, ok := matchedRecordsByTarget[target]; ok {
					log.WithFields(log.Fields{
						"zoneID":     zoneID,
						"dnsName":    ep.DNSName,
						"zoneName":   zone.Domain,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Updating Existing Target")

					linodeUpdates = append(linodeUpdates, LinodeChangeUpdate{
						Domain:       zone,
						DomainRecord: record,
						Options: linodego.DomainRecordUpdateOptions{
							Target:   target,
							Name:     getStrippedRecordName(zone, ep),
							Type:     recordType,
							Weight:   getWeight(recordType),
							Port:     getPort(),
							Priority: getPriority(),
							TTLSec:   int(ep.RecordTTL),
						},
					})

					delete(matchedRecordsByTarget, target)
				} else {
					// Record did not previously exist, create new 'target'
					log.WithFields(log.Fields{
						"zoneID":     zoneID,
						"dnsName":    ep.DNSName,
						"zoneName":   zone.Domain,
						"recordType": ep.RecordType,
						"target":     target,
					}).Warn("Creating New Target")

					linodeCreates = append(linodeCreates, LinodeChangeCreate{
						Domain: zone,
						Options: linodego.DomainRecordCreateOptions{
							Target:   target,
							Name:     getStrippedRecordName(zone, ep),
							Type:     recordType,
							Weight:   getWeight(recordType),
							Port:     getPort(),
							Priority: getPriority(),
							TTLSec:   int(ep.RecordTTL),
						},
					})
				}
			}

			// Any remaining records have been removed, delete them
			for _, record := range matchedRecordsByTarget {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"dnsName":    ep.DNSName,
					"zoneName":   zone.Domain,
					"recordType": ep.RecordType,
					"target":     record.Target,
				}).Warn("Deleting Target")

				linodeDeletes = append(linodeDeletes, LinodeChangeDelete{
					Domain:       zone,
					DomainRecord: record,
				})
			}
		}
	}

	// Generate Deletes
	for zoneID, deletes := range deletesByZone {
		zone := zonesByID[zoneID]

		if len(deletes) == 0 {
			log.WithFields(log.Fields{
				"zoneID":   zoneID,
				"zoneName": zone.Domain,
			}).Debug("Skipping Zone, no deletes found.")
			continue
		}

		for _, ep := range deletes {
			// Use filtered query to find records to delete
			matchedRecords, err := p.getRecordIDFiltered(ctx, zone.ID, zone, ep)
			if err != nil {
				return err
			}

			if len(matchedRecords) == 0 {
				log.WithFields(log.Fields{
					"zoneID":     zoneID,
					"dnsName":    ep.DNSName,
					"zoneName":   zone.Domain,
					"recordType": ep.RecordType,
				}).Warn("Records to Delete not found.")
			}

			for _, record := range matchedRecords {
				linodeDeletes = append(linodeDeletes, LinodeChangeDelete{
					Domain:       zone,
					DomainRecord: record,
				})
			}
		}
	}

	return p.submitChanges(ctx, LinodeChanges{
		Creates: linodeCreates,
		Deletes: linodeDeletes,
		Updates: linodeUpdates,
	})
}

func endpointsByZone(zoneNameIDMapper provider.ZoneIDName, endpoints []*endpoint.Endpoint) map[string][]endpoint.Endpoint {
	endpointsByZone := make(map[string][]endpoint.Endpoint)

	for _, ep := range endpoints {
		zoneID, _ := zoneNameIDMapper.FindZone(ep.DNSName)
		if zoneID == "" {
			log.Debugf("Skipping record %s because no hosted zone matching record DNS Name was detected", ep.DNSName)
			continue
		}
		endpointsByZone[zoneID] = append(endpointsByZone[zoneID], *ep)
	}

	return endpointsByZone
}

func convertRecordType(recordType string) (linodego.DomainRecordType, error) {
	switch recordType {
	case "A":
		return linodego.RecordTypeA, nil
	case "AAAA":
		return linodego.RecordTypeAAAA, nil
	case "CNAME":
		return linodego.RecordTypeCNAME, nil
	case "TXT":
		return linodego.RecordTypeTXT, nil
	case "SRV":
		return linodego.RecordTypeSRV, nil
	case "NS":
		return linodego.RecordTypeNS, nil
	default:
		return "", fmt.Errorf("invalid Record Type: %s", recordType)
	}
}

func getStrippedRecordName(zone linodego.Domain, ep endpoint.Endpoint) string {
	// Handle root
	if ep.DNSName == zone.Domain {
		return ""
	}

	return strings.TrimSuffix(ep.DNSName, "."+zone.Domain)
}

// getRecordID finds matching records by iterating through a pre-fetched list
// This is used when we already have all records loaded (e.g., in ApplyChanges bulk operations)
func getRecordID(records []linodego.DomainRecord, zone linodego.Domain, ep endpoint.Endpoint) []linodego.DomainRecord {
	var matchedRecords []linodego.DomainRecord

	for _, record := range records {
		if record.Name == getStrippedRecordName(zone, ep) && string(record.Type) == ep.RecordType {
			matchedRecords = append(matchedRecords, record)
		}
	}

	return matchedRecords
}

// getRecordIDFiltered fetches and returns matching records using X-Filter for efficient API queries
// This reduces API calls by filtering server-side instead of fetching all records
func (p *LinodeProvider) getRecordIDFiltered(ctx context.Context, domainID int, zone linodego.Domain, ep endpoint.Endpoint) ([]linodego.DomainRecord, error) {
	recordType, err := convertRecordType(ep.RecordType)
	if err != nil {
		return nil, err
	}

	name := getStrippedRecordName(zone, ep)
	records, err := p.fetchRecordsFiltered(ctx, domainID, name, recordType)
	if err != nil {
		return nil, err
	}

	return records, nil
}
