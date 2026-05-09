package receiver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeJSON decodes the recorder body into a map[string]any.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

func TestStatusMapping_LockedConstants(t *testing.T) {
	// LOCKED — D-7.1-15 taxonomy. Any change here requires a CONTEXT.md
	// amendment.
	assert.Equal(t, "ok", errorClassOK)
	assert.Equal(t, "signature_mismatch", errorClassSignatureMismatch)
	assert.Equal(t, "bad_request", errorClassBadRequest)
	assert.Equal(t, "lambda_panic", errorClassLambdaPanic)
	assert.Equal(t, "dispatch_failed", errorClassDispatchFailed)
	assert.Equal(t, "event_filtered", errorClassEventFiltered)
	assert.Equal(t, "duplicate_skipped", errorClassDuplicateSkipped)
}

func TestWriteJSON_Success(t *testing.T) {
	t.Run("multi", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeSuccessResponse(rec, []string{"a", "b"})
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		body := decodeJSON(t, rec)
		ids, ok := body["workflow_ids"].([]any)
		require.True(t, ok, "expected workflow_ids list, got %#v", body)
		assert.Equal(t, []any{"a", "b"}, ids)
		_, hasSingle := body["workflow_id"]
		assert.False(t, hasSingle)
	})
}

func TestWriteJSON_SingleWorkflow(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSuccessResponse(rec, []string{"webhook_demo/abc/xyz"})
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "webhook_demo/abc/xyz", body["workflow_id"])
	_, hasMulti := body["workflow_ids"]
	assert.False(t, hasMulti, "single-workflow form must not emit workflow_ids array")
}

func TestWriteJSON_Duplicate(t *testing.T) {
	rec := httptest.NewRecorder()
	writeDuplicateResponse(rec, "existing-id")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "duplicate; skipped", body["status"])
	assert.Equal(t, "existing-id", body["workflow_id"])
}

func TestWriteJSON_EventFiltered(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEventFilteredResponse(rec)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "event filtered", body["status"])
}

func TestWriteJSON_Unauthorized(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUnauthorizedResponse(rec)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "unauthorized", body["error"])
	_, ok := body["detail"]
	assert.False(t, ok, "401 unauthorized must NOT have a detail field — D-7.1-14 forbids leaking signature-vs-missing distinction")
}

func TestWriteJSON_BadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	writeBadRequestResponse(rec, "json parse failed")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "bad_request", body["error"])
	assert.Equal(t, "json parse failed", body["detail"])
}

func TestWriteJSON_UnsupportedMediaType(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUnsupportedMediaTypeResponse(rec)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "unsupported_media_type", body["error"])
	_, ok := body["detail"]
	assert.False(t, ok, "415 must NOT have a detail field — error class communicates everything the source provider needs")
}

func TestWriteJSON_Internal(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalResponse(rec, "starlark eval error")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "internal", body["error"])
	assert.Equal(t, "starlark eval error", body["detail"])
}

func TestWriteJSON_Upstream(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUpstreamResponse(rec)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	body := decodeJSON(t, rec)
	assert.Equal(t, "upstream", body["error"])
	assert.Equal(t, "temporal_unavailable", body["detail"])
}
