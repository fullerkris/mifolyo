package crawljobsv2

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestAuthorizationExpiredCancelRunPublicAPIIsClosed(t *testing.T) {
	wantConstructorType := reflect.TypeOf((func(TransportGate, RunID) (OperationWireRequest, error))(nil))
	if got := reflect.TypeOf(NewAuthorizationExpiredCancelRunWireRequest); got != wantConstructorType {
		t.Fatalf("authorization-expiry constructor type = %v, want %v", got, wantConstructorType)
	}

	requestType := reflect.TypeOf(OperationWireRequest{})
	for index := 0; index < requestType.NumField(); index++ {
		if requestType.Field(index).IsExported() {
			t.Fatalf("OperationWireRequest exposes caller-settable field %q", requestType.Field(index).Name)
		}
	}

	fixture := newWireOracleFixture(t)
	gate := wireOracleGate(t, fixture, OperationCancelRun, wireOracleActive)
	request, err := NewAuthorizationExpiredCancelRunWireRequest(gate, fixture.runID)
	if err != nil {
		t.Fatalf("construct maintenance authorization-expiry cancellation: %v", err)
	}
	if request.Operation() != OperationCancelRun {
		t.Fatalf("maintenance constructor operation = %s, want %s", request.Operation(), OperationCancelRun)
	}

	for reason := range reasons {
		reason := reason
		t.Run("caller_reason_"+string(reason), func(t *testing.T) {
			_, err := NewCancelRunWireRequest(gate, CancelRunWireInput{RunID: fixture.runID, Reason: reason})
			if reason == ReasonOperatorCancelled || reason == ReasonSourceCancelled {
				if err != nil {
					t.Fatalf("caller cancellation reason %q was rejected: %v", reason, err)
				}
				return
			}
			if !errors.Is(err, ErrOperationWireArguments) {
				t.Fatalf("caller cancellation reason %q error = %v, want %v", reason, err, ErrOperationWireArguments)
			}
		})
	}
	if _, err := NewCancelRunWireRequest(gate, CancelRunWireInput{
		RunID:  fixture.runID,
		Reason: Reason("review_unknown_cancellation_reason"),
	}); !errors.Is(err, ErrOperationWireArguments) {
		t.Fatalf("unknown caller cancellation reason error = %v, want %v", err, ErrOperationWireArguments)
	}
}

func TestAuthorizationExpiredCancelRunUsesExistingExactWireContract(t *testing.T) {
	if len(operations) != wireOracleConstructorCount || len(operationWireOrder) != wireOracleConstructorCount || len(operationWireSpecifications) != wireOracleConstructorCount {
		t.Fatalf("operation inventory changed: operations=%d order=%d specifications=%d want=%d", len(operations), len(operationWireOrder), len(operationWireSpecifications), wireOracleConstructorCount)
	}
	specification := operationWireSpecifications[OperationCancelRun]
	if specification.operation != OperationCancelRun ||
		!reflect.DeepEqual(specification.gateModes, []GateMode{GateActive, GateCandidate}) ||
		!reflect.DeepEqual(specification.semanticFields, []string{"run_id", "reason"}) ||
		specification.tailKind != wireTailNone || specification.keyPlan != wireKeysRun {
		t.Fatal("authorization-expiry constructor did not reuse the closed CJ2_CANCEL_RUN specification")
	}

	fixture := newWireOracleFixture(t)
	expectations := wireOracleOperationExpectations()
	bundle, identities := newWireOracleScriptBindingSet(t, expectations, fixture.artifacts.contract)
	wantKeys := append(wireOracleAuthorityKeys(),
		"mifolyo:crawl:v2:runs",
		"mifolyo:crawl:v2:active_runs",
		"mifolyo:crawl:v2:unarchived_runs",
	)
	wantKeys = append(wantKeys, wireOracleRunKeys(fixture.runID)...)

	for _, variant := range []wireOracleGateVariant{wireOracleActive, wireOracleCandidateBeforeRetirement} {
		variant := variant
		t.Run(wireOracleVariantName(variant), func(t *testing.T) {
			gate := wireOracleGate(t, fixture, OperationCancelRun, variant)
			request, err := NewAuthorizationExpiredCancelRunWireRequest(gate, fixture.runID)
			if err != nil {
				t.Fatal(err)
			}

			if request.operation != OperationCancelRun || !request.initialized || !request.hasGate ||
				!reflect.DeepEqual(request.gate, gate) ||
				!reflect.DeepEqual(request.semantic, operationWireFields(
					textField("run_id", string(fixture.runID)),
					textField("reason", string(ReasonAuthorizationExpired)),
				)) ||
				!reflect.DeepEqual(request.keyContext, operationWireKeyContext{runID: fixture.runID}) ||
				len(request.records) != 0 || len(request.recordBytes) != 0 || len(request.repeated) != 0 || request.chunk.present {
				t.Fatal("authorization-expiry cancellation request has unexpected internal wire state")
			}

			built, err := BuildEvalSHARequest(bundle, request)
			if err != nil {
				t.Fatalf("build authorization-expiry cancellation: %v", err)
			}
			identity := identities[OperationCancelRun]
			if built.Operation() != OperationCancelRun || built.ScriptName() != "cj2_cancel_run.lua" ||
				built.ScriptSHA1() != identity.redisSHA1 || built.SourceSHA256() != identity.sourceSHA256 {
				t.Fatal("authorization-expiry cancellation selected the wrong operation-bound script")
			}

			gateArguments, err := gate.Arguments()
			if err != nil {
				t.Fatal(err)
			}
			wantArguments := append(gateArguments,
				[]byte(fixture.runID),
				[]byte(ReasonAuthorizationExpired),
			)
			assertReviewWireValues(t, "arguments", built.Arguments(), wantArguments)
			assertReviewWireStrings(t, "request keys", request.keys, wantKeys)
			assertReviewWireStrings(t, "built keys", built.Keys(), wantKeys)
			if wantSize := wireOracleRESPSize(built.ScriptSHA1(), built.Keys(), wantArguments); built.SerializedSize() != wantSize {
				t.Fatalf("serialized size = %d, want %d", built.SerializedSize(), wantSize)
			}
		})
	}
}

func TestAuthorizationExpiredCancelRunFailsClosedAndRemainsRedacted(t *testing.T) {
	fixture := newWireOracleFixture(t)
	gate := wireOracleGate(t, fixture, OperationCancelRun, wireOracleActive)
	request, err := NewAuthorizationExpiredCancelRunWireRequest(gate, fixture.runID)
	if err != nil {
		t.Fatal(err)
	}

	wrongOperationGate := wireOracleGate(t, fixture, OperationRecoverExpired, wireOracleActive)
	for _, test := range []struct {
		name      string
		gate      TransportGate
		runID     RunID
		wantError error
	}{
		{name: "zero gate", gate: TransportGate{}, runID: fixture.runID, wantError: ErrInvalidTransportGate},
		{name: "wrong operation gate", gate: wrongOperationGate, runID: fixture.runID, wantError: ErrInvalidTransportGate},
		{name: "invalid run identity", gate: gate, runID: RunID("not-a-run-id"), wantError: ErrOperationWireArguments},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAuthorizationExpiredCancelRunWireRequest(test.gate, test.runID); !errors.Is(err, test.wantError) {
				t.Fatalf("constructor error = %v, want %v", err, test.wantError)
			}
		})
	}

	expectations := wireOracleOperationExpectations()
	validBundle, _ := newWireOracleScriptBindingSet(t, expectations, fixture.artifacts.contract)
	if _, err := BuildEvalSHARequest(ScriptBindingSet{}, request); !errors.Is(err, ErrInvalidScriptBindingSet) {
		t.Fatalf("unsealed script authority error = %v, want %v", err, ErrInvalidScriptBindingSet)
	}
	otherContract := wireOracleDigestCanary("authorization-expiry-review-other-contract")
	wrongContractBundle, _ := newWireOracleScriptBindingSet(t, expectations, otherContract)
	if _, err := BuildEvalSHARequest(wrongContractBundle, request); !errors.Is(err, ErrScriptContractMismatch) {
		t.Fatalf("mismatched contract authority error = %v, want %v", err, ErrScriptContractMismatch)
	}
	if _, err := BuildEvalSHARequest(validBundle, OperationWireRequest{}); !errors.Is(err, ErrInvalidOperationWire) {
		t.Fatalf("zero wire request error = %v, want %v", err, ErrInvalidOperationWire)
	}

	tamperedRun := request
	tamperedRun.semantic = cloneRecord(request.semantic)
	tamperedRun.semantic[0].Value = []byte(fixture.otherRunID)
	if _, err := BuildEvalSHARequest(validBundle, tamperedRun); !errors.Is(err, ErrOperationWireKeys) {
		t.Fatalf("run identity substitution error = %v, want %v", err, ErrOperationWireKeys)
	}

	tamperedGate := request
	for index := range tamperedGate.gate.arguments {
		tamperedGate.gate.arguments[index] = append([]byte(nil), tamperedGate.gate.arguments[index]...)
	}
	tamperedGate.gate.arguments[3] = append(tamperedGate.gate.arguments[3], 'x')
	if _, err := BuildEvalSHARequest(validBundle, tamperedGate); !errors.Is(err, ErrInvalidTransportGate) {
		t.Fatalf("manifest authority substitution error = %v, want %v", err, ErrInvalidTransportGate)
	}

	built, err := BuildEvalSHARequest(validBundle, request)
	if err != nil {
		t.Fatal(err)
	}
	rawValues := []string{string(fixture.runID), string(ReasonAuthorizationExpired)}
	for _, value := range request.keys {
		rawValues = append(rawValues, string(value))
	}
	for _, value := range built.Arguments() {
		if len(value) != 0 {
			rawValues = append(rawValues, string(value))
		}
	}
	for _, test := range []redactionSurfaceCase{
		{name: "operation wire request", typeName: "OperationWireRequest", value: request, composite: true, rawValues: rawValues},
		{name: "evalsha request", typeName: "EvalSHARequest", value: built, composite: true, rawValues: rawValues},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertRedactionSurfaces(t, test)
		})
	}
}

func assertReviewWireValues(t *testing.T, label string, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d", label, len(got), len(want))
	}
	for index := range want {
		if !bytes.Equal(got[index], want[index]) {
			t.Fatalf("%s[%d] mismatch", label, index)
		}
	}
}

func assertReviewWireStrings(t *testing.T, label string, got [][]byte, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d", label, len(got), len(want))
	}
	for index := range want {
		if string(got[index]) != want[index] {
			t.Fatalf("%s[%d] mismatch", label, index)
		}
	}
}
