---
name: skytime-add-extension
description: Use when creating or modifying a Skytime Go extension (typed I/O wrapper for an external service), an OperationSpec/OperationFunc, credential handling, a TriggerSource, extbin registration, or anything under pkg/extension.
---

# Adding or modifying a Skytime extension

An extension is plain Go: implement `extension.Extension`, declare `OperationSpec`s
with typed kwargs, and a single generic Temporal activity ("ExecuteBatch") dispatches
your `OperationFunc`s. You never register activities, never touch `workflow.Context`.

## Read these first (in order)

1. `pkg/extension/extension.go` — the `Extension` interface (Name/Initialize/Operations). Read end-to-end; the doc comments are the contract.
2. `pkg/extension/builtin/http/http.go` — canonical reference implementation (5 ops, `endpoint()` factory, D4-14 idempotence policy).
3. `examples/http-github-webhook/extensions/github/github.go` + `response.go` — two-file layout (glue in github.go, typed outputs in response.go) and the value-or-pointer args pattern.
4. `docs/for-extension-developers/README.md` and `docs/architecture.md` — background on the parse/execute split.
5. For trigger extensions: `pkg/extension/trigger.go` (sealed TriggerSource) and `examples/http-github-webhook/extensions/github/webhook.go`.

## Hard rules (each is enforced by a test)

- NEVER import `go.temporal.io/*` in an extension. Enforced by `pkg/extension/extension_test.go::TestNoTemporalImportsInExtensionPackage` and module-wide by `pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList` (only pkg/{activity,interpreter,worker,cli,testing,extension/receiver,extension/schedules} may).
- `OperationFunc` takes `context.Context` (stdlib), never `workflow.Context`. Signature: `func(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error)`.
- `time.Now()`, randomness, real I/O are FINE inside OperationFuncs — they run in an activity, not workflow code.
- `Idempotent` is `*bool` and REQUIRED: write `Idempotent: extension.Ptr(true)` or `extension.Ptr(false)`. Nil → `ErrIdempotentRequired` ("Idempotent declaration required") at registration.
- Secrets never leave the activity: outputs, ActionRefs, trigger config, and logs carry credential ID strings only.

## Building a new extension, step by step

Layout: one package under `examples/<project>/extensions/<name>/` (downstream) or
`pkg/extension/builtin/<name>/` (bundled). Split: `<name>.go` (interface + factories +
OperationFuncs), `response.go` (output types), `webhook.go` (TriggerSource, if any).

1. **Extension type**: unexported struct, `New() extension.Extension`, `Name()` returning the .star global (e.g. "github"), compile-time `var _ extension.Extension = myExt{}`.
2. **Initialize**: return a `*starlarkstruct.Module` whose members are factory builtins (e.g. `client`, `endpoint`, `webhook`). Called ONCE per parser at Register time — not per invocation.
3. **Factory builtin**: unpack `credential?` (and any bound config like `base_url`) via `starlark.UnpackArgs`, return a sub-Module whose members are per-op builtins closing over the credential ID. Copy `clientFactory`/`newOpBuiltin` from the github extension.
4. **Op builtin → ActionRef**: build a frozen `*starlark.Dict` from kwargs and return a `&dag.ActionRef{Pos, Kind_: "<ext>.<op>", Kwargs, CredentialID}` struct literal (there is no constructor). `Pos` comes from `thread.CallFrame(1).Pos` — copy the `callerPosition` helper from github.go verbatim; using `fn.Position()` silently yields wrong error positions.
5. **Kwargs schema**: one struct per op with `star:"name,required"` (or optional without `,required`) tags. Tags MUST match the .star kwarg names. Supported field types are ONLY string / int* / bool / float64 / []string / map[string]string — the switch in `pkg/extension/schema.go::assignStarlarkToGo` (shared by parse-time and runtime decode). A new type means extending that one switch. `${ctx.x}` interpolation / lambdas are only legal for string-typed kwargs.
6. **Operations() map**: `{Name, Idempotent: extension.Ptr(...), Func, KwargsType: reflect.TypeOf(XArgs{}), DefaultTimeout}`. DefaultTimeout 0 = no per-action timeout (only activity-level applies); set 30s for HTTP-shaped ops.
7. **OperationFunc**: first line coerces args with a value-or-pointer helper (see `asGetRepoArgs` in github.go — the runtime decoder passes a VALUE, many tests pass pointers; a hard `args.(*XArgs)` cast panicked in production once, quick 260502-guu). Pass `ctx` into `http.NewRequestWithContext` so timeouts cancel in flight.
8. **Typed outputs** in response.go: each output struct gets json tags (these field names ARE what flow authors see on `ctx.<output_alias>`) and an empty exported marker `func (T) IsOperationOutput() {}` (marker is EXPORTED — do not write `isOperationOutput`). Stringify time.Time to RFC3339 UTC (`t.UTC().Format(time.RFC3339)`) — raw time.Time in outputs risks timezone drift in Temporal JSON. Returning `nil, err` is legal.

## Idempotency and what batching does with it

- `Idempotent: true` ops may be batched: `step(block=[...])` groups up to 50 into ONE ExecuteBatch activity invocation. A retryable failure mid-batch retries the WHOLE batch (`pkg/activity/execute_batch.go`) — so declare true only if re-running the op is genuinely safe.
- `Idempotent: false` ops always run one-per-activity. The parser lints mixed blocks and oversize blocks (`pkg/parser/linter.go`); `pkg/activity/validate_batch.go` re-checks at runtime as defense in depth (violations → NonRetryable ApplicationError).
- Application semantics beat RFC-7231: the http builtin declares PUT/DELETE non-idempotent on purpose (D4-14, locked). Pin your matrix in a test like `TestExtension_OperationsIdempotenceMatchesD4_14` (pkg/extension/builtin/http/http_test.go:29).
- `activity.WithMaxBlockSize` and `parser.WithMaxBlockSize` both default 50 and must stay coordinated (loosening only the parser side makes parse-accepted blocks fail at runtime).

## Error classification (retry semantics)

Extensions cannot construct `temporal.ApplicationError` (firewall). Signal via sentinel:

```go
return nil, fmt.Errorf("HTTP %d ...: %w", sc, extension.ErrNonRetryable)  // permanent → no retry
return nil, err                                                            // plain error → Temporal RETRIES
```

Convention (see `classifyGitHubError` in github.go and pkg/extension/builtin/http):
4xx → wrap `ErrNonRetryable`; 5xx / network / rate-limit → plain error (retryable).
Getting this backwards silently changes retry behavior and no test will catch it in
your extension unless you write the 404/500 pair (see Test pattern below).

## Credentials

- Sealed kinds in `pkg/extension/credential.go`: `*BearerCredential`, `*BasicCredential`, `*APIKeyCredential`. Secret fields are `extension.Secret` — redacts under %s %v %+v %#v %q, json, TextMarshaler; raw value ONLY via `.Reveal()`. Every `.Reveal()` call site is an audit boundary — add one only where the secret feeds the wire (e.g. `WithAuthToken(bearer.Token.Reveal())`).
- Your OperationFunc receives the resolved `Credential` JIT; the ActionRef only ever carried the ID string. Handle `cred == nil` (empty CredentialID skips resolution entirely) and wrong-kind creds gracefully — fall back to unauthenticated and let the API 401 → ErrNonRetryable (github.go `newClientForCredential`).
- Custom `CredentialHandler`s must wrap unknown IDs: `fmt.Errorf("%w: %s", extension.ErrUnknownCredential, id)` — that is what makes the activity classify the failure NonRetryable. See `pkg/extension/credfile/resolver.go` (TOML file handler; schema in `pkg/extension/credfile/doc.go`) and `pkg/extension/testing/fake_handler.go`.
- Never put a Secret or resolved credential in an output struct, trigger config, or log. `tests/firewall_credential_redaction_test.go::TestCredentialRedactionFirewall` bans `%+v`/`%#v` in production fmt calls but only scans a fixed targetDirs list (pkg/dag, pkg/extension{,builtin,receiver,builtin/core,schedules}, examples/.../extensions/github) — if you add an extension package elsewhere, add its dir to that list.

## TriggerSource (trigger extensions only)

- Concrete type MUST embed `extension.TriggerSourceSeal` (the unexported seal method arrives via promotion; you cannot declare it in your package). Add `var _ extension.TriggerSource = (*mySource)(nil)`.
- Implement `Kind()` ("myext.webhook"), `ReqSchema()` (the valid `req.<field>` names the parser's req-walker checks trigger lambdas against), and `MarshalJSON()` producing the `{kind, config}` envelope — config carries credential ID strings ONLY.
- Register an unmarshaler in Initialize: `_ = extension.RegisterTriggerSourceFactory(kind, fn)` — the `_ =` swallow is deliberate; the global registry errors on ANY second registration of the same kind, and multiple parsers re-Initialize.
- The source value is also returned by your `.star` factory builtin, so it needs starlark.Value methods (String/Type/Freeze/Truth/Hash) — copy `httpWebhookSource` in `pkg/extension/builtin/http/webhook.go`.
- HTTP mounting is STRUCTURAL: the receiver type-asserts anonymous interfaces (Path/Method mounter; `SignatureAlgo()/SignatureHeader()/SecretCredID()` for signing). A new HTTP source missing those exact method names silently mounts UNSIGNED with no error (`pkg/extension/receiver/` readSigningConfig). If you add a signature algo, `allowedSignatureAlgos` in builtin/http/webhook.go and the `newHMAC` switch in `pkg/extension/receiver/signature.go` must change together.
- Do not move `FakeTriggerSource` out of pkg/extension; sub-packages cannot satisfy the seal it uses.

## Registration in an extbin

Copy `examples/http-github-webhook/cmd/extbin/main.go` (~30 lines):

```go
root, err := cli.NewRootCommand(
    cli.WithExtensions(skyhttp.New(), skycore.New(), myext.New()),
    cli.WithCredfile(os.Getenv(cli.EnvCredfilePath)))
```

For parser-only use (tests, tools): `parser.NewParser(parser.WithExtensions(myext.New()))`.
`cli.WithCredfile` and `cli.WithCredentialHandler` are last-wins if both given.

## Docs conventions

- `docs/reference/builtins.md` is generated ONLY from pkg/parser (`// skytime:doc` markers). Adding an extension does NOT require regenerating it — but if you also touched any pkg/parser builtin or marker, run `go generate ./pkg/parser/`, commit builtins.md, and confirm with `go test ./tests/ -run TestDocgenDrift`.
- Flow-author-facing extension reference is hand-written: follow `docs/for-flow-authors/extensions/http.md`'s section structure (Setup → Operations → Idempotence → Response Classification → Credentials → Common Patterns).
- Document every op in the package doc comment with method/path/idempotence, like the tables atop github.go and http.go.

## Test pattern

Copy from `pkg/extension/builtin/http/http_test.go` and `examples/http-github-webhook/extensions/github/github_test.go`. Minimum set:

1. Registration gate: every op has non-nil Idempotent/Func/KwargsType (`TestExtension_RegistersWithoutError`).
2. Idempotence matrix pin (`TestExtension_OperationsIdempotenceMatchesEndpoints`).
3. Outputs implement OperationOutput (`TestExtension_OutputsImplementOperationOutput`).
4. Op behavior against `httptest.NewServer` — including the retry-semantics pair: 404 → `errors.Is(err, extension.ErrNonRetryable)`, 500 → plain retryable (`TestExtension_Get_404_NonRetryable` / `_500_Retryable`).
5. Parse gate: register with a real parser and parse a 2-line flow (`TestExtension_RegistersAndParsesAFlow`, http_test.go:343).
6. Tier-3 `.star` tests (`*_test.star` next to the flows, run by `extbin test <dir>`): mock with `tester.mock_action(extension="github", ...)` — the REGISTERED `Name()`, never the local Starlark variable (`gh = github.client(...)` → still `extension="github"`). See `examples/http-github-webhook/issue_triage_test.star` header. Mocks receive `kwargs["_credential_id"]` (ID string, never the secret).

Verify (all CI-mirrored; see .github/workflows/ci.yml):

```bash
go vet ./...
go test -race ./... -count=1
go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin
/tmp/extbin test ./examples/http-github-webhook/
```

Targeted firewall checks after extension work:

```bash
go test ./pkg/extension/ -run TestNoTemporalImportsInExtensionPackage -count=1
go test ./pkg/activity/ -run TestNoTemporalImportsOutsideAllowList -count=1
go test ./tests/ -run TestCredentialRedactionFirewall -count=1
```

## Modifying the extension SDK itself (pkg/extension)

- Sealed types (Credential's `isCredential()`, TriggerSource's `triggerSourceMarker()`) are sealed on purpose; adding a Credential kind means editing `pkg/extension/credential.go` itself and giving every Secret field full formatter coverage (mirror `pkg/extension/secret.go`; pin with a redaction-matrix test like `secret_test.go::TestSecret_FullRedactionMatrix`).
- `schema.go` is shared by parse-time kwarg validation AND runtime activity decode — a type added to `assignStarlarkToGo` changes both; add cases to both `ParseSchema` acceptance and decode tests.
- Registry rejections (empty name, nil spec/Func/KwargsType/Idempotent) are pinned by `pkg/extension/registry_test.go`; keep error strings stable — downstream code matches with `errors.Is(err, extension.ErrIdempotentRequired)`.
- `pkg/extension` must never import go.temporal.io (its own doc.go + firewall test) even though sibling dirs `receiver/` and `schedules/` are allow-listed "system extensions".
- Changing `receiver.HTTPMounter` or the signing accessor method set silently unmounts / unsigns every structural implementor — grep for anonymous interface assertions in `pkg/extension/receiver/` before touching any TriggerSource-facing signature.
- `posHash` is deliberately duplicated in `pkg/extension/receiver/workflow_id.go` and `pkg/extension/schedules/id.go`; they must stay byte-equal (parity test `schedules/id_test.go`).

## Common mistakes

- **Imported the Temporal SDK** (even transitively via a helper): `TestNoTemporalImportsOutsideAllowList` fails naming your file. Fix: signal non-retryable via `extension.ErrNonRetryable`, not `temporal.NewNonRetryableApplicationError`.
- **`args.(*XArgs)` hard cast** in an OperationFunc: unit tests that build pointer args pass; production (which decodes into a VALUE via `DecodeKwargsFromDict`) panics. Always use the value-or-pointer `asXArgs` helper.
- **Forgot `extension.Ptr(...)` on Idempotent**: registration fails with "operation %q: Idempotent declaration required" the first time an extbin or parser test runs — not at compile time.
- **4xx returned as plain error**: workflow retries a permanent failure until retry policy exhaustion (looks like a 30s hang per attempt). Wrap `ErrNonRetryable`.
- **Mock never matches in `.star` tests**: you used the local variable name (`gh`) instead of the registered `Name()` in `tester.mock_action(extension=...)`. Symptom: NonRetryable `no mock for <ext.op> at <file:line:col> (step ...)`.
- **New kwarg type (e.g. int slice, nested dict)**: silently unsupported — extend the switch in `pkg/extension/schema.go::assignStarlarkToGo` or the parser rejects/mis-decodes it. Only string kwargs may take `${ctx...}` lambdas.
- **Logged a credential struct with `%+v`**: `TestCredentialRedactionFirewall` fails (if your dir is scanned). Secret itself redacts everywhere, but keep `.Reveal()` out of any fmt/log path regardless.
- **Wrote `isOperationOutput()` (unexported)**: won't compile against `dag.OperationOutput`; the marker is exported `IsOperationOutput()` (see `pkg/dag/output.go` — deliberate deviation from older docs).
- **TriggerSource without `extension.TriggerSourceSeal` embed**: compile error "missing method triggerSourceMarker" at your `var _` assertion; you cannot implement the marker yourself.
- **Errored on duplicate `RegisterTriggerSourceFactory`**: second parser Initialize in the same process always collides; swallow with `_ =` like builtin/core/cron.go:272 and builtin/http/webhook.go:275.
- **Renamed/relocated github extension constants**: `pkg/extension/receiver/handler.go` imports `examples/http-github-webhook/extensions/github` for header/signature constants — a pkg→examples edge that breaks if you move the examples tree.
- **Testing retry behavior with `TestActivityEnvironment`**: it hardcodes Attempt=1; retry-path tests in pkg/activity use the unexported `withAttemptFunc` seam instead.
- **In Tier-3 tests, non-monotone attempt logic** (e.g. fail on attempt 2 only): the AttemptCounter is shared across tester.run's two replay runs — keyed mocks must be monotone (`attempt == 1` fails, later succeeds).
