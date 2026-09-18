package crawljobsv2

import (
	"errors"
	"fmt"
	"log/slog"
)

var (
	errInvalidScriptLoadPlan = errors.New("crawljobsv2: invalid script load plan")
	errScriptLoadReply       = errors.New("crawljobsv2: invalid script load reply")
)

// scriptLoadPlan retains only the sealed source and an immutable retry snapshot.
// It is a dormant identity helper, not permission to load or dispatch a command.
type scriptLoadPlan struct {
	source string
	retry  EvalSHARequest
}

func prepareScriptLoad(bundle ScriptBindingSet, retry EvalSHARequest) (scriptLoadPlan, error) {
	// Revalidate the complete bundle, including rehashing every source's SHA-256,
	// before retaining the exact immutable source bytes for SCRIPT LOAD.
	binding, err := bundle.bindingFor(retry.operation)
	if err != nil {
		return scriptLoadPlan{}, err
	}
	if retry.bundleSeal != *bundle.seal || retry.scriptName != binding.sourceName ||
		retry.scriptSHA1 != binding.redisSHA1 || retry.sourceSHA256 != binding.sourceSHA256 {
		return scriptLoadPlan{}, ErrScriptBindingMismatch
	}
	retry.keys = cloneByteSlices(retry.keys)
	retry.arguments = cloneByteSlices(retry.arguments)
	return scriptLoadPlan{source: binding.source, retry: retry}, nil
}

func (plan scriptLoadPlan) commandArguments() [][]byte {
	if plan.source == "" {
		return nil
	}
	return [][]byte{[]byte("SCRIPT"), []byte("LOAD"), []byte(plan.source)}
}

// verifyScriptLoad binds reply interpretation to the existing private transport
// capability only. authority.valid does NOT prove fresh connection/boot/marker
// checks. M5 must perform those checks before SCRIPT LOAD, then verify its reply
// before dispatching the saved retry. Neither helper performs I/O or those checks.
func (authority transportAuthority) verifyScriptLoad(plan scriptLoadPlan, raw any) (EvalSHARequest, error) {
	if !authority.valid() {
		return EvalSHARequest{}, ErrInvalidResponseAuthority
	}
	if plan.source == "" {
		return EvalSHARequest{}, errInvalidScriptLoadPlan
	}
	loadedSHA1, ok := raw.(string)
	if !ok || !isLowerHex(loadedSHA1, 40) || loadedSHA1 != plan.retry.scriptSHA1 {
		return EvalSHARequest{}, errScriptLoadReply
	}
	retry := plan.retry
	retry.keys = cloneByteSlices(retry.keys)
	retry.arguments = cloneByteSlices(retry.arguments)
	return retry, nil
}

func (scriptLoadPlan) String() string   { return redactedString("scriptLoadPlan") }
func (scriptLoadPlan) GoString() string { return redactedString("scriptLoadPlan") }
func (scriptLoadPlan) Format(state fmt.State, verb rune) {
	formatRedacted(state, verb, redactedString("scriptLoadPlan"))
}
func (scriptLoadPlan) MarshalJSON() ([]byte, error) { return redactedJSON("scriptLoadPlan") }
func (scriptLoadPlan) MarshalText() ([]byte, error) {
	return redactedCompositeText("scriptLoadPlan")
}
func (scriptLoadPlan) LogValue() slog.Value { return redactedLogValue("scriptLoadPlan") }
