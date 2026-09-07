package crawljobsv2

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSensitiveValuesRedactStringGoStringAndJSON(t *testing.T) {
	const (
		urlCanary  = "https://example.com/private-redaction-canary"
		textCanary = "ARBITRARY_SECRET_CANARY"
		hexCanary  = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	)
	token := LeaseToken(hexCanary)
	reservationID := ReservationID(hexCanary)
	target := RequestTarget{URLID: JobID(hexCanary), CanonicalURL: urlCanary}
	lease := LeaseIdentity{
		RunID: RunID("deadbeefdeadbeefdeadbeefdeadbeef"), JobID: JobID(hexCanary),
		OwnerID: OwnerID("deadbeefdeadbeefdeadbeefdeadbeef"), Fence: 7, Token: token,
	}
	decision := PolicyDecision{RequestKind: RequestDocument, TargetURLID: JobID(hexCanary), GroupID: GroupID(textCanary)}
	page := OutputPage{NormalizedURL: urlCanary, HTML: []byte(textCanary), ContentType: textCanary}
	discovery := OutputDiscovery{JobID: JobID(hexCanary), CanonicalURL: urlCanary, Decision: decision}
	source := SourceJob{JobID: JobID(hexCanary), CanonicalURL: urlCanary, GroupID: GroupID(textCanary), Decision: decision}
	chunk := StageChunk{commitID: Digest(hexCanary), kind: ChunkHTML, records: []Record{{{Name: textCanary, Value: []byte(textCanary)}}}}
	start := StartRequestResponse{
		status:      StatusStarted,
		started:     &StartRequestStarted{reservationID: reservationID},
		rateBlocked: &StartRequestRateBlocked{scopeID: Digest(hexCanary)},
	}
	values := []any{
		token,
		reservationID,
		CanonicalOrigin(urlCanary),
		Field{Name: textCanary, Value: []byte(textCanary)},
		Record{{Name: textCanary, Value: []byte(textCanary)}},
		target,
		lease,
		decision,
		PolicyDecisionInput{Target: target, GroupID: GroupID(textCanary)},
		PolicyGroup{GroupID: GroupID(textCanary)},
		ReservationIntent{Lease: lease, Target: target, Decision: decision},
		PublicationIdentity{RunID: lease.RunID, JobID: lease.JobID, OutputDigest: Digest(hexCanary)},
		CommitIdentity{RunID: lease.RunID, JobID: lease.JobID, OwnerID: lease.OwnerID, Token: token},
		source,
		page,
		OutputImage{NormalizedSourceURL: urlCanary, Alt: textCanary},
		discovery,
		SuccessfulDocumentRequest{target: target, lease: lease},
		OutputContext{finalTarget: target, lastCrawled: textCanary, aliases: []outputAlias{{CanonicalURL: urlCanary}}},
		CrawlOutput{Page: page, Outlinks: []string{urlCanary}, Discoveries: []OutputDiscovery{discovery}},
		chunk,
		RejectReadyTransitionInput{RunID: lease.RunID, Job: source},
		TryClaimTransitionInput{Job: source, Lease: lease},
		ReleaseBeforeIOTransitionInput{Lease: lease},
		RetryTransitionInput{Lease: lease},
		DeadTransitionInput{Lease: lease},
		CancelJobTransitionInput{Lease: lease},
		CompleteNoOutputTransitionInput{Lease: lease},
		AbortStageTransitionInput{Lease: lease, CommitID: Digest(hexCanary)},
		StartRequestStarted{reservationID: reservationID},
		StartRequestRateBlocked{scopeID: Digest(hexCanary)},
		LeaseLostResponse{currentFence: 7},
		start,
	}
	for _, value := range values {
		formats := []string{
			fmt.Sprintf("%v", value),
			fmt.Sprintf("%+v", value),
			fmt.Sprintf("%#v", value),
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
		formats = append(formats, string(encoded))
		for _, formatted := range formats {
			for _, canary := range []string{urlCanary, textCanary, hexCanary} {
				if strings.Contains(formatted, canary) {
					t.Fatalf("%T formatting exposed sensitive input", value)
				}
			}
		}
	}
}

func TestSensitivePrimitiveJSONIsValidAndExplicitlyRedacted(t *testing.T) {
	for _, value := range []any{
		LeaseToken("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		ReservationID("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		CanonicalOrigin("https://example.com:443"),
	} {
		encoded, err := json.Marshal(value)
		if err != nil || !json.Valid(encoded) || !strings.Contains(string(encoded), `"redacted":true`) {
			t.Fatalf("%T redacted JSON = %s, err=%v", value, encoded, err)
		}
	}
}
