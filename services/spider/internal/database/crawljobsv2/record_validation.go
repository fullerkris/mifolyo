package crawljobsv2

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrRecordRelation = errors.New("crawljobsv2: fixed-record relation mismatch")

// ValidateRecord is the value-aware fixed-record entry point. In contrast to
// ValidateRecordSchema, it validates ordered names, lexical values, protocol
// constants, sentinels, and the bounded cross-field relations available from a
// record in isolation.
func ValidateRecord(schema RecordSchema, record Record) error {
	expected, err := RecordSchemaFields(schema)
	if err != nil {
		return err
	}
	if len(record) != len(expected) {
		return ErrInvalidSchema
	}
	for index := range expected {
		if record[index].Name != expected[index] {
			return ErrInvalidSchema
		}
	}
	switch schema {
	case SchemaRun, SchemaJob, SchemaReservation, SchemaRateScope, SchemaStageMeta:
		return validateLedgerRecord(schema, record)
	default:
		return validateAuthorityRecord(schema, record)
	}
}

func validateLedgerRecord(schema RecordSchema, record Record) error {
	values, err := strictTextValues(record)
	if err != nil {
		return err
	}
	if values[0] != "2" {
		return ErrInvalidRecordValue
	}
	switch schema {
	case SchemaRun:
		return validateRunRecordValues(values)
	case SchemaJob:
		return validateJobRecordValues(values)
	case SchemaReservation:
		return validateReservationRecordValues(values)
	case SchemaRateScope:
		return validateRateScopeRecordValues(values)
	case SchemaStageMeta:
		return validateStageMetaRecordValues(values)
	default:
		return ErrInvalidSchema
	}
}

func validateRunRecordValues(values []string) error {
	for _, index := range []int{1, 4, 6, 7, 10, 12, 14, 16} {
		if _, err := ParseDigest(values[index]); err != nil {
			return err
		}
	}
	if !oneOf(values[2], "loading", "auditing", "sealed", "active", "completed", "budget_exhausted", "cancelled", "archived") {
		return ErrInvalidRecordValue
	}
	if _, err := ParseSourceKind(values[3]); err != nil {
		return err
	}
	numericIndexes := []int{5, 8, 9, 11, 13, 15, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 56, 57}
	numbers, err := parseIndexedDecimals(values, numericIndexes)
	if err != nil {
		return err
	}
	if numbers[5] > MaxJobsPerRun || numbers[15] == 0 || numbers[15] > MaxPolicyGroupsPerRun || numbers[17] != MaxJobsPerRun ||
		numbers[18] == 0 || numbers[18] > MaxRequestStartsPerRun || numbers[19] != GlobalActiveRequestLimit || numbers[20] != MaxDeliveryAttempts ||
		numbers[21] > numbers[5] || numbers[22] > numbers[21] || numbers[23] > numbers[18] || numbers[24] > MaxReservationCreationsPerRun ||
		numbers[25]+numbers[26] > numbers[24] || numbers[31]+numbers[32]+numbers[33] > numbers[21] || numbers[34] > numbers[31] ||
		numbers[57] > numbers[21] || numbers[8] == 0 || numbers[40] == 0 || numbers[47] == 0 {
		return ErrRecordRelation
	}
	if values[9] != "1" || values[11] != "2" || values[20] != "3" || !oneOf(values[39], "0", "1") {
		return ErrInvalidRecordValue
	}
	if values[38] == "" {
		if numbers[37] != 0 {
			return ErrRecordRelation
		}
	} else if _, err := ParseJobID(values[38]); err != nil {
		return err
	}
	if values[53] == "" {
		if numbers[52] != 0 {
			return ErrRecordRelation
		}
	} else if _, err := ParseDigest(values[53]); err != nil {
		return err
	}
	switch values[54] {
	case "none":
		if values[55] != "" || numbers[56] != 0 || numbers[57] != 0 {
			return ErrRecordRelation
		}
	case "in_progress":
		if _, err := ParseDigest(values[55]); err != nil {
			return err
		}
		if numbers[56] == 0 || numbers[57] == 0 || numbers[57] >= numbers[21] {
			return ErrRecordRelation
		}
	default:
		return ErrInvalidRecordValue
	}
	if _, err := ParseReason(values[58]); err != nil {
		return err
	}
	return nil
}

func validateJobRecordValues(values []string) error {
	runID, err := ParseRunID(values[1])
	if err != nil {
		return err
	}
	_ = runID
	jobID, err := ParseJobID(values[2])
	if err != nil {
		return err
	}
	if values[3] != values[2] || validateJobURL(jobID, values[4]) != nil {
		return ErrRecordRelation
	}
	if _, err := ParseUnsignedDecimal(values[5]); err != nil {
		return err
	}
	if _, err := ParseScoreText(values[6]); err != nil {
		return err
	}
	if !oneOf(values[7], "ready", "leased", "delayed", "completed", "dead", "cancelled") {
		return ErrInvalidRecordValue
	}
	if _, err := ParseGroupID(values[8]); err != nil {
		return err
	}
	if _, err := ParseRateScopeID(values[9]); err != nil {
		return err
	}
	for _, index := range []int{10, 11, 12} {
		if _, err := ParseDigest(values[index]); err != nil {
			return err
		}
	}
	numericIndexes := []int{13, 14, 15, 16, 17, 18, 19, 20, 21, 29, 30, 31, 32, 36, 37, 38, 40, 41, 48, 49, 50, 51, 52}
	numbers, err := parseIndexedDecimals(values, numericIndexes)
	if err != nil {
		return err
	}
	if numbers[14] > MaxDeliveryAttempts || numbers[31] > 1 || numbers[36] > numbers[29] || numbers[38] > numbers[29] {
		return ErrRecordRelation
	}
	if err := validateOptionalDocumentIdentity(values[20], values[21], values[22], values[23], values[24]); err != nil {
		return err
	}
	for _, index := range []int{25, 26} {
		if _, err := ParseReason(values[index]); err != nil {
			return err
		}
	}
	if err := validateOptionalLease(values[7], values[27], values[28], numbers[29], numbers[30], numbers[31]); err != nil {
		return err
	}
	if values[32] != "" {
		if _, err := ParseReservationID(values[32]); err != nil {
			return err
		}
	}
	for _, index := range []int{33, 34, 35, 42, 43, 44, 46} {
		if values[index] != "" {
			if _, err := ParseDigest(values[index]); err != nil {
				return err
			}
		}
	}
	if !oneOf(values[39], "none", "pages_queue_full", "memory_headroom_low") {
		return ErrInvalidRecordValue
	}
	if values[39] == "none" {
		if numbers[38] != 0 || numbers[40] != 0 || numbers[41] != 0 {
			return ErrRecordRelation
		}
	} else if numbers[38] == 0 || numbers[40] == 0 || numbers[41] <= numbers[40] {
		return ErrRecordRelation
	}
	publicationSet := values[42] != "" || values[43] != "" || values[44] != "" || values[45] != ""
	if publicationSet && (values[42] == "" || values[43] == "" || values[44] == "" || values[45] == "") {
		return ErrRecordRelation
	}
	if values[46] == "" != (values[47] == "") {
		return ErrRecordRelation
	}
	if values[47] != "" {
		if _, err := ParseStatus(values[47]); err != nil {
			return err
		}
	}
	if numbers[48] == 0 || numbers[49] == 0 {
		return ErrRecordRelation
	}
	return nil
}

func validateReservationRecordValues(values []string) error {
	if _, err := ParseReservationID(values[1]); err != nil {
		return err
	}
	if _, err := ParseRunID(values[2]); err != nil {
		return err
	}
	if _, err := ParseJobID(values[3]); err != nil {
		return err
	}
	if _, err := ParseOwnerID(values[4]); err != nil {
		return err
	}
	if _, err := ParseLeaseToken(values[5]); err != nil {
		return err
	}
	if _, err := ParseFence(values[6]); err != nil {
		return err
	}
	ordinal, err := parsePositiveDecimal(values[7])
	if err != nil || ordinal == 0 {
		return ErrInvalidRecordValue
	}
	if !oneOf(values[8], "pending", "started", "finished", "cancelled", "expired") {
		return ErrInvalidRecordValue
	}
	if _, err := ParseRequestKind(values[9]); err != nil {
		return err
	}
	targetID, err := ParseJobID(values[10])
	if err != nil {
		return err
	}
	if err := validateJobURL(targetID, values[11]); err != nil {
		return err
	}
	for _, index := range []int{12, 13, 14, 17, 18, 19} {
		if _, err := ParseDigest(values[index]); err != nil {
			return err
		}
	}
	if _, err := ParseGroupID(values[15]); err != nil {
		return err
	}
	if _, err := ParseRateScopeID(values[16]); err != nil {
		return err
	}
	numbers, err := parseIndexedDecimals(values, []int{20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33})
	if err != nil {
		return err
	}
	if numbers[20] != GlobalActiveRequestLimit || numbers[21] != 0 || !validScopeTuple(numbers[22], numbers[23]) || !validScopeTuple(numbers[24], numbers[25]) ||
		numbers[26] == 0 || numbers[33] == 0 {
		return ErrRecordRelation
	}
	switch values[8] {
	case "pending":
		if numbers[27] != 0 || numbers[28] != 0 {
			return ErrRecordRelation
		}
	case "started":
		if numbers[27] == 0 || numbers[28] != 0 {
			return ErrRecordRelation
		}
	case "finished":
		if numbers[27] == 0 || numbers[28] == 0 {
			return ErrRecordRelation
		}
	case "cancelled", "expired":
		if numbers[28] == 0 {
			return ErrRecordRelation
		}
	}
	return nil
}

func validateRateScopeRecordValues(values []string) error {
	scopeID, err := ParseDigest(values[1])
	if err != nil {
		return err
	}
	numbers, err := parseIndexedDecimals(values, []int{4, 5, 6, 7, 8, 9, 10, 13})
	if err != nil {
		return err
	}
	if numbers[4] == 0 || numbers[4] > MaxScopeConcurrency || numbers[5] > MaxScopeIntervalMilliseconds || numbers[9]+numbers[10] != numbers[8] || numbers[13] == 0 {
		return ErrRecordRelation
	}
	for _, index := range []int{11, 12} {
		if _, err := ParseDigest(values[index]); err != nil {
			return err
		}
	}
	switch values[2] {
	case "global":
		if values[3] != "global" || scopeID != DeriveGlobalScopeID() || numbers[4] != GlobalActiveRequestLimit || numbers[5] != 0 {
			return ErrRecordRelation
		}
	case "group":
		rateScopeID, err := ParseRateScopeID(values[3])
		if err != nil {
			return err
		}
		expected, _ := DeriveGroupScopeID(rateScopeID)
		if scopeID != expected {
			return ErrRecordRelation
		}
	case "origin":
		origin := CanonicalOrigin(values[3])
		if err := validateCanonicalOrigin(origin); err != nil {
			return err
		}
		expected, _ := DeriveOriginScopeID(origin)
		if scopeID != expected {
			return ErrRecordRelation
		}
	default:
		return ErrInvalidRecordValue
	}
	return nil
}

func validateStageMetaRecordValues(values []string) error {
	if _, err := ParseRunID(values[1]); err != nil {
		return err
	}
	if _, err := ParseJobID(values[2]); err != nil {
		return err
	}
	if _, err := ParseOwnerID(values[3]); err != nil {
		return err
	}
	if _, err := ParseFence(values[4]); err != nil {
		return err
	}
	for _, index := range []int{5, 6, 7, 8} {
		if _, err := ParseDigest(values[index]); err != nil {
			return err
		}
	}
	numbers, err := parseIndexedDecimals(values, integerRange(9, 28))
	if err != nil {
		return err
	}
	if numbers[9] == 0 || numbers[10] <= numbers[9] || !oneOf(values[11], "0", "1") || !oneOf(values[13], "0", "1") ||
		numbers[14] != FinalPageFieldCount || numbers[15] > MaxOutlinksPerJob || numbers[16] > MaxDiscoveriesPerJob ||
		numbers[17] == 0 || numbers[17] > MaxAliasesPerJob || numbers[18] > MaxImagesPerPage || numbers[19] > numbers[14] ||
		numbers[22] > numbers[15] || numbers[23] > numbers[16] || numbers[24] > numbers[17] || numbers[25] > numbers[18] ||
		numbers[27] > MaxStageAggregateLogicalDataBytes || numbers[28] < 2 || numbers[28] > MaxStageKeys {
		return ErrRecordRelation
	}
	if values[11] == "0" && numbers[12] != 0 || values[11] == "1" && numbers[12] == 0 {
		return ErrRecordRelation
	}
	for index := 29; index < len(values); index++ {
		if values[index] != "" {
			if _, err := ParseDigest(values[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOptionalDocumentIdentity(startedAt, fence, targetID, targetURL, targetDigest string) error {
	allEmpty := startedAt == "0" && fence == "0" && targetID == "" && targetURL == "" && targetDigest == ""
	if allEmpty {
		return nil
	}
	if startedAt == "0" || fence == "0" || targetID == "" || targetURL == "" || targetDigest == "" {
		return ErrRecordRelation
	}
	if _, err := parsePositiveDecimal(startedAt); err != nil {
		return err
	}
	if _, err := ParseFence(fence); err != nil {
		return err
	}
	jobID, err := ParseJobID(targetID)
	if err != nil {
		return err
	}
	if err := validateJobURL(jobID, targetURL); err != nil {
		return err
	}
	digest, err := DeriveTargetDigest(RequestTarget{URLID: jobID, CanonicalURL: targetURL})
	if err != nil || string(digest) != targetDigest {
		return ErrRecordRelation
	}
	return nil
}

func validateOptionalLease(state, owner, token string, fence, startedAt, expiresAt uint64) error {
	if state == "leased" {
		if _, err := ParseOwnerID(owner); err != nil {
			return err
		}
		if _, err := ParseLeaseToken(token); err != nil {
			return err
		}
		if fence == 0 || startedAt == 0 || expiresAt <= startedAt {
			return ErrRecordRelation
		}
		return nil
	}
	if owner != "" || token != "" || startedAt != 0 || expiresAt != 0 {
		return ErrRecordRelation
	}
	return nil
}

func parseIndexedDecimals(values []string, indexes []int) (map[int]uint64, error) {
	parsed := make(map[int]uint64, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(values) {
			return nil, ErrInvalidSchema
		}
		value, err := parseCanonicalDecimal(values[index])
		if err != nil {
			return nil, err
		}
		parsed[index] = value
	}
	return parsed, nil
}

func integerRange(first, last int) []int {
	result := make([]int, 0, last-first+1)
	for index := first; index <= last; index++ {
		result = append(result, index)
	}
	return result
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateBoundedUTF8(value string, maximum int, allowEmpty bool) error {
	if (!allowEmpty && value == "") || len(value) > maximum || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 }) >= 0 {
		return ErrInvalidRecordValue
	}
	return nil
}
