package dynamicrecords

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer returns a SharedServer with an empty buffer, suitable for
// handler unit tests (no TLS, no listener).
func newTestServer() *SharedServer {
	return &SharedServer{
		buffer:     NewRRBuffer(),
		defaultTTL: 300,
	}
}

// decodeResponse decodes the JSON body of a recorded HTTP response.
func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return resp
}

func assertJSONContentType(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want \"application/json\"", ct)
	}
}

// --- jsonError ---

func TestJsonError(t *testing.T) {
	w := httptest.NewRecorder()
	jsonError(w, "something went wrong", http.StatusBadRequest)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	assertJSONContentType(t, w)

	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
	if resp.Error != "something went wrong" {
		t.Errorf("error field: got %q, want \"something went wrong\"", resp.Error)
	}
}

// --- /records ---

func TestHandleRecords_Success(t *testing.T) {
	s := newTestServer()
	body := `{"records": ["test.example.com. 300 IN A 192.0.2.1", "test.example.com. 300 IN A 192.0.2.2"]}`
	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	assertJSONContentType(t, w)

	resp := decodeResponse(t, w)
	if !resp.OK {
		t.Errorf("expected ok:true, got error: %s", resp.Error)
	}
	if resp.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestHandleRecords_WrongMethod(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/records", nil)
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", w.Code)
	}
	assertJSONContentType(t, w)
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

func TestHandleRecords_BadJSON(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	assertJSONContentType(t, w)
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

func TestHandleRecords_EmptyRecords(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString(`{"records":[]}`))
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

func TestHandleRecords_InvalidRecord(t *testing.T) {
	s := newTestServer()
	body := `{"records": ["this is not a valid record"]}`
	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error field")
	}
}

func TestHandleRecords_MixedQName(t *testing.T) {
	s := newTestServer()
	body := `{"records": ["a.example.com. 300 IN A 1.2.3.4", "b.example.com. 300 IN A 1.2.3.5"]}`
	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

func TestHandleRecords_MixedQType(t *testing.T) {
	s := newTestServer()
	body := `{"records": ["x.example.com. 300 IN A 1.2.3.4", "x.example.com. 300 IN AAAA ::1"]}`
	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleRecords(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

// --- /records/delete ---

func TestHandleDelete_Success(t *testing.T) {
	s := newTestServer()

	// Pre-populate buffer via handleRecords
	addBody := `{"records": ["del.example.com. 300 IN A 10.0.0.1"]}`
	addReq := httptest.NewRequest(http.MethodPost, "/records", bytes.NewBufferString(addBody))
	s.handleRecords(httptest.NewRecorder(), addReq)

	delBody := `{"records": ["del.example.com. 300 IN A 10.0.0.1"]}`
	req := httptest.NewRequest(http.MethodDelete, "/records/delete", bytes.NewBufferString(delBody))
	w := httptest.NewRecorder()
	s.handleDelete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	assertJSONContentType(t, w)
	resp := decodeResponse(t, w)
	if !resp.OK {
		t.Errorf("expected ok:true, got error: %s", resp.Error)
	}
}

func TestHandleDelete_WrongMethod(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/records/delete", nil)
	w := httptest.NewRecorder()
	s.handleDelete(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", w.Code)
	}
	assertJSONContentType(t, w)
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

func TestHandleDelete_EmptyRecords(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/records/delete", bytes.NewBufferString(`{"records":[]}`))
	w := httptest.NewRecorder()
	s.handleDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}

// --- /health ---

func TestHandleHealth_Success(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	assertJSONContentType(t, w)

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "healthy" {
		t.Errorf("status field: got %v, want \"healthy\"", body["status"])
	}
}

func TestHandleHealth_WrongMethod(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", w.Code)
	}
	assertJSONContentType(t, w)
	resp := decodeResponse(t, w)
	if resp.OK {
		t.Error("expected ok:false")
	}
}
