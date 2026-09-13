package crawljobsv2

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidChunk           = errors.New("crawljobsv2: invalid stage chunk")
	ErrChunkRecordsOutOfOrder = errors.New("crawljobsv2: stage chunk records are not in canonical order")
	ErrDuplicateChunkRecord   = errors.New("crawljobsv2: duplicate stage chunk record")
)

type StageChunk struct {
	commitID Digest
	kind     ChunkKind
	ordinal  uint64
	records  []Record
	context  OutputContext
	identity CommitIdentity
}

func (chunk StageChunk) CommitID() Digest  { return chunk.commitID }
func (chunk StageChunk) Kind() ChunkKind   { return chunk.kind }
func (chunk StageChunk) Ordinal() uint64   { return chunk.ordinal }
func (chunk StageChunk) Records() []Record { return cloneRecords(chunk.records) }

func NewPageFieldsStageChunk(identity CommitIdentity, context OutputContext, page OutputPage) (StageChunk, error) {
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return StageChunk{}, err
	}
	pageRecord, err := outputPageRecord(context, page)
	if err != nil {
		return StageChunk{}, err
	}
	record := Record{
		cloneField(pageRecord[0]),
		cloneField(pageRecord[3]),
		cloneField(pageRecord[4]),
		cloneField(pageRecord[5]),
		cloneField(pageRecord[6]),
		cloneField(pageRecord[7]),
		cloneField(pageRecord[8]),
		textField("publication_id", string(identity.PublicationID)),
	}
	return newBoundStageChunk(identity, context, ChunkPageFields, 0, []Record{record})
}

func NewPageBlobStageChunk(identity CommitIdentity, context OutputContext, kind ChunkKind, value []byte) (StageChunk, error) {
	if kind != ChunkHTML && kind != ChunkOriginalHTML {
		return StageChunk{}, ErrUnknownChunkKind
	}
	record := Record{
		textField("field_name", string(kind)),
		{Name: "field_bytes", Value: append([]byte(nil), value...)},
	}
	return newBoundStageChunk(identity, context, kind, 0, []Record{record})
}

func NewOutlinksStageChunk(identity CommitIdentity, ordinal uint64, context OutputContext, outlinks []string) (StageChunk, error) {
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return StageChunk{}, err
	}
	records, err := outputOutlinkRecords(context.finalTarget.CanonicalURL, outlinks)
	if err != nil {
		return StageChunk{}, err
	}
	return newBoundStageChunk(identity, context, ChunkOutlinks, ordinal, records)
}

func NewDiscoveriesStageChunk(identity CommitIdentity, ordinal uint64, context OutputContext, discoveries []OutputDiscovery) (StageChunk, error) {
	if len(discoveries) > MaxNonBlobStageBatchRecords {
		return StageChunk{}, ErrInputLimitExceeded
	}
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return StageChunk{}, err
	}
	if err := validateOutputDiscoveriesAgainstRunPolicy(context.runPolicy, discoveries); err != nil {
		return StageChunk{}, err
	}
	ordered := append([]OutputDiscovery(nil), discoveries...)
	sort.Slice(ordered, func(left, right int) bool {
		return string(ordered[left].JobID) < string(ordered[right].JobID)
	})
	records := make([]Record, 0, len(ordered))
	for index, discovery := range ordered {
		if index > 0 && discovery.JobID == ordered[index-1].JobID {
			return StageChunk{}, ErrDuplicateDiscovery
		}
		record, err := completeDiscoveryRecord(discovery)
		if err != nil {
			return StageChunk{}, err
		}
		records = append(records, record)
	}
	return newBoundStageChunk(identity, context, ChunkDiscoveries, ordinal, records)
}

func NewAliasesStageChunk(identity CommitIdentity, context OutputContext) (StageChunk, error) {
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return StageChunk{}, err
	}
	records, err := outputAliasRecords(context.aliases)
	if err != nil {
		return StageChunk{}, err
	}
	return newBoundStageChunk(identity, context, ChunkAliases, 0, records)
}

func NewImagesStageChunk(identity CommitIdentity, context OutputContext, images []OutputImage) (StageChunk, error) {
	records, err := outputImageRecords(images)
	if err != nil {
		return StageChunk{}, err
	}
	return newBoundStageChunk(identity, context, ChunkImages, 0, records)
}

func NewImageManifestStageChunk(identity CommitIdentity, context OutputContext, images []OutputImage) (StageChunk, error) {
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return StageChunk{}, err
	}
	imageRecords, err := outputImageRecords(images)
	if err != nil {
		return StageChunk{}, err
	}
	imageKeys := make([]string, 0, len(imageRecords))
	for _, record := range imageRecords {
		key, err := ImageDataKey(identity.PublicationID, context.finalTarget.CanonicalURL, string(record[0].Value))
		if err != nil {
			return StageChunk{}, err
		}
		imageKeys = append(imageKeys, key)
	}
	encodedKeys, err := json.Marshal(imageKeys)
	if err != nil || len(encodedKeys) > MaxImageManifestBytes {
		return StageChunk{}, ErrInvalidChunk
	}
	record := Record{
		textField("contract_version", "1"),
		textField("publication_id", string(identity.PublicationID)),
		textField("normalized_url", context.finalTarget.CanonicalURL),
		textField("image_count", canonicalDecimal(uint64(len(imageKeys)))),
		{Name: "image_keys", Value: encodedKeys},
	}
	return newBoundStageChunk(identity, context, ChunkImageManifest, 0, []Record{record})
}

func newBoundStageChunk(identity CommitIdentity, context OutputContext, kind ChunkKind, ordinal uint64, records []Record) (StageChunk, error) {
	if err := validateOutputCommitIdentity(context, identity); err != nil {
		return StageChunk{}, err
	}
	commitID, err := DeriveCommitID(identity)
	if err != nil {
		return StageChunk{}, err
	}
	chunk, err := newValidatedStageChunk(commitID, kind, ordinal, records)
	if err != nil {
		return StageChunk{}, err
	}
	chunk.context, chunk.identity = context, identity
	return chunk, nil
}

func validateStageChunkAuthority(chunk StageChunk, lease LeaseIdentity) error {
	if err := validateOutputCommitIdentity(chunk.context, chunk.identity); err != nil {
		return err
	}
	commitID, err := DeriveCommitID(chunk.identity)
	if err != nil || commitID != chunk.commitID || lease != chunk.context.lease {
		return ErrOutputContextMismatch
	}
	return validateChunkRecords(chunk)
}

func newValidatedStageChunk(commitID Digest, kind ChunkKind, ordinal uint64, records []Record) (StageChunk, error) {
	chunk := StageChunk{commitID: commitID, kind: kind, ordinal: ordinal, records: cloneRecords(records)}
	if err := validateNonzeroDigest(commitID); err != nil {
		return StageChunk{}, err
	}
	if err := validateChunkKind(kind); err != nil {
		return StageChunk{}, err
	}
	if ordinal > MaxExactInteger {
		return StageChunk{}, ErrInvalidUnsignedDecimal
	}
	if err := validateChunkRecords(chunk); err != nil {
		return StageChunk{}, err
	}
	return chunk, nil
}

// DeriveChunkDigest is pure serialization. Wire consumers additionally require
// the private full lease/transcript binding installed by public constructors.
func DeriveChunkDigest(chunk StageChunk) (Digest, error) {
	if err := validateNonzeroDigest(chunk.commitID); err != nil {
		return "", err
	}
	if err := validateChunkKind(chunk.kind); err != nil {
		return "", err
	}
	if chunk.ordinal > MaxExactInteger {
		return "", ErrInvalidUnsignedDecimal
	}
	if err := validateChunkRecords(chunk); err != nil {
		return "", err
	}
	section, err := EncodeSection("records", chunk.records)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	_, _ = hash.Write(F([]byte("mifolyo:stage-chunk:v2")))
	_, _ = hash.Write(F([]byte(chunk.commitID)))
	_, _ = hash.Write(F([]byte(chunk.kind)))
	_, _ = hash.Write(F([]byte(canonicalDecimal(chunk.ordinal))))
	_, _ = hash.Write(section)
	return Digest(hex.EncodeToString(hash.Sum(nil))), nil
}

func validateChunkRecords(chunk StageChunk) error {
	switch chunk.kind {
	case ChunkPageFields:
		return validatePageFieldsChunk(chunk)
	case ChunkHTML, ChunkOriginalHTML:
		return validateBlobChunk(chunk)
	case ChunkOutlinks:
		return validateOutlinksChunk(chunk)
	case ChunkDiscoveries:
		return validateDiscoveriesChunk(chunk)
	case ChunkAliases:
		return validateAliasesChunk(chunk)
	case ChunkImages:
		return validateImagesChunk(chunk)
	case ChunkImageManifest:
		return validateImageManifestChunk(chunk)
	default:
		return ErrUnknownChunkKind
	}
}

func validatePageFieldsChunk(chunk StageChunk) error {
	if chunk.ordinal != 0 || len(chunk.records) != 1 {
		return ErrInvalidChunk
	}
	record := chunk.records[0]
	if !hasFieldNames(record, "normalized_url", "content_type", "status_code", "last_crawled", "rendered", "render_policy_rule", "render_policy_sha256", "publication_id") {
		return ErrInvalidChunk
	}
	values, err := textValues(record)
	if err != nil {
		return err
	}
	if _, err := requireCanonicalURL(values[0]); err != nil {
		return err
	}
	if err := validateContentType(values[1]); err != nil {
		return err
	}
	statusCode, err := strconv.Atoi(values[2])
	if err != nil || statusCode < 100 || statusCode > 399 || fmt.Sprintf("%03d", statusCode) != values[2] {
		return ErrInvalidChunk
	}
	parsedTime, err := time.Parse(lastCrawledLayout, values[3])
	if err != nil || parsedTime.UTC().Format(lastCrawledLayout) != values[3] {
		return ErrInvalidChunk
	}
	if values[4] != "true" && values[4] != "false" {
		return ErrInvalidChunk
	}
	if values[4] == "false" && (values[5] != "" || values[6] != "") {
		return ErrInvalidChunk
	}
	if values[4] == "true" {
		if len(values[5]) == 0 || len(values[5]) > MaxRenderPolicyRuleIDBytes || containsControl(values[5]) {
			return ErrInvalidChunk
		}
		if _, err := parseNonzeroDigest(values[6]); err != nil {
			return err
		}
	}
	if _, err := parseNonzeroDigest(values[7]); err != nil {
		return err
	}
	return nil
}

func validateBlobChunk(chunk StageChunk) error {
	if chunk.ordinal != 0 || len(chunk.records) != 1 || !hasFieldNames(chunk.records[0], "field_name", "field_bytes") {
		return ErrInvalidChunk
	}
	record := chunk.records[0]
	if !utf8.Valid(record[0].Value) || string(record[0].Value) != string(chunk.kind) || !utf8.Valid(record[1].Value) || len(record[1].Value) > MaxPageBlobBytes {
		return ErrInvalidChunk
	}
	return nil
}

func validateOutlinksChunk(chunk StageChunk) error {
	if chunk.ordinal >= MaxOutlinkChunks || len(chunk.records) == 0 || len(chunk.records) > MaxNonBlobStageBatchRecords {
		return ErrInvalidChunk
	}
	previous := ""
	for index, record := range chunk.records {
		if !hasFieldNames(record, "target_url") {
			return ErrInvalidChunk
		}
		values, err := textValues(record)
		if err != nil {
			return err
		}
		if _, err := requireCanonicalURL(values[0]); err != nil {
			return err
		}
		if index > 0 {
			if values[0] == previous {
				return ErrDuplicateChunkRecord
			}
			if values[0] < previous {
				return ErrChunkRecordsOutOfOrder
			}
		}
		previous = values[0]
	}
	return nil
}

func validateDiscoveriesChunk(chunk StageChunk) error {
	if chunk.ordinal >= MaxDiscoveryChunks || len(chunk.records) == 0 || len(chunk.records) > MaxNonBlobStageBatchRecords {
		return ErrInvalidChunk
	}
	previous := ""
	for index, record := range chunk.records {
		if !hasFieldNames(record, "job_id", "canonical_url", "depth", "score_text", "group_id", "rate_scope_id", "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256") {
			return ErrInvalidChunk
		}
		values, err := textValues(record)
		if err != nil {
			return err
		}
		jobID, err := ParseJobID(values[0])
		if err != nil {
			return err
		}
		if err := validateJobURL(jobID, values[1]); err != nil {
			return err
		}
		if _, err := ParseUnsignedDecimal(values[2]); err != nil {
			return err
		}
		if _, err := ParseScoreText(values[3]); err != nil {
			return err
		}
		if _, err := ParseGroupID(values[4]); err != nil {
			return err
		}
		if _, err := ParseRateScopeID(values[5]); err != nil {
			return err
		}
		for _, digest := range values[6:] {
			if _, err := parseNonzeroDigest(digest); err != nil {
				return err
			}
		}
		if index > 0 {
			if values[0] == previous {
				return ErrDuplicateChunkRecord
			}
			if values[0] < previous {
				return ErrChunkRecordsOutOfOrder
			}
		}
		previous = values[0]
	}
	return nil
}

func validateAliasesChunk(chunk StageChunk) error {
	if chunk.ordinal != 0 || len(chunk.records) == 0 || len(chunk.records) > MaxAliasesPerJob {
		return ErrInvalidChunk
	}
	previous := ""
	depth := ""
	for index, record := range chunk.records {
		if !hasFieldNames(record, "url_id", "canonical_url", "depth") {
			return ErrInvalidChunk
		}
		values, err := textValues(record)
		if err != nil {
			return err
		}
		urlID, err := ParseJobID(values[0])
		if err != nil {
			return err
		}
		if err := validateJobURL(urlID, values[1]); err != nil {
			return err
		}
		if _, err := ParseUnsignedDecimal(values[2]); err != nil {
			return err
		}
		if index == 0 {
			depth = values[2]
		} else if values[2] != depth {
			return ErrInvalidChunk
		}
		if index > 0 {
			if values[0] == previous {
				return ErrDuplicateChunkRecord
			}
			if values[0] < previous {
				return ErrChunkRecordsOutOfOrder
			}
		}
		previous = values[0]
	}
	return nil
}

func validateImagesChunk(chunk StageChunk) error {
	if chunk.ordinal != 0 || len(chunk.records) == 0 || len(chunk.records) > MaxImagesPerPage {
		return ErrInvalidChunk
	}
	previous := ""
	for index, record := range chunk.records {
		if !hasFieldNames(record, "normalized_source_url", "alt") {
			return ErrInvalidChunk
		}
		values, err := textValues(record)
		if err != nil {
			return err
		}
		if _, err := requireCanonicalURL(values[0]); err != nil {
			return err
		}
		if len(values[1]) > MaxImageAltBytes {
			return ErrInvalidChunk
		}
		if index > 0 {
			if values[0] == previous {
				return ErrDuplicateChunkRecord
			}
			if values[0] < previous {
				return ErrChunkRecordsOutOfOrder
			}
		}
		previous = values[0]
	}
	return nil
}

func validateImageManifestChunk(chunk StageChunk) error {
	if chunk.ordinal != 0 || len(chunk.records) != 1 || !hasFieldNames(chunk.records[0], "contract_version", "publication_id", "normalized_url", "image_count", "image_keys") {
		return ErrInvalidChunk
	}
	values, err := textValues(chunk.records[0])
	if err != nil {
		return err
	}
	if values[0] != "1" {
		return ErrInvalidChunk
	}
	publicationID, err := parseNonzeroDigest(values[1])
	if err != nil {
		return err
	}
	if _, err := requireCanonicalURL(values[2]); err != nil {
		return err
	}
	countText, err := ParseUnsignedDecimal(values[3])
	if err != nil {
		return ErrInvalidChunk
	}
	imageCount, err := countText.Uint64()
	if err != nil || imageCount > MaxImagesPerPage || len(values[4]) > MaxImageManifestBytes {
		return ErrInvalidChunk
	}
	var imageKeys []string
	if err := json.Unmarshal([]byte(values[4]), &imageKeys); err != nil || imageKeys == nil || uint64(len(imageKeys)) != imageCount {
		return ErrInvalidChunk
	}
	canonicalJSON, err := json.Marshal(imageKeys)
	if err != nil || !bytes.Equal(canonicalJSON, []byte(values[4])) {
		return ErrInvalidChunk
	}
	prefix := "image_data:" + string(publicationID) + ":" + base64.RawURLEncoding.EncodeToString([]byte(values[2])) + ":"
	previousSource := ""
	for index, key := range imageKeys {
		if !strings.HasPrefix(key, prefix) || len(key) == len(prefix) {
			return ErrInvalidChunk
		}
		encodedSource := strings.TrimPrefix(key, prefix)
		if strings.Contains(encodedSource, ":") {
			return ErrInvalidChunk
		}
		source, err := base64.RawURLEncoding.DecodeString(encodedSource)
		if err != nil || base64.RawURLEncoding.EncodeToString(source) != encodedSource || !utf8.Valid(source) {
			return ErrInvalidChunk
		}
		if _, err := requireCanonicalURL(string(source)); err != nil {
			return err
		}
		if index > 0 && string(source) <= previousSource {
			if string(source) == previousSource {
				return ErrDuplicateChunkRecord
			}
			return ErrChunkRecordsOutOfOrder
		}
		previousSource = string(source)
	}
	return nil
}

func hasFieldNames(record Record, names ...string) bool {
	if len(record) != len(names) {
		return false
	}
	for index := range names {
		if record[index].Name != names[index] {
			return false
		}
	}
	return true
}

func textValues(record Record) ([]string, error) {
	values := make([]string, len(record))
	for index, field := range record {
		if !utf8.Valid(field.Value) {
			return nil, ErrInvalidChunk
		}
		values[index] = string(field.Value)
	}
	return values, nil
}

func cloneRecords(records []Record) []Record {
	cloned := make([]Record, len(records))
	for index := range records {
		cloned[index] = cloneRecord(records[index])
	}
	return cloned
}
