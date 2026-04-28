# expects: cannot mix idempotent and non-idempotent operations in a block

# This fixture exercises D2-05: a step(block=[...]) cannot mix
# idempotent and non-idempotent operations. The corpus test harness in
# pkg/parser/fixtures_test.go registers the test-only `fake_ext` extension
# whose `echo` op is idempotent and `post` op is NOT — the mixed block
# below trips the parser's lintMixedIdempotency pass.

flow(
    name = "mixed",
    inputs = {},
    steps = [
        step(block = [
            fake_ext.echo(msg = "ok"),       # idempotent
            fake_ext.post(payload = "x"),    # NOT idempotent
        ]),
    ],
)
