package integration

import (
	"strings"
	"sync"
	"testing"
)

// The connector hooks tests require the emulator under test to be started
// with CONNECTOR_HOOKS=test/integration/testdata/hooks/hooks.yaml (CI does
// this). Against an emulator without hooks the probe call fails with
// "unknown function" and the tests skip.

var hooksProbe struct {
	once       sync.Once
	configured bool
}

func requireConnectorHooks(t *testing.T) {
	t.Helper()
	hooksProbe.once.Do(func() {
		yaml := `
main:
  steps:
    - boom:
        call: test.connector.fail
        result: r
`
		// The probe fails either way: with the hook's structured 409 when
		// hooks are configured, or with "unknown function" when they're not.
		er := deployAndRunExpectError(t, "connector-hook-probe", yaml, nil)
		msg, _ := parseErrorPayload(er)
		hooksProbe.configured = !strings.Contains(msg, "unknown function")
	})
	if !hooksProbe.configured {
		t.Skip("emulator not started with CONNECTOR_HOOKS; skipping hooks test")
	}
}

func TestConnectorHookExecReturnsResult(t *testing.T) {
	requireConnectorHooks(t)
	yaml := `
main:
  params: [args]
  steps:
    - publish:
        call: googleapis.pubsub.v1.projects.topics.publish
        args:
          topic: ${args.topic}
          body:
            messages:
              - data: aGVsbG8=
        result: r
    - done:
        return: ${r}
`
	er := deployAndRun(t, "connector-hook-publish", yaml, map[string]interface{}{
		"topic": "projects/my-project/topics/events",
	})
	assertResultContains(t, er, "messageIds", []interface{}{"hook-1"})
}

func TestConnectorHookStructuredErrorIsCatchable(t *testing.T) {
	requireConnectorHooks(t)
	yaml := `
main:
  steps:
    - guarded:
        try:
          steps:
            - boom:
                call: test.connector.fail
                result: r
        except:
          as: e
          steps:
            - handled:
                return:
                  code: ${e.code}
                  tag: ${e.tags[0]}
                  connector: ${e.connector}
`
	er := deployAndRun(t, "connector-hook-catch", yaml, nil)
	assertResultEquals(t, er, map[string]interface{}{
		"code":      409,
		"tag":       "HttpError",
		"connector": "test.connector.fail",
	})
}

func TestConnectorHookErrorPropagatesTags(t *testing.T) {
	requireConnectorHooks(t)
	yaml := `
main:
  steps:
    - boom:
        call: test.connector.fail
        result: r
`
	er := deployAndRunExpectError(t, "connector-hook-propagate", yaml, nil)
	assertErrorHasTag(t, er, "HttpError")
	assertErrorContains(t, er, "resource already exists")
}

func TestConnectorHookRetriesOnFailure(t *testing.T) {
	requireConnectorHooks(t)
	yaml := `
main:
  steps:
    - guarded:
        try:
          steps:
            - boom:
                call: test.connector.fail
                result: r
        retry:
          predicate: ${retry_predicate}
          max_retries: 2
          backoff:
            initial_delay: 0.1
            max_delay: 1
            multiplier: 2
        except:
          as: e
          steps:
            - handled:
                return: ${e.code}
    - fallthrough:
        return: "not reached"

retry_predicate:
  params: [e]
  steps:
    - check:
        switch:
          - condition: ${e.code == 409}
            return: true
    - no_retry:
        return: false
`
	er := deployAndRun(t, "connector-hook-retry", yaml, nil)
	assertResultEquals(t, er, 409)
}
