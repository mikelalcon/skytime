# cross_file_flow.star — declares flow check_user. Loaded by
# cross_file_trigger.star which declares the trigger.
#
# Exports a sentinel (`marker`) so the loader has at least one bindable
# symbol — go.starlark.net's load() requires every symbol in the load
# list to be bound, and load() with zero symbols is a syntax error.
# Names with leading underscores are not exported, so we use a plain
# identifier instead.

marker = True

flow(
    name = "check_user",
    inputs = {"repo": "string"},
    steps = [],
)
