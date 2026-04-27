package dag

import (
	"fmt"
	"time"

	"go.starlark.net/starlark"
)

// RetryPolicy is the DSL-08 retry-kwargs payload. Pure data — Phase 1 never
// executes anything; Phase 2 forwards these into Temporal's RetryPolicy when
// dispatching the activity.
//
// Implements starlark.Unpacker so the step() and for_each_parallel() builtins
// can do `starlark.UnpackArgs("step", args, kwargs, "retry?", &retry)` and have
// a Starlark dict literal decode into this struct directly. The dict shape is:
//
//	{
//	    "initial_interval":      "1s",        # time.ParseDuration string
//	    "backoff_coefficient":   2.0,         # number
//	    "max_attempts":          5,           # int
//	    "non_retryable_errors":  ["FOO"],     # list of strings
//	}
//
// Unknown keys produce errors (catches typos like "max_attempt" vs "max_attempts").
type RetryPolicy struct {
	InitialInterval    time.Duration
	BackoffCoefficient float64
	MaxAttempts        int
	NonRetryableErrors []string
}

// Compile-time guarantee: *RetryPolicy implements starlark.Unpacker.
var _ starlark.Unpacker = (*RetryPolicy)(nil)

// Unpack decodes a Starlark *Dict into the receiver.
func (r *RetryPolicy) Unpack(v starlark.Value) error {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("retry must be a dict, got %s", v.Type())
	}
	for _, item := range d.Items() {
		keyStr, ok := item[0].(starlark.String)
		if !ok {
			return fmt.Errorf("retry dict key must be string, got %s", item[0].Type())
		}
		key := string(keyStr)
		switch key {
		case "initial_interval":
			s, ok := item[1].(starlark.String)
			if !ok {
				return fmt.Errorf("retry.initial_interval must be string (e.g. \"1s\"), got %s", item[1].Type())
			}
			dur, err := time.ParseDuration(string(s))
			if err != nil {
				return fmt.Errorf("retry.initial_interval: %w", err)
			}
			r.InitialInterval = dur
		case "backoff_coefficient":
			switch x := item[1].(type) {
			case starlark.Float:
				r.BackoffCoefficient = float64(x)
			case starlark.Int:
				// Accept Int for "2" being equivalent to 2.0 — common
				// authoring convenience; Starlark literal `2` is Int.
				i, ok := x.Int64()
				if !ok {
					return fmt.Errorf("retry.backoff_coefficient: integer overflow")
				}
				r.BackoffCoefficient = float64(i)
			default:
				return fmt.Errorf("retry.backoff_coefficient must be number, got %s", item[1].Type())
			}
		case "max_attempts":
			i, ok := item[1].(starlark.Int)
			if !ok {
				return fmt.Errorf("retry.max_attempts must be int, got %s", item[1].Type())
			}
			v, ok := i.Int64()
			if !ok {
				return fmt.Errorf("retry.max_attempts: integer overflow")
			}
			r.MaxAttempts = int(v)
		case "non_retryable_errors":
			lst, ok := item[1].(*starlark.List)
			if !ok {
				return fmt.Errorf("retry.non_retryable_errors must be list, got %s", item[1].Type())
			}
			iter := lst.Iterate()
			var x starlark.Value
			for iter.Next(&x) {
				s, ok := x.(starlark.String)
				if !ok {
					iter.Done()
					return fmt.Errorf("retry.non_retryable_errors entries must be string, got %s", x.Type())
				}
				r.NonRetryableErrors = append(r.NonRetryableErrors, string(s))
			}
			iter.Done()
		default:
			return fmt.Errorf("retry: unknown key %q (allowed: initial_interval, backoff_coefficient, max_attempts, non_retryable_errors)", key)
		}
	}
	return nil
}

// Timeout is the DSL-08 timeout-kwargs payload. Like RetryPolicy, pure data
// with a starlark.Unpacker decoder.
type Timeout struct {
	StartToClose    time.Duration
	ScheduleToStart time.Duration
}

// Compile-time guarantee: *Timeout implements starlark.Unpacker.
var _ starlark.Unpacker = (*Timeout)(nil)

// Unpack decodes a Starlark *Dict like
// `{"start_to_close": "30s", "schedule_to_start": "5s"}` into the receiver.
func (t *Timeout) Unpack(v starlark.Value) error {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("timeout must be a dict, got %s", v.Type())
	}
	for _, item := range d.Items() {
		keyStr, ok := item[0].(starlark.String)
		if !ok {
			return fmt.Errorf("timeout dict key must be string, got %s", item[0].Type())
		}
		key := string(keyStr)
		valStr, ok := item[1].(starlark.String)
		if !ok {
			return fmt.Errorf("timeout.%s must be string (e.g. \"30s\"), got %s", key, item[1].Type())
		}
		dur, err := time.ParseDuration(string(valStr))
		if err != nil {
			return fmt.Errorf("timeout.%s: %w", key, err)
		}
		switch key {
		case "start_to_close":
			t.StartToClose = dur
		case "schedule_to_start":
			t.ScheduleToStart = dur
		default:
			return fmt.Errorf("timeout: unknown key %q (allowed: start_to_close, schedule_to_start)", key)
		}
	}
	return nil
}
