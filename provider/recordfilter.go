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

package provider

// supportedRecordTypesMap is the source of truth for record types supported by external-dns.
// Currently A, AAAA, CNAME, SRV, TXT and NS record types are supported.
var supportedRecordTypesMap = map[string]struct{}{
	"A":     {},
	"AAAA":  {},
	"CNAME": {},
	"SRV":   {},
	"TXT":   {},
	"NS":    {},
}

// GetSupportedRecordTypes returns a slice of all supported record types.
// The order is not guaranteed.
func GetSupportedRecordTypes() []string {
	result := make([]string, 0, len(supportedRecordTypesMap))
	for recordType := range supportedRecordTypesMap {
		result = append(result, recordType)
	}
	return result
}

// SupportedRecordType returns true only for supported record types.
// Currently A, AAAA, CNAME, SRV, TXT and NS record types are supported.
func SupportedRecordType(recordType string) bool {
	_, ok := supportedRecordTypesMap[recordType]
	return ok
}
