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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordFilter(t *testing.T) {
	t.Run("NewRecordFilter", func(t *testing.T) {
		rf := NewRecordFilter([]string{"api.example.com", "*.example.org"})
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("api.example.com."))
		assert.True(t, rf.Match("test.example.org"))
		assert.True(t, rf.Match("www.example.org"))
		assert.False(t, rf.Match("other.example.com"))
		assert.False(t, rf.Match("example.org"))
	})

	t.Run("NewRecordFilterWithExclusions", func(t *testing.T) {
		rf := NewRecordFilterWithExclusions([]string{"example.com"}, []string{"test.example.com"})
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("www.example.com"))
		assert.False(t, rf.Match("test.example.com"))
		assert.False(t, rf.Match("other.org"))
	})

	t.Run("NewRegexRecordFilter", func(t *testing.T) {
		include := regexp.MustCompile(`^(api|www)\.example\.com$`)
		exclude := regexp.MustCompile(`^test\..*`)
		rf := NewRegexRecordFilter(include, exclude)

		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("www.example.com"))
		assert.False(t, rf.Match("test.example.com"))
		assert.False(t, rf.Match("other.example.com"))
	})

	t.Run("NewRecordFilterWithOptions", func(t *testing.T) {
		rf := NewRecordFilterWithOptions(
			WithRecordFilter([]string{"api.example.com"}),
			WithRecordExclude([]string{"test.example.com"}),
		)
		assert.True(t, rf.Match("api.example.com"))
		assert.False(t, rf.Match("test.example.com"))
	})

	t.Run("NewRecordFilterWithOptions regex", func(t *testing.T) {
		include := regexp.MustCompile(`^api\..*`)
		rf := NewRecordFilterWithOptions(
			WithRegexRecordFilter(include),
		)
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("api.test.org"))
		assert.False(t, rf.Match("www.example.com"))
	})
}

func TestRecordFilterMatch(t *testing.T) {
	t.Run("nil filter matches everything", func(t *testing.T) {
		var rf *RecordFilter
		assert.True(t, rf.Match("any.record.com"))
	})

	t.Run("empty filter matches everything", func(t *testing.T) {
		rf := NewRecordFilter([]string{})
		assert.True(t, rf.Match("any.record.com"))
	})

	t.Run("exact match", func(t *testing.T) {
		rf := NewRecordFilter([]string{"api.example.com"})
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("api.example.com.")) // trailing dot
		assert.False(t, rf.Match("www.example.com"))
	})

	t.Run("wildcard match", func(t *testing.T) {
		rf := NewRecordFilter([]string{"*.example.com"})
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("www.example.com"))
		assert.True(t, rf.Match("test.example.com"))
		assert.True(t, rf.Match("sub.api.example.com")) // wildcard matches all that end with .example.com
		assert.False(t, rf.Match("example.com"))
	})

	t.Run("suffix match", func(t *testing.T) {
		rf := NewRecordFilter([]string{".example.com"})
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("www.example.com"))
		assert.True(t, rf.Match("sub.api.example.com"))
		assert.False(t, rf.Match("example.com"))
	})

	t.Run("case insensitive", func(t *testing.T) {
		rf := NewRecordFilter([]string{"API.Example.COM"})
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("API.EXAMPLE.COM"))
		assert.True(t, rf.Match("Api.Example.Com"))
	})
}

func TestRecordFilterIsConfigured(t *testing.T) {
	t.Run("nil filter is not configured", func(t *testing.T) {
		var rf *RecordFilter
		assert.False(t, rf.IsConfigured())
	})

	t.Run("empty filter is not configured", func(t *testing.T) {
		rf := NewRecordFilter([]string{})
		assert.False(t, rf.IsConfigured())
	})

	t.Run("filter with includes is configured", func(t *testing.T) {
		rf := NewRecordFilter([]string{"api.example.com"})
		assert.True(t, rf.IsConfigured())
	})

	t.Run("filter with excludes is configured", func(t *testing.T) {
		rf := NewRecordFilterWithExclusions([]string{}, []string{"test.example.com"})
		assert.True(t, rf.IsConfigured())
	})

	t.Run("regex filter is configured", func(t *testing.T) {
		rf := NewRegexRecordFilter(regexp.MustCompile(`^api\..*`), nil)
		assert.True(t, rf.IsConfigured())
	})
}

func TestRecordFilterJSON(t *testing.T) {
	t.Run("marshal and unmarshal simple filter", func(t *testing.T) {
		rf := NewRecordFilterWithExclusions(
			[]string{"api.example.com", "www.example.com"},
			[]string{"test.example.com"},
		)

		data, err := json.Marshal(rf)
		require.NoError(t, err)

		var unmarshaled RecordFilter
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.True(t, unmarshaled.Match("api.example.com"))
		assert.False(t, unmarshaled.Match("test.example.com"))
	})

	t.Run("marshal and unmarshal regex filter", func(t *testing.T) {
		rf := NewRegexRecordFilter(
			regexp.MustCompile(`^api\..*`),
			regexp.MustCompile(`^test\..*`),
		)

		data, err := json.Marshal(rf)
		require.NoError(t, err)

		var unmarshaled RecordFilter
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.True(t, unmarshaled.Match("api.example.com"))
		assert.False(t, unmarshaled.Match("test.example.com"))
		assert.False(t, unmarshaled.Match("www.example.com"))
	})

	t.Run("marshal nil filter", func(t *testing.T) {
		var rf *RecordFilter
		data, err := json.Marshal(rf)
		require.NoError(t, err)
		assert.Contains(t, string(data), "null")
	})
}

func TestMatchAllRecordFilters(t *testing.T) {
	t.Run("all filters must match", func(t *testing.T) {
		filter1 := NewRecordFilter([]string{"example.com"})
		filter2 := NewRecordFilter([]string{"api.example.com"})

		allFilters := MatchAllRecordFilters{filter1, filter2}

		assert.True(t, allFilters.Match("api.example.com"))
		assert.False(t, allFilters.Match("www.example.com"))
		assert.False(t, allFilters.Match("api.other.com"))
	})

	t.Run("skip nil filters", func(t *testing.T) {
		filter1 := NewRecordFilter([]string{"example.com"})
		var filter2 *RecordFilter

		allFilters := MatchAllRecordFilters{filter1, filter2}

		assert.True(t, allFilters.Match("api.example.com"))
	})
}

func TestNormalizeRecord(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"api.example.com", "api.example.com"},
		{"API.EXAMPLE.COM", "api.example.com"},
		{"api.example.com.", "api.example.com"},
		{"API.EXAMPLE.COM.", "api.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeRecord(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRecordFilterOptions(t *testing.T) {
	t.Run("WithRecordFilter", func(t *testing.T) {
		rf := NewRecordFilterWithOptions(
			WithRecordFilter([]string{"api.example.com", "www.example.com"}),
		)
		assert.True(t, rf.Match("api.example.com"))
		assert.True(t, rf.Match("www.example.com"))
		assert.False(t, rf.Match("test.example.com"))
	})

	t.Run("WithRecordExclude", func(t *testing.T) {
		rf := NewRecordFilterWithOptions(
			WithRecordExclude([]string{"test.example.com"}),
		)
		// Empty include means match all, except exclusions
		assert.True(t, rf.Match("api.example.com"))
		assert.False(t, rf.Match("test.example.com"))
	})

	t.Run("WithRegexRecordFilter", func(t *testing.T) {
		rf := NewRecordFilterWithOptions(
			WithRegexRecordFilter(regexp.MustCompile(`^api\..*`)),
		)
		assert.True(t, rf.Match("api.example.com"))
		assert.False(t, rf.Match("www.example.com"))
	})

	t.Run("WithRegexRecordExclude", func(t *testing.T) {
		rf := NewRecordFilterWithOptions(
			WithRegexRecordFilter(regexp.MustCompile(`\.example\.com$`)),
			WithRegexRecordExclude(regexp.MustCompile(`^test\..*`)),
		)
		assert.True(t, rf.Match("api.example.com"))
		assert.False(t, rf.Match("test.example.com"))
	})
}
