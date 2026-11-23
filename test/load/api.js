// Load on the platform API (ADR-0021: the DB pool and the outbox under
// load), run by k6 in docker from web/e2e/load.spec.ts, which sets up the
// organization, its Workspaces and the logged-in members, and passes their
// sessions in /data/fixture.json (never printed, never kept).
//
// Each iteration of a virtual user is one of:
//   70 %  a read: /me, the organization's Workspaces, one Workspace, its
//         usage, an operation;
//   25 %  a write through the outbox: a group's role on a Workspace toggled
//         Developer <-> Viewer (workspace.update), waited until it ends;
//    5 %  a Namespace created then deleted (namespace.create, .delete).
//
// MODE=full: 10 -> 25 -> 50 -> 100 VUs, 1 min ramp + 3 min hold each.
// MODE=dry: 1 VU for 30 s, to check the whole chain before a real run.
//
// k6 stops the run itself (exit 99) when the p95 of all requests passes 5 s
// or more than 5 % of the requests are errors. An error is a network
// failure, a 5xx (503 SERVICE_BUSY included) or a 4xx the API should not
// answer here. Not errors, but counted: 429 (the write limit, 120 a minute
// per person) and 409 on a Workspace busy with another operation, or a
// Namespace quota already taken by another VU. The runner stops it from
// outside when Postgres connections pass 80 %.
import http from "k6/http";
import { sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

const fixture = JSON.parse(open("/data/fixture.json"));
const BASE = `${fixture.portal}/api/v1`;
const MODE = __ENV.MODE || "dry";

const errors = new Rate("errors");
const busy = new Counter("busy_409");
const limited = new Counter("limited_429");
const serviceBusy = new Counter("service_busy_503");
const opWait = new Trend("operation_wait", true);

const thresholds = {
  http_req_duration: [
    { threshold: "p(95)<5000", abortOnFail: true, delayAbortEval: "30s" },
  ],
  errors: [
    { threshold: "rate<0.05", abortOnFail: true, delayAbortEval: "30s" },
  ],
};

export const options =
  MODE === "full"
    ? {
        scenarios: {
          ramp: {
            executor: "ramping-vus",
            startVUs: 0,
            stages: [10, 25, 50, 100].flatMap((n) => [
              { duration: "1m", target: n },
              { duration: "3m", target: n },
            ]),
            gracefulRampDown: "30s",
            gracefulStop: "2m",
          },
        },
        thresholds,
        summaryTrendStats: [
          "avg",
          "min",
          "med",
          "p(90)",
          "p(95)",
          "p(99)",
          "max",
        ],
      }
    : {
        vus: 1,
        duration: "30s",
        thresholds,
        summaryTrendStats: ["avg", "med", "p(95)", "max"],
      };

// Each VU is one of the members, round robin, so the write limit is shared out.
function who() {
  const u = fixture.users[(__VU - 1) % fixture.users.length];
  return {
    Cookie: u.cookie,
    "X-CSRF-Token": u.csrf,
    "Content-Type": "application/json",
  };
}

function call(method, path, name, body, headers) {
  const res = http.request(
    method,
    BASE + path,
    body === undefined ? null : JSON.stringify(body),
    {
      headers: { ...who(), ...headers },
      tags: { name },
      timeout: "30s",
      redirects: 0,
    },
  );
  const ok = res.status >= 200 && res.status < 300;
  const code = ok ? undefined : safeCode(res);
  const isBusy =
    res.status === 409 &&
    (code === "WORKSPACE_NOT_READY" || code === "CONFLICT");
  if (res.status === 429) limited.add(1);
  if (isBusy) busy.add(1);
  if (res.status === 503 && code === "SERVICE_BUSY") serviceBusy.add(1);
  errors.add(!ok && res.status !== 429 && !isBusy, { name });
  return { status: res.status, code, body: ok ? safeJSON(res) : null };
}

function safeJSON(res) {
  try {
    return res.json();
  } catch (e) {
    return null;
  }
}

function safeCode(res) {
  const b = safeJSON(res);
  return b && b.code;
}

// The operation behind a write, polled until it leaves Pending and Running.
function waitFor(op) {
  const start = Date.now();
  let state = op.state;
  while (
    (state === "Pending" || state === "Running") &&
    Date.now() - start < 120000
  ) {
    sleep(1);
    const r = call("GET", `/operations/${op.id}`, "GET /operations/{id}");
    if (r.body) state = r.body.state;
  }
  opWait.add(Date.now() - start, { type: op.type, state });
  return state;
}

const pick = (xs) => xs[Math.floor(Math.random() * xs.length)];

function read() {
  const ws = pick(fixture.workspaces);
  switch (Math.floor(Math.random() * 5)) {
    case 0:
      return call("GET", "/me", "GET /me");
    case 1:
      return call(
        "GET",
        `/organizations/${fixture.org}/workspaces`,
        "GET /organizations/{org}/workspaces",
      );
    case 2:
      return call("GET", `/workspaces/${ws.id}`, "GET /workspaces/{id}");
    case 3:
      return call(
        "GET",
        `/workspaces/${ws.id}/usage`,
        "GET /workspaces/{id}/usage",
      );
    default:
      return call(
        "GET",
        `/operations/${ws.operationId}`,
        "GET /operations/{id}",
      );
  }
}

function toggle() {
  const ws = pick(fixture.workspaces);
  const role = Math.random() < 0.5 ? "Developer" : "Viewer";
  const r = call(
    "PUT",
    `/workspaces/${ws.id}/bindings/${fixture.toggleGroupId}`,
    "PUT /workspaces/{id}/bindings/{group}",
    {
      role,
    },
  );
  if (r.status === 202 && r.body.operation) waitFor(r.body.operation);
  else sleep(1);
}

function namespacePair() {
  const ws = pick(fixture.workspaces);
  const name = `l${__VU}x${__ITER}`.slice(0, 20);
  const made = call(
    "POST",
    `/workspaces/${ws.id}/namespaces`,
    "POST /workspaces/{id}/namespaces",
    { name },
    {
      "Idempotency-Key": crypto.randomUUID(),
    },
  );
  if (made.status !== 202) return sleep(1);
  waitFor(made.body.operation);
  const full = made.body.namespace.name;
  // The delete is asked again while the Workspace is busy with another operation.
  for (let i = 0; i < 60; i++) {
    const del = call(
      "DELETE",
      `/workspaces/${ws.id}/namespaces/${full}`,
      "DELETE /workspaces/{id}/namespaces/{name}",
    );
    if (del.status === 202) return waitFor(del.body.operation);
    if (del.status !== 409 && del.status !== 429) return;
    sleep(2);
  }
}

export default function () {
  const r = Math.random();
  if (r < 0.7) {
    read();
    sleep(0.5 + Math.random());
  } else if (r < 0.95) toggle();
  else namespacePair();
}
