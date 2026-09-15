# Limitations

The following features are **not** supported by the emulator.

## Google Cloud Connectors

The `googleapis.*` connectors (e.g., `googleapis.cloudresourcemanager.v3.projects.get`) require real GCP service backends and are not emulated. The emulator handles all `http.*` calls but not connector-specific semantics.

**Workaround**: Map each connector call name your workflows use to a local script or HTTP endpoint with [connector hooks](../guide/connector-hooks.md) — the workflow source runs unmodified, and handlers can return structured errors so try/retry/except behaves like the real service. Alternatively, branch inside the workflow on an environment variable and call `http.*` against a local mock (see the [FAQ](./faq.md)).

## IAM / Authentication

The emulator accepts all requests without any authentication or authorization checks. No credentials are needed.

## Eventarc / Pub/Sub Triggers

Workflows can only be triggered via the REST API (`POST .../executions`). Eventarc triggers, Pub/Sub subscriptions, and Cloud Scheduler triggers are not supported.

## Long-Running Operations

In the real GCW API, workflow Create/Update/Delete return long-running Operations that need to be polled. The emulator completes these operations immediately and returns the result directly.

## Execution Step History

The emulator provides execution results and errors, but does not maintain a step-by-step audit log (step entries). You can see the final state but not which steps ran in what order.

## CMEK / Billing / Quotas

Customer-managed encryption keys, billing simulation, and quota enforcement are not applicable to a local emulator.

## Multi-Region

The emulator runs as a single local instance. Multi-region deployment semantics are not simulated.

## Workflow Revision History

The emulator supports updating workflows, but does not maintain a revision history. `ListWorkflowRevisions` is not available.
