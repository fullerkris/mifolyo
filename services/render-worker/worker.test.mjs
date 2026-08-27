import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import test from "node:test";

import {
  buildContentSecurityPolicy,
  createFrameReader,
  createSocketBroker,
  encodeFrame,
  renderResult,
  validateNoDuplicateMembers,
  validateBrowserResourceRequest,
  validateRequestPayload,
  validateResourceReplyPayload,
} from "./worker.mjs";

const jobID = "0123456789abcdef0123456789abcdef";

function validRequest() {
  return {
    version: 2,
    kind: "render_start",
    job_id: jobID,
    mode: "inline_only",
    effective_url: "https://render.example.org/app",
    html: "<!doctype html><html><body>fixture</body></html>",
    resource_hosts: {
      script: [],
      stylesheet: [],
    },
    limits: {
      max_render_time_ms: 5000,
      settle_time_ms: 10,
      max_resource_requests: 0,
      max_aggregate_resource_bytes: 0,
      max_resource_body_bytes: 0,
      max_rendered_dom_bytes: 1048576,
      max_dom_nodes: 1000,
      max_redirect_hops: 0,
      max_console_bytes: 1024,
    },
  };
}

function brokeredRequest() {
  const request = validRequest();
  request.mode = "brokered";
  request.resource_hosts = {
    script: ["a.cdn.example.org", "z.cdn.example.org"],
    stylesheet: ["a.cdn.example.org"],
  };
  request.limits.max_resource_requests = 4;
  request.limits.max_aggregate_resource_bytes = 1024;
  request.limits.max_resource_body_bytes = 512;
  return request;
}

function payload(value) {
  return Buffer.from(JSON.stringify(value), "utf8");
}

function resourceReply(overrides = {}) {
  return {
    version: 2,
    kind: "resource_reply",
    job_id: jobID,
    intent_id: 1,
    status: "ok",
    status_code: 200,
    content_type: "application/javascript; charset=utf-8",
    body_base64: "YWJjZGU=",
    body_bytes: 5,
    error_code: "",
    ...overrides,
  };
}

test("accepts the exact V2 inline render_start", () => {
  const request = validRequest();
  assert.deepEqual(validateRequestPayload(payload(request)), request);
});

test("requires exact V2 fields and rejects duplicate members", () => {
  assert.throws(
    () => validateNoDuplicateMembers('{"version":2,"version":2}'),
    /duplicate object member/,
  );

  for (const mutate of [
    (request) => { request.group_id = "spoofed"; },
    (request) => { delete request.kind; },
    (request) => { request.limits.extra = 1; },
    (request) => { request.resource_hosts.extra = []; },
  ]) {
    const request = validRequest();
    mutate(request);
    assert.throws(() => validateRequestPayload(payload(request)), /fields do not match/);
  }

  const invalidUTF8 = payload(validRequest());
  invalidUTF8[invalidUTF8.indexOf(Buffer.from("fixture"))] = 0xff;
  assert.throws(() => validateRequestPayload(invalidUTF8), /UTF-8/);
});

test("enforces inline mode invariants and all limit bounds", () => {
  for (const mutate of [
    (request) => { request.version = 1; },
    (request) => { request.kind = "render"; },
    (request) => { request.mode = "other"; },
    (request) => { request.effective_url = "http://render.example.org/app"; },
    (request) => { request.resource_hosts.script = ["cdn.example.org"]; },
    (request) => { request.limits.max_resource_requests = 1; },
    (request) => { request.limits.max_aggregate_resource_bytes = 1; },
    (request) => { request.limits.max_resource_body_bytes = 1; },
    (request) => { request.limits.max_redirect_hops = 1; },
    (request) => { request.limits.max_render_time_ms = 30001; },
    (request) => { request.limits.settle_time_ms = -1; },
    (request) => { request.limits.max_rendered_dom_bytes = 0; },
    (request) => { request.limits.max_dom_nodes = 100001; },
    (request) => { request.limits.max_console_bytes = 65537; },
  ]) {
    const request = validRequest();
    mutate(request);
    assert.throws(() => validateRequestPayload(payload(request)));
  }
});

test("accepts brokered mode and strictly validates hosts and resource limits", () => {
  const request = brokeredRequest();
  assert.deepEqual(validateRequestPayload(payload(request)), request);

  for (const mutate of [
    (value) => { value.resource_hosts = { script: [], stylesheet: [] }; },
    (value) => { value.resource_hosts.script = ["z.cdn.example.org", "a.cdn.example.org"]; },
    (value) => { value.resource_hosts.script = ["a.cdn.example.org", "a.cdn.example.org"]; },
    (value) => { value.resource_hosts.script = ["CDN.example.org"]; },
    (value) => { value.resource_hosts.script = ["127.0.0.1"]; },
    (value) => { value.resource_hosts.script = ["cdn.localhost"]; },
    (value) => { value.resource_hosts.script = ["singlelabel"]; },
    (value) => { value.limits.max_resource_requests = 0; },
    (value) => { value.limits.max_aggregate_resource_bytes = 0; },
    (value) => { value.limits.max_resource_body_bytes = 0; },
    (value) => { value.limits.max_redirect_hops = 1; },
    (value) => { value.limits.max_resource_requests = 65; },
    (value) => { value.limits.max_aggregate_resource_bytes = 32 * 1024 * 1024 + 1; },
    (value) => { value.limits.max_resource_body_bytes = 5 * 1024 * 1024 + 1; },
  ]) {
    const invalid = brokeredRequest();
    mutate(invalid);
    assert.throws(() => validateRequestPayload(payload(invalid)));
  }
});

test("builds host-exact brokered CSP while retaining denials", () => {
  const csp = buildContentSecurityPolicy(brokeredRequest());
  assert.match(csp, /default-src 'none'/);
  assert.match(csp, /connect-src 'none'/);
  assert.match(csp, /img-src 'none'/);
  assert.match(csp, /script-src 'unsafe-inline' https:\/\/a\.cdn\.example\.org https:\/\/z\.cdn\.example\.org/);
  assert.match(csp, /style-src 'unsafe-inline' https:\/\/a\.cdn\.example\.org/);
  assert.match(csp, /script-src-attr 'none'/);
  assert.doesNotMatch(csp, /script-src[^;]* https:(?: |;)/);
});

test("admits only canonical direct HTTPS browser resource requests", () => {
  const approvedHosts = {
    script: new Set(["scripts.cdn.example.org"]),
    stylesheet: new Set(["styles.cdn.example.org"]),
  };
  assert.equal(
    validateBrowserResourceRequest(
      "https://scripts.cdn.example.org/assets/app.js",
      "GET",
      "script",
      approvedHosts,
    ),
    "https://scripts.cdn.example.org/assets/app.js",
  );

  for (const [rawURL, method, resourceType] of [
    ["http://scripts.cdn.example.org/assets/app.js", "GET", "script"],
    ["https://user@scripts.cdn.example.org/assets/app.js", "GET", "script"],
    ["https://scripts.cdn.example.org:444/assets/app.js", "GET", "script"],
    ["https://scripts.cdn.example.org/assets/app.js?v=1", "GET", "script"],
    ["https://scripts.cdn.example.org/assets/%252fsecret.js", "GET", "script"],
    ["HTTPS://scripts.cdn.example.org/assets/app.js", "GET", "script"],
    ["https://unapproved.example.org/assets/app.js", "GET", "script"],
    ["https://scripts.cdn.example.org/assets/app.js", "POST", "script"],
    ["https://scripts.cdn.example.org/assets/app.js", "GET", "image"],
    [`https://scripts.cdn.example.org/${"a".repeat(2049)}.js`, "GET", "script"],
  ]) {
    assert.throws(
      () => validateBrowserResourceRequest(rawURL, method, resourceType, approvedHosts),
      /browser resource/,
    );
  }
});

test("parses a strict successful resource_reply", () => {
  const reply = validateResourceReplyPayload(
    payload(resourceReply()),
    jobID,
    1,
    brokeredRequest().limits,
    10,
  );
  assert.equal(reply.status, "ok");
  assert.deepEqual(reply.body, Buffer.from("abcde"));
  assert.equal(reply.bodyBytes, 5);
  assert.equal(reply.contentType, "application/javascript;charset=utf-8");
});

test("accepts only the neutral resource_denied reply", () => {
  const denied = resourceReply({
    status: "error",
    status_code: 0,
    content_type: "",
    body_base64: "",
    body_bytes: 0,
    error_code: "resource_denied",
  });
  assert.equal(
    validateResourceReplyPayload(payload(denied), jobID, 1, brokeredRequest().limits).status,
    "error",
  );

  for (const mutate of [
    (reply) => { reply.status_code = 403; },
    (reply) => { reply.content_type = "text/plain"; },
    (reply) => { reply.body_base64 = "YQ=="; reply.body_bytes = 1; },
    (reply) => { reply.error_code = "robots_denied"; },
  ]) {
    const invalid = structuredClone(denied);
    mutate(invalid);
    assert.throws(() => validateResourceReplyPayload(payload(invalid), jobID, 1, brokeredRequest().limits));
  }
});

test("rejects unknown, duplicate, invalid UTF-8, and stale resource replies", () => {
  const unknown = resourceReply({ extra: true });
  assert.throws(
    () => validateResourceReplyPayload(payload(unknown), jobID, 1, brokeredRequest().limits),
    /fields do not match/,
  );

  const duplicate = JSON.stringify(resourceReply()).replace(
    '"kind":"resource_reply"',
    '"kind":"resource_reply","kind":"resource_reply"',
  );
  assert.throws(
    () => validateResourceReplyPayload(Buffer.from(duplicate), jobID, 1, brokeredRequest().limits),
    /duplicate object member/,
  );

  const invalidUTF8 = payload(resourceReply());
  invalidUTF8[invalidUTF8.indexOf(Buffer.from("javascript"))] = 0xff;
  assert.throws(
    () => validateResourceReplyPayload(invalidUTF8, jobID, 1, brokeredRequest().limits),
    /UTF-8/,
  );

  for (const [expectedJobID, expectedIntentID, reply] of [
    ["00000000000000000000000000000000", 1, resourceReply()],
    [jobID, 2, resourceReply()],
    [jobID, 1, resourceReply({ job_id: "00000000000000000000000000000000" })],
    [jobID, 1, resourceReply({ intent_id: 2 })],
  ]) {
    assert.throws(
      () => validateResourceReplyPayload(payload(reply), expectedJobID, expectedIntentID, brokeredRequest().limits),
      /outstanding intent/,
    );
  }
});

test("requires canonical padded base64 and exact body byte counts", () => {
  for (const reply of [
    resourceReply({ body_base64: "YWJjZGU", body_bytes: 5 }),
    resourceReply({ body_base64: "YWJjZGU===", body_bytes: 5 }),
    resourceReply({ body_base64: "YWJjZGU_", body_bytes: 6 }),
    resourceReply({ body_bytes: 4 }),
    resourceReply({ body_base64: "/w==", body_bytes: 1 }),
    resourceReply({ body_base64: "", body_bytes: 0 }),
  ]) {
    assert.throws(() => validateResourceReplyPayload(payload(reply), jobID, 1, brokeredRequest().limits));
  }
});

test("enforces content type, per-body, and aggregate reply bounds", () => {
  for (const [reply, limits, aggregateBytes] of [
    [resourceReply({ content_type: "text/plain\r\nx: y" }), brokeredRequest().limits, 0],
    [resourceReply({ content_type: "text/plain; charset=utf-8; charset=ascii" }), brokeredRequest().limits, 0],
    [resourceReply({ content_type: "x".repeat(1025) }), brokeredRequest().limits, 0],
    [resourceReply(), { ...brokeredRequest().limits, max_resource_body_bytes: 4 }, 0],
    [resourceReply(), { ...brokeredRequest().limits, max_aggregate_resource_bytes: 12 }, 8],
  ]) {
    assert.throws(() => validateResourceReplyPayload(payload(reply), jobID, 1, limits, aggregateBytes));
  }
});

test("encodes exact terminal fields and a four-byte big-endian frame", () => {
  const terminal = renderResult(jobID, "ok", {
    html: "<html></html>",
    domNodes: 1,
    consoleBytes: 2,
    resourceRequests: 3,
    resourceBytes: 4,
  });
  assert.deepEqual(Object.keys(terminal).sort(), [
    "console_bytes",
    "dom_nodes",
    "error_code",
    "html",
    "job_id",
    "kind",
    "resource_bytes",
    "resource_requests",
    "status",
    "version",
  ]);
  const frame = encodeFrame(terminal);
  assert.equal(frame.readUInt32BE(0), frame.length - 4);
  assert.deepEqual(JSON.parse(frame.subarray(4).toString("utf8")), terminal);
});

test("frame reader accepts one fragmented frame and rejects unsolicited or repeated bytes", async () => {
  const socket = new PassThrough();
  const reader = createFrameReader(socket);
  const first = encodeFrame({ sequence: 1 });
  const firstRead = reader.read("first frame");
  socket.write(first.subarray(0, 3));
  socket.write(first.subarray(3));
  assert.deepEqual(JSON.parse((await firstRead).toString("utf8")), { sequence: 1 });
  reader.assertIdle();

  socket.write(encodeFrame({ sequence: 2 }).subarray(0, 3));
  assert.throws(() => reader.assertIdle(), /unsolicited bytes/);
  assert.equal(socket.destroyed, true);
});

test("frame reader rejects a second frame in the same receive phase", async () => {
  const socket = new PassThrough();
  const reader = createFrameReader(socket);
  const read = reader.read("single reply");
  socket.write(Buffer.concat([encodeFrame({ sequence: 1 }), encodeFrame({ sequence: 2 })]));
  await assert.rejects(read, /multiple or early frames/);
  assert.equal(socket.destroyed, true);
});

class FakeSocket extends EventEmitter {
  constructor() {
    super();
    this.destroyed = false;
    this.writes = [];
  }

  setTimeout() {}

  write(data, callback) {
    this.writes.push(Buffer.from(data));
    queueMicrotask(() => callback?.());
    return true;
  }

  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    this.emit("close");
  }
}

test("socket broker emits one intent and consumes one matching reply", async () => {
  const socket = new FakeSocket();
  const reader = createFrameReader(socket);
  const request = brokeredRequest();
  const broker = createSocketBroker(socket, reader, request);
  const exchange = broker({
    url: "https://a.cdn.example.org/app.js",
    method: "GET",
    resourceType: "script",
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(socket.writes.length, 1);
  const intent = JSON.parse(socket.writes[0].subarray(4).toString("utf8"));
  assert.equal(intent.kind, "resource_intent");
  assert.equal(intent.intent_id, 1);
  socket.emit("data", encodeFrame(resourceReply()));
  const resource = await exchange;
  assert.deepEqual(resource.body, Buffer.from("abcde"));
  assert.equal(resource.contentType, "application/javascript;charset=utf-8");
  assert.equal(broker.hasOutstandingIntent(), false);
  broker.assertHealthy();
});

test("socket broker cancellation destroys a connection with one outstanding intent", async () => {
  const socket = new FakeSocket();
  const reader = createFrameReader(socket);
  const request = brokeredRequest();
  const broker = createSocketBroker(socket, reader, request);
  const exchange = broker({
    url: "https://a.cdn.example.org/app.js",
    method: "GET",
    resourceType: "script",
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(socket.writes.length, 1);
  assert.equal(broker.hasOutstandingIntent(), true);
  const intent = JSON.parse(socket.writes[0].subarray(4).toString("utf8"));
  assert.equal(intent.kind, "resource_intent");
  assert.equal(intent.intent_id, 1);

  broker.cancel(new Error("deadline"));
  assert.equal(socket.destroyed, true);
  await assert.rejects(exchange, /render_failed/);
});
