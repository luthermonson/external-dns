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

package endpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MatchAllRecordFilters applies all record filters
type MatchAllRecordFilters []RecordFilterInterface

func (f MatchAllRecordFilters) Match(record string) bool {
	for _, filter := range f {
		if filter == nil {
			continue
		}
		if !filter.Match(record) {
			return false
		}
	}
	return true
}

// RecordFilterInterface defines the interface for record filtering
type RecordFilterInterface interface {
	Match(record string) bool
}

// RecordFilter holds a lists of valid DNS record names
type RecordFilter struct {
	// Filters define what records to match
	Filters []string
	// exclude define what records not to match
	exclude []string
	// regex defines a regular expression to match the records
	regex *regexp.Regexp
	// regexExclusion defines a regular expression to exclude the records matched
	regexExclusion *regexp.Regexp
}

var _ RecordFilterInterface = &RecordFilter{}

// recordFilterSerde is a helper type for serializing and deserializing RecordFilter.
type recordFilterSerde struct {
	Include      []string `json:"include,omitempty"`
	Exclude      []string `json:"exclude,omitempty"`
	RegexInclude string   `json:"regexInclude,omitempty"`
	RegexExclude string   `json:"regexExclude,omitempty"`
}

// prepareRecordFilters provides consistent trimming for filters/exclude params
func prepareRecordFilters(filters []string) []string {
	var fs []string
	for _, filter := range filters {
		if record := normalizeRecord(strings.TrimSpace(filter)); record != "" {
			fs = append(fs, record)
		}
	}
	return fs
}

// NewRecordFilterWithExclusions returns a new RecordFilter, given a list of matches and exclusions
func NewRecordFilterWithExclusions(recordFilters []string, excludeRecords []string) *RecordFilter {
	return &RecordFilter{Filters: prepareRecordFilters(recordFilters), exclude: prepareRecordFilters(excludeRecords)}
}

// NewRecordFilter returns a new RecordFilter given a list of records
func NewRecordFilter(recordFilters []string) *RecordFilter {
	return &RecordFilter{Filters: prepareRecordFilters(recordFilters)}
}

// NewRegexRecordFilter returns a new RecordFilter given regular expressions
func NewRegexRecordFilter(regexRecordFilter *regexp.Regexp, regexRecordExclusion *regexp.Regexp) *RecordFilter {
	return &RecordFilter{regex: regexRecordFilter, regexExclusion: regexRecordExclusion}
}

// NewRecordFilterWithOptions creates a RecordFilter based on the provided parameters.
//
// Example usage:
//
//	rf := NewRecordFilterWithOptions(
//		WithRecordFilter([]string{"api.example.com"}),
//		WithRecordExclude([]string{"test.example.com"}),
//	)
func NewRecordFilterWithOptions(opts ...RecordFilterOption) *RecordFilter {
	cfg := &recordFilterConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.isRegexFilter {
		return NewRegexRecordFilter(cfg.regexInclude, cfg.regexExclude)
	}
	return NewRecordFilterWithExclusions(cfg.include, cfg.exclude)
}

// Match checks whether a record can be found in the RecordFilter.
// RegexFilter takes precedence over Filters
func (rf *RecordFilter) Match(record string) bool {
	if rf == nil {
		return true // nil filter matches everything
	}
	if rf.regex != nil && rf.regex.String() != "" || rf.regexExclusion != nil && rf.regexExclusion.String() != "" {
		return matchRecordRegex(rf.regex, rf.regexExclusion, record)
	}

	return matchRecordFilter(rf.Filters, record, true) && !matchRecordFilter(rf.exclude, record, false)
}

// matchRecordFilter determines if any `filters` match `record`.
// If no `filters` are provided, behavior depends on `emptyval`
// (empty `rf.filters` matches everything, while empty `rf.exclude` excludes nothing)
func matchRecordFilter(filters []string, record string, emptyval bool) bool {
	if len(filters) == 0 {
		return emptyval
	}

	strippedRecord := normalizeRecord(record)
	for _, filter := range filters {
		if filter == "" {
			continue
		}

		// Exact match or suffix match (for wildcard patterns)
		switch {
		case strings.HasPrefix(filter, ".") && strings.HasSuffix(strippedRecord, filter):
			return true
		case strippedRecord == filter:
			return true
		case strings.HasPrefix(filter, "*.") && strings.HasSuffix(strippedRecord, filter[1:]):
			// Handle wildcard patterns like "*.example.com"
			return true
		case strings.HasSuffix(strippedRecord, "."+filter):
			return true
		}
	}
	return false
}

// matchRecordRegex determines if a record matches the configured regular expressions in RecordFilter.
// The function checks exclusion first, then inclusion:
// 1. If negativeRegex is set and matches the record, return false (excluded)
// 2. If regex is set and matches the record, return true (included)
// 3. If regex is not set but negativeRegex is set, return true (not excluded, no inclusion filter)
// 4. If regex is set but doesn't match, return false (not included)
func matchRecordRegex(regex *regexp.Regexp, negativeRegex *regexp.Regexp, record string) bool {
	strippedRecord := normalizeRecord(record)

	// First check exclusion - if record matches exclusion, reject it
	if negativeRegex != nil && negativeRegex.String() != "" {
		if negativeRegex.MatchString(strippedRecord) {
			return false
		}
	}

	// Then check inclusion filter if set
	if regex != nil && regex.String() != "" {
		return regex.MatchString(strippedRecord)
	}

	// If only exclusion is set (no inclusion filter), accept the record
	// since it didn't match the exclusion
	return true
}

// IsConfigured returns true if any inclusion or exclusion rules have been specified.
func (rf *RecordFilter) IsConfigured() bool {
	if rf == nil {
		return false // nil filter is not configured
	}
	if rf.regex != nil && rf.regex.String() != "" {
		return true
	} else if rf.regexExclusion != nil && rf.regexExclusion.String() != "" {
		return true
	}
	return len(rf.Filters) > 0 || len(rf.exclude) > 0
}

func (rf *RecordFilter) MarshalJSON() ([]byte, error) {
	if rf == nil {
		// compatibility with nil RecordFilter
		return json.Marshal(recordFilterSerde{
			Include: nil,
			Exclude: nil,
		})
	}
	if rf.regex != nil || rf.regexExclusion != nil {
		var include, exclude string
		if rf.regex != nil {
			include = rf.regex.String()
		}
		if rf.regexExclusion != nil {
			exclude = rf.regexExclusion.String()
		}
		return json.Marshal(recordFilterSerde{
			RegexInclude: include,
			RegexExclude: exclude,
		})
	}
	sort.Strings(rf.Filters)
	sort.Strings(rf.exclude)
	return json.Marshal(recordFilterSerde{
		Include: rf.Filters,
		Exclude: rf.exclude,
	})
}

func (rf *RecordFilter) UnmarshalJSON(b []byte) error {
	var deserialized recordFilterSerde
	err := json.Unmarshal(b, &deserialized)
	if err != nil {
		return err
	}

	if deserialized.RegexInclude == "" && deserialized.RegexExclude == "" {
		*rf = *NewRecordFilterWithExclusions(deserialized.Include, deserialized.Exclude)
		return nil
	}

	if len(deserialized.Include) > 0 || len(deserialized.Exclude) > 0 {
		return errors.New("cannot have both record list and regex")
	}

	var include, exclude *regexp.Regexp
	if deserialized.RegexInclude != "" {
		include, err = regexp.Compile(deserialized.RegexInclude)
		if err != nil {
			return fmt.Errorf("invalid regexInclude: %w", err)
		}
	}
	if deserialized.RegexExclude != "" {
		exclude, err = regexp.Compile(deserialized.RegexExclude)
		if err != nil {
			return fmt.Errorf("invalid regexExclude: %w", err)
		}
	}
	*rf = *NewRegexRecordFilter(include, exclude)
	return nil
}

// normalizeRecord converts a record to a canonical form, so that we can filter on it
// it: trim "." suffix and convert to lowercase for consistent matching
func normalizeRecord(record string) string {
	return strings.ToLower(strings.TrimSuffix(record, "."))
}

type RecordFilterOption func(*recordFilterConfig)
type recordFilterConfig struct {
	include       []string
	exclude       []string
	regexInclude  *regexp.Regexp
	regexExclude  *regexp.Regexp
	isRegexFilter bool
}

func WithRecordFilter(filters []string) RecordFilterOption {
	return func(cfg *recordFilterConfig) {
		cfg.include = prepareRecordFilters(filters)
	}
}

func WithRecordExclude(exclude []string) RecordFilterOption {
	return func(cfg *recordFilterConfig) {
		cfg.exclude = prepareRecordFilters(exclude)
	}
}

func WithRegexRecordFilter(regex *regexp.Regexp) RecordFilterOption {
	return func(cfg *recordFilterConfig) {
		cfg.regexInclude = regex
		if regex != nil && regex.String() != "" {
			cfg.isRegexFilter = true
		}
	}
}

func WithRegexRecordExclude(regex *regexp.Regexp) RecordFilterOption {
	return func(cfg *recordFilterConfig) {
		cfg.regexExclude = regex
		if regex != nil && regex.String() != "" {
			cfg.isRegexFilter = true
		}
	}
}
