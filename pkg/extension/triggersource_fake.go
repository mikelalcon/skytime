package extension

import (
	"encoding/json"
	"fmt"
	"sort"
)

// FakeTriggerSource is the canonical TriggerSource test stub for Phase 7+
// tests. It lives in pkg/extension (NOT in pkg/extension/testing) because
// the unexported triggerSourceMarker() seal can only be satisfied from
// within package extension — Go's package-visibility rules forbid sub-
// packages from implementing parent-package unexported methods.
//
// Provided as exported test infrastructure (not gated by _test.go) so any
// cross-package test (pkg/parser, pkg/dag, pkg/worker, pkg/interpreter)
// can construct and round-trip a TriggerSource without re-declaring its
// own stub. NOT importable from production code by convention; reviewers
// reject any non-test caller.
//
// MarshalJSON emits the {kind, config} envelope per D-07-09:
//
//	{"kind":"<KindName>","config":{"req_fields":[...],"credential_id":"<id>"}}
//
// CredentialIDInConfig is the test-only knob to confirm credential IDs
// round-trip through {kind, config} without exposing Secrets. Empty by
// default — when blank the credential_id key is omitted.
//
// DEVIATION FROM PLAN (Rule 1 — Bug): The plan's <interfaces> block placed
// FakeTriggerSource in pkg/extension/testing/triggersource.go. That layout
// fails to compile because triggerSourceMarker is unexported in package
// extension and Go does NOT permit sub-packages to satisfy a parent-
// package unexported-method seal (verified at compile time:
// "cannot use ... as extension.TriggerSource value ... unexported method
// triggerSourceMarker"). Moved into package extension itself; the
// cross-package test-reuse goal is preserved because the type is exported
// from pkg/extension directly.
type FakeTriggerSource struct {
	KindName             string
	ReqFields            []string
	CredentialIDInConfig string
}

// Kind returns the configured kind string.
func (f *FakeTriggerSource) Kind() string { return f.KindName }

// ReqSchema returns a sorted COPY of the configured req fields. The copy
// + sort make the output deterministic regardless of how callers populate
// ReqFields, which is what the parser-time req-walker (Plan 03) expects.
func (f *FakeTriggerSource) ReqSchema() []string {
	out := append([]string(nil), f.ReqFields...)
	sort.Strings(out)
	return out
}

// MarshalJSON emits the {kind, config} envelope. config carries the
// req_fields slice plus an optional credential_id string. NO Secret-typed
// field reaches the marshaled bytes; D-07-09 / D-07-10 enforce this at
// the contract level and trigger_test.go::TestFakeTriggerSource_NoSecretInConfig
// asserts it concretely.
func (f *FakeTriggerSource) MarshalJSON() ([]byte, error) {
	cfg := map[string]any{
		"req_fields": f.ReqSchema(),
	}
	if f.CredentialIDInConfig != "" {
		cfg["credential_id"] = f.CredentialIDInConfig
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, f.KindName, string(cfgBytes))), nil
}

// triggerSourceMarker satisfies the TriggerSource seal. Only valid because
// this file is in package extension; sub-packages cannot satisfy the seal
// (see DEVIATION note on FakeTriggerSource).
func (*FakeTriggerSource) triggerSourceMarker() {}

// RegisterFakeFactories installs unmarshal factories for two test kinds:
// "skytime.test.webhook" (req fields: payload, headers — populated by the
// caller via cfg.req_fields) and "skytime.test.cron" (typically req
// fields: scheduled_time, workflow_attempt). Each registered factory
// closure pins its KindName so the round-tripped FakeTriggerSource carries
// the correct discriminator.
//
// Errors from RegisterTriggerSourceFactory are intentionally swallowed
// (`_ = ...`) so re-registration on a second test-package invocation does
// not panic. Tests should call this once in TestMain or in a test setup
// helper. The strict-collision error from the second call is the expected
// no-op signal.
func RegisterFakeFactories() {
	_ = RegisterTriggerSourceFactory("skytime.test.webhook", func(data []byte) (TriggerSource, error) {
		var cfg struct {
			ReqFields            []string `json:"req_fields"`
			CredentialIDInConfig string   `json:"credential_id"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		return &FakeTriggerSource{
			KindName:             "skytime.test.webhook",
			ReqFields:            cfg.ReqFields,
			CredentialIDInConfig: cfg.CredentialIDInConfig,
		}, nil
	})
	_ = RegisterTriggerSourceFactory("skytime.test.cron", func(data []byte) (TriggerSource, error) {
		var cfg struct {
			ReqFields            []string `json:"req_fields"`
			CredentialIDInConfig string   `json:"credential_id"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		return &FakeTriggerSource{
			KindName:             "skytime.test.cron",
			ReqFields:            cfg.ReqFields,
			CredentialIDInConfig: cfg.CredentialIDInConfig,
		}, nil
	})
}
