package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCallbacks_CreateAndAwait verifies creating a callback endpoint and
// receiving a callback.
func TestCallbacks_CreateAndAwait(t *testing.T) {
	wfID := uniqueID("callback-basic")
	yaml := `
main:
  steps:
    - create_cb:
        call: events.create_callback_endpoint
        args:
          http_callback_method: "POST"
        result: callback
    - wait:
        call: events.await_callback
        args:
          callback: ${callback}
          timeout: 10
        result: callback_data
    - done:
        return: ${callback_data}
`
	name := createWorkflow(t, wfID, yaml)

	// Start execution in background (it will wait for callback)
	body, _ := json.Marshal(map[string]interface{}{})
	url := apiURL(name + "/executions")
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	var exec map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&exec)
	resp.Body.Close()
	execName, _ := exec["name"].(string)

	// Wait a moment for the execution to create the callback
	time.Sleep(2 * time.Second)

	// List callbacks to find the URL
	listResp, err := http.Get(apiURL(execName + "/callbacks"))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	var callbacks map[string]interface{}
	json.NewDecoder(listResp.Body).Decode(&callbacks)
	listResp.Body.Close()

	cbList, ok := callbacks["callbacks"].([]interface{})
	if !ok || len(cbList) == 0 {
		t.Skipf("no callbacks found - callback feature may not be implemented yet")
		return
	}

	// Get the callback URL
	cb, _ := cbList[0].(map[string]interface{})
	cbURL, _ := cb["url"].(string)
	if cbURL == "" {
		t.Skip("callback URL not found")
		return
	}

	// Send callback data
	cbBody, _ := json.Marshal(map[string]interface{}{
		"status": "approved",
	})
	cbResp, err := http.Post(cbURL, "application/json", bytes.NewReader(cbBody))
	if err != nil {
		t.Fatalf("callback HTTP error: %v", err)
	}
	cbResp.Body.Close()

	// Wait for execution to complete
	er := waitForExecution(t, execName, 15*time.Second)
	assertSucceeded(t, er)
}

// TestCallbacks_Timeout verifies that callback timeout raises an error.
func TestCallbacks_Timeout(t *testing.T) {
	yaml := `
main:
  steps:
    - create_cb:
        call: events.create_callback_endpoint
        args:
          http_callback_method: "POST"
        result: callback
    - try_wait:
        try:
          steps:
            - wait:
                call: events.await_callback
                args:
                  callback: ${callback}
                  timeout: 2
                result: callback_data
        except:
          as: e
          steps:
            - handle:
                return:
                  timed_out: true
                  message: ${e.message}
`
	er := deployAndRun(t, uniqueID("cb-timeout"), yaml, nil)
	assertSucceeded(t, er)
	assertResultContains(t, er, "timed_out", true)
}

// TestCallbacks_CreateReturnsURL verifies that events.create_callback_endpoint
// returns the endpoint URL, matching real GCW behavior.
func TestCallbacks_CreateReturnsURL(t *testing.T) {
	yaml := `
main:
  steps:
    - create_cb:
        call: events.create_callback_endpoint
        args:
          http_callback_method: "POST"
        result: callback
    - done:
        return:
          url: ${callback.url}
          method: ${callback.method}
`
	er := deployAndRun(t, uniqueID("cb-url"), yaml, nil)
	assertSucceeded(t, er)

	result, ok := er.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result to be a map, got %T: %v", er.Result, er.Result)
	}
	url, _ := result["url"].(string)
	if !strings.Contains(url, "/callbacks/") {
		t.Errorf("expected callback URL to contain /callbacks/, got %q", url)
	}
	if !strings.HasPrefix(url, "http") {
		t.Errorf("expected absolute callback URL, got %q", url)
	}
	if result["method"] != "POST" {
		t.Errorf("expected method POST, got %v", result["method"])
	}
}

// TestCallbacks_DeliveredPayloadShape verifies the callback data delivered to
// events.await_callback has the http_request shape of real GCW.
func TestCallbacks_DeliveredPayloadShape(t *testing.T) {
	wfID := uniqueID("cb-payload")
	yaml := `
main:
  steps:
    - create_cb:
        call: events.create_callback_endpoint
        args:
          http_callback_method: "POST"
        result: callback
    - wait:
        call: events.await_callback
        args:
          callback: ${callback}
          timeout: 10
        result: callback_data
    - done:
        return:
          approved: ${callback_data.http_request.body.status}
          method: ${callback_data.http_request.method}
`
	name := createWorkflow(t, wfID, yaml)

	body, _ := json.Marshal(map[string]interface{}{})
	resp, err := http.Post(apiURL(name+"/executions"), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	var exec map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&exec)
	resp.Body.Close()
	execName, _ := exec["name"].(string)

	time.Sleep(2 * time.Second)

	listResp, err := http.Get(apiURL(execName + "/callbacks"))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	var callbacks map[string]interface{}
	json.NewDecoder(listResp.Body).Decode(&callbacks)
	listResp.Body.Close()

	cbList, ok := callbacks["callbacks"].([]interface{})
	if !ok || len(cbList) == 0 {
		t.Fatalf("expected a registered callback, got %v", callbacks)
	}
	cb, _ := cbList[0].(map[string]interface{})
	cbURL, _ := cb["url"].(string)
	if cbURL == "" {
		t.Fatalf("expected callback URL in list response, got %v", cb)
	}

	cbBody, _ := json.Marshal(map[string]interface{}{"status": "approved"})
	cbResp, err := http.Post(cbURL, "application/json", bytes.NewReader(cbBody))
	if err != nil {
		t.Fatalf("callback HTTP error: %v", err)
	}
	if cbResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from callback delivery, got %d", cbResp.StatusCode)
	}
	cbResp.Body.Close()

	er := waitForExecution(t, execName, 15*time.Second)
	assertSucceeded(t, er)
	assertResultContains(t, er, "approved", "approved")
	assertResultContains(t, er, "method", "POST")
}

// TestCallbacks_SendUnknownID verifies that delivering to an unknown callback
// id returns 404 instead of silently succeeding.
func TestCallbacks_SendUnknownID(t *testing.T) {
	resp, err := http.Post(strings.TrimRight(testServer, "/")+"/callbacks/does-not-exist", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown callback id, got %d", resp.StatusCode)
	}
}
