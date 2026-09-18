package crawljobsv2

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This documentation guard reuses only the literal, test-owned reviewed oracle,
// never operationWireSpecifications or expectedOperationWireKeys. The existing
// read-only constructor test independently checks all 43 operations/52 variants.
// The shared current-document/empty-Lua vector remains FOUNDATION framing, not
// authority for a complete M3 Lua bundle (including when partial sources exist).
func TestProtocolWireLayoutReviewedOracle(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate protocol document")
	}
	document, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../../docs/crawl-jobs-v2.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(document), "#### 10.1.1 Closed KEYS/ARGV wire layout\n")
	if !ok {
		t.Fatal("missing normative wire layout")
	}
	section, _, ok = strings.Cut(section, "### 10.2 Administrative and run preparation transitions")
	if !ok {
		t.Fatal("missing wire layout boundary")
	}
	_, definitions, ok := strings.Cut(section, "```text\n")
	if !ok {
		t.Fatal("missing ordered key blocks")
	}
	definitions, _, ok = strings.Cut(definitions, "\n```")
	if !ok {
		t.Fatal("unterminated ordered key blocks")
	}
	blocks := make(map[string]string)
	for _, line := range strings.Split(definitions, "\n") {
		name, value, ok := strings.Cut(line, " = ")
		if !ok || blocks[name] != "" {
			t.Fatalf("invalid or duplicate key block %q", name)
		}
		blocks[name] = value
	}

	// Placeholder identities are used only by the literal oracle, not passed to
	// production constructors. They keep the full key-family comparison readable.
	f := &wireOracleFixture{
		runID: "R", job: SourceJob{JobID: "J"}, commitID: "C", reservationID: "Q",
		intent: ReservationIntent{Decision: PolicyDecision{
			GlobalScopeID: "global_scope_id", GroupScopeID: "group_scope_id", OriginScopeID: "origin_scope_id",
		}},
	}
	oracleKeys := func(plan wireOracleKeyPlan) []string {
		return wireOracleExpectedKeys(f, wireOracleOperationExpectation{keyPlan: plan})
	}
	wantBlocks := map[string][]string{
		"BOOT":         {"mifolyo:crawl:v2:durability"},
		"AUTH":         wireOracleAuthorityKeys(),
		"RUNS":         {"mifolyo:crawl:v2:runs", "mifolyo:crawl:v2:active_runs", "mifolyo:crawl:v2:unarchived_runs"},
		"LIVE":         wireOracleRuntimeRunKeys("R", "J")[:8],
		"RUN":          wireOracleRunKeys("R"),
		"JOB":          {wireOracleRunJobKey("R", "J")},
		"RATE(S)":      wireOracleRateScopeKeys("S"),
		"STAGE":        wireOracleStageKeys("C"),
		"LEGACY":       wireOracleLegacyKeys(),
		"OWNERS":       {"pages_queue:indexer_owner", "image_indexer_queue:owner"},
		"DOWNSTREAM":   wireOracleDownstreamKeys(),
		"WORK":         oracleKeys(wireOracleKeysJob),
		"REQUEST":      oracleKeys(wireOracleKeysReservation),
		"MAINT":        oracleKeys(wireOracleKeysRunMaintenance),
		"SHUTDOWN_RUN": {"mifolyo:crawl:v2:run:R", "mifolyo:crawl:v2:run:R:leased"},
	}
	if len(blocks) != len(wantBlocks) {
		t.Fatalf("key block count = %d, want %d", len(blocks), len(wantBlocks))
	}
	for name, want := range wantBlocks {
		got := protocolWireExpandBlock(t, blocks, name, "S", 0)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("normative %s key order differs from literal reviewed oracle\ngot  %q\nwant %q", name, got, want)
		}
	}

	// These formulas are test-owned reviewed data. Optional/repeated suffixes do
	// not disappear merely because the existing 52-variant oracle uses one job,
	// two shutdown runs, a nonempty purge cursor, and migration promotion.
	plans := map[wireOracleKeyPlan][2]string{
		wireOracleKeysApproveBoot:     {"BOOT", "1"},
		wireOracleKeysAuthority:       {"AUTH", "8"},
		wireOracleKeysRetire:          {"AUTH + RUNS + LEGACY", "16"},
		wireOracleKeysPromote:         {"AUTH + LIVE + LEGACY + DOWNSTREAM + RUN*m", "29+27*m"},
		wireOracleKeysMarkShutdown:    {"AUTH + P:active_runs + P:active_leases + P:stage_slots + P:stage_expiry + P:rate_scopes + RATE(global_scope_id) + OWNERS + SHUTDOWN_RUN*a", "19+2*a"},
		wireOracleKeysCreateRun:       {"AUTH + RUNS + RUN", "38"},
		wireOracleKeysRunRecords:      {"AUTH + RUNS + RUN + JOB*n", "38+n"},
		wireOracleKeysRun:             {"AUTH + RUNS + RUN", "38"},
		wireOracleKeysJob:             {"WORK", "44"},
		wireOracleKeysReservation:     {"REQUEST", "57"},
		wireOracleKeysStage:           {"WORK + STAGE", "117"},
		wireOracleKeysRunMaintenance:  {"MAINT", "42"},
		wireOracleKeysArchive:         {"AUTH + RUNS + P:active_leases + P:stage_expiry + P:stage_slots + RUN + DOWNSTREAM", "49"},
		wireOracleKeysPurge:           {"AUTH + RUNS + P:first_request_start + P:active_leases + P:stage_expiry + P:stage_slots + RUN + JOB*e", "42+e"},
		wireOracleKeysCleanStage:      {"AUTH + P:stage_expiry + P:stage_slots + STAGE", "83"},
		wireOracleKeysRateMaintenance: {"AUTH + P:rate_scopes", "9"},
	}
	recordTails := map[OperationName]string{
		OperationCreateRun: "groups", OperationEnqueueBatch: "source", OperationAuditRunBatch: "source",
		OperationStagePageFields: "page_fields", OperationStagePageBlob: "blob",
		OperationStageOutlinksBatch: "outlinks", OperationStageDiscoveriesBatch: "discoveries",
		OperationStageAliasesBatch: "aliases", OperationStageImagesBatch: "images", OperationStageImageManifest: "image_manifest",
	}
	tailBounds := map[string]string{
		"groups": "1..64", "source": "`1..500` enqueue; `0..100` audit", "page_fields": "1", "blob": "1",
		"outlinks": "1..64", "discoveries": "1..64", "aliases": "1..5", "images": "1..64", "image_manifest": "1",
	}
	tails := protocolWireMarkdownRows(t, section, "| Tail | Exact ordered RECORD fields | Record count |", 3)
	if len(tails) != 9 {
		t.Fatalf("record tail definitions = %d, want 9", len(tails))
	}
	tailFields := make(map[string]string)
	for _, row := range tails {
		if tailFields[row[0]] != "" || tailBounds[row[0]] != row[2] {
			t.Fatalf("unexpected record tail definition %q", row)
		}
		tailFields[row[0]] = row[1]
	}

	rows := protocolWireMarkdownRows(t, section, "| Operation | Ordered KEYS | KEYS count | s | ARGV tail |", 5)
	expectations := wireOracleOperationExpectations()
	wireOracleAssertInventory(t, expectations)
	if len(rows) != 43 {
		t.Fatalf("normative wire operations = %d, want 43", len(rows))
	}
	for i, expectation := range expectations {
		plan := plans[expectation.keyPlan]
		switch expectation.operation {
		case OperationActivateRun:
			plan = [2]string{"AUTH + RUNS + RUN + LEGACY", "43"}
		case OperationCommit:
			plan = [2]string{"WORK + STAGE + pages_queue", "118"}
		}
		tail := "none"
		switch expectation.tailKind {
		case wireOracleActiveRunIDTail:
			tail = "run_ids"
		case wireOracleRecordTail:
			tail = recordTails[expectation.operation]
			if tail == "" || tailFields[tail] != strings.Join(expectation.recordFields, " ") {
				t.Fatalf("%s normative RECORD fields differ from literal oracle", expectation.operation)
			}
		}
		want := []string{string(expectation.operation), plan[0], plan[1], strconv.Itoa(len(expectation.semanticFields)), tail}
		if !reflect.DeepEqual(rows[i], want) {
			t.Fatalf("wire row %d differs from literal reviewed oracle\ngot  %q\nwant %q", i, rows[i], want)
		}
	}

	responses := protocolWireMarkdownRows(t, string(document), "| Operation/status | Exact response array |", 2)
	checked := 0
	for _, row := range responses {
		if strings.HasPrefix(row[0], "`CJ2_START_REQUEST` /") || strings.HasPrefix(row[0], "any `CJ2_STAGE_*` /") {
			if len(strings.Split(row[1], ",")) != 9 {
				t.Fatalf("%s must retain nine response scalars", row[0])
			}
			checked++
		}
	}
	if checked != 2 {
		t.Fatalf("checked %d of the two nine-scalar response rows", checked)
	}
}

func protocolWireMarkdownRows(t *testing.T, document, header string, columns int) [][]string {
	t.Helper()
	_, rest, ok := strings.Cut(document, header+"\n")
	if !ok || strings.Count(document, header+"\n") != 1 {
		t.Fatalf("missing or duplicate normative table %q", header)
	}
	lines := strings.Split(rest, "\n")
	var rows [][]string
	for _, line := range lines[1:] { // Skip the Markdown separator, not a data row.
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != columns {
			t.Fatalf("wrong column count in %q", line)
		}
		for i, cell := range cells {
			cell = strings.TrimSpace(cell)
			if strings.Count(cell, "`") == 2 && strings.HasPrefix(cell, "`") && strings.HasSuffix(cell, "`") {
				cell = strings.Trim(cell, "`")
			}
			cells[i] = cell
		}
		rows = append(rows, cells)
	}
	return rows
}

func protocolWireExpandBlock(t *testing.T, blocks map[string]string, expression, scope string, depth int) []string {
	t.Helper()
	if depth > len(blocks) {
		t.Fatal("cyclic normative key blocks")
	}
	var keys []string
	for _, token := range strings.Fields(expression) {
		switch {
		case blocks[token] != "":
			keys = append(keys, protocolWireExpandBlock(t, blocks, blocks[token], scope, depth+1)...)
		case strings.HasPrefix(token, "RATE(") && strings.HasSuffix(token, ")"):
			keys = append(keys, protocolWireExpandBlock(t, blocks, blocks["RATE(S)"], token[5:len(token)-1], depth+1)...)
		case token == "T:image:{0..63}":
			for i := 0; i < 64; i++ {
				keys = append(keys, "mifolyo:crawl:v2:stage:C:image:"+strconv.Itoa(i))
			}
		default:
			token = strings.NewReplacer("P:", "mifolyo:crawl:v2:", "B", "mifolyo:crawl:v2:run:R", "T:", "mifolyo:crawl:v2:stage:C:").Replace(token)
			keys = append(keys, strings.ReplaceAll(token, ":S", ":"+scope))
		}
	}
	return keys
}
