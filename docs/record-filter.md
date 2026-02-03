# Record Filter Feature

## Overview

The Record Filter feature allows you to filter DNS records by their names, similar to how Domain Filters work for domains. This provides fine-grained control over which DNS records are managed by external-dns.

## Configuration Parameters

The following new command-line flags have been added to `types.go`:

### Basic Filters
- `--record-filter`: Limit managed records by DNS record name (can be specified multiple times)
- `--exclude-records`: Exclude specific DNS record names (can be specified multiple times)

### Regex Filters
- `--regex-record-filter`: Limit records using a regular expression (overrides `--record-filter`)
- `--regex-record-exclusion`: Exclude records using a regular expression

## Usage Examples

### Example 1: Include Specific Records
```bash
external-dns \
  --provider=linode \
  --source=service \
  --record-filter=api.example.com \
  --record-filter=www.example.com
```

This will only manage DNS records named `api.example.com` and `www.example.com`.

### Example 2: Exclude Specific Records
```bash
external-dns \
  --provider=linode \
  --source=service \
  --exclude-records=test.example.com \
  --exclude-records=staging.example.com
```

This will manage all records except `test.example.com` and `staging.example.com`.

### Example 3: Wildcard Pattern
```bash
external-dns \
  --provider=linode \
  --source=service \
  --record-filter=*.example.com
```

This will manage all records that end with `.example.com`.

### Example 4: Suffix Matching
```bash
external-dns \
  --provider=linode \
  --source=service \
  --record-filter=.example.com
```

This will manage all subdomains under `example.com` (but not `example.com` itself).

### Example 5: Regex Include Filter
```bash
external-dns \
  --provider=linode \
  --source=service \
  --regex-record-filter='^(api|www|admin)\.example\.com$'
```

This will only manage records matching the regex pattern (api, www, or admin.example.com).

### Example 6: Combined Include and Exclude
```bash
external-dns \
  --provider=linode \
  --source=service \
  --record-filter=example.com \
  --exclude-records=test.example.com \
  --exclude-records=dev.example.com
```

This will manage all records under `example.com` except `test.example.com` and `dev.example.com`.

### Example 7: Regex with Exclusion
```bash
external-dns \
  --provider=linode \
  --source=service \
  --regex-record-filter='\.example\.com$' \
  --regex-record-exclusion='^(test|staging)\.'
```

This will manage all records ending with `.example.com` except those starting with `test.` or `staging.`.

## Configuration Struct Fields

The following fields have been added to the `Config` struct in `pkg/apis/externaldns/types.go`:

```go
type Config struct {
    // ... existing fields ...
    
    RecordFilter       []string        // List of record names to include
    RecordExclude      []string        // List of record names to exclude
    RegexRecordFilter  *regexp.Regexp  // Regex pattern for included records
    RegexRecordExclude *regexp.Regexp  // Regex pattern for excluded records
    
    // ... remaining fields ...
}
```

## RecordFilter API

The `endpoint` package provides a `RecordFilter` type with the following constructors:

```go
// NewRecordFilter creates a filter with inclusion rules
rf := endpoint.NewRecordFilter([]string{"api.example.com", "www.example.com"})

// NewRecordFilterWithExclusions creates a filter with both inclusion and exclusion rules
rf := endpoint.NewRecordFilterWithExclusions(
    []string{"example.com"},      // include
    []string{"test.example.com"}, // exclude
)

// NewRegexRecordFilter creates a filter with regex patterns
rf := endpoint.NewRegexRecordFilter(
    regexp.MustCompile(`^api\.`),   // include pattern
    regexp.MustCompile(`^test\.`),  // exclude pattern
)

// NewRecordFilterWithOptions creates a filter using functional options
rf := endpoint.NewRecordFilterWithOptions(
    endpoint.WithRecordFilter([]string{"api.example.com"}),
    endpoint.WithRecordExclude([]string{"test.example.com"}),
)
```

## Matching Logic

### Priority
1. If regex filters are configured, they take precedence over simple filters
2. Exclusion rules are checked before inclusion rules
3. If a record matches an exclusion rule, it is immediately rejected

### Behavior
- **Nil filter**: Matches everything
- **Empty filter**: Matches everything
- **Include only**: Only specified records match
- **Exclude only**: All records match except excluded ones
- **Include + Exclude**: Only included records match, except excluded ones

### Pattern Matching
- **Exact match**: `api.example.com` matches `api.example.com` only
- **Wildcard**: `*.example.com` matches any subdomain of `example.com`
- **Suffix**: `.example.com` matches all subdomains (including nested ones)
- **Case insensitive**: All matching is case-insensitive
- **Trailing dots**: Automatically stripped during normalization

## Provider Integration

To integrate the record filter in a provider, follow these steps:

1. Add a `recordFilter` field to your provider struct:
```go
type MyProvider struct {
    // ... existing fields ...
    recordFilter *endpoint.RecordFilter
}
```

2. Accept the record filter in your provider constructor:
```go
func NewMyProvider(
    domainFilter *endpoint.DomainFilter,
    recordFilter *endpoint.RecordFilter,
    // ... other params ...
) (*MyProvider, error) {
    return &MyProvider{
        domainFilter: domainFilter,
        recordFilter: recordFilter,
        // ... other fields ...
    }, nil
}
```

3. Use the filter when processing records:
```go
func (p *MyProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
    // Fetch records from provider
    records := // ...
    
    // Filter records
    var filtered []*endpoint.Endpoint
    for _, record := range records {
        if p.recordFilter.Match(record.DNSName) {
            filtered = append(filtered, record)
        }
    }
    
    return filtered, nil
}
```

## Notes

- Record names are normalized (lowercased, trailing dots removed) before matching
- The record filter is independent of the domain filter - both can be used together
- When both filters are configured, a record must pass both filters to be managed
