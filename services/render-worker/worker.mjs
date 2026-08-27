import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { MIMEType } from "node:util";

import { chromium } from "playwright-core";

export const PROTOCOL_VERSION = 2;
export const MAX_FRAME_BYTES = 32 * 1024 * 1024;
const MAX_SOURCE_BYTES = 5 * 1024 * 1024;
const MAX_CONTENT_TYPE_BYTES = 1024;
const MAX_RESOURCE_URL_BYTES = 2048;
const DEFAULT_SOCKET_PATH = "/run/mifolyo-render/renderer.sock";
const REQUEST_KEYS = ["effective_url", "html", "job_id", "kind", "limits", "mode", "resource_hosts", "version"];
const LIMIT_KEYS = [
  "max_aggregate_resource_bytes",
  "max_console_bytes",
  "max_dom_nodes",
  "max_redirect_hops",
  "max_render_time_ms",
  "max_rendered_dom_bytes",
  "max_resource_body_bytes",
  "max_resource_requests",
  "settle_time_ms",
];
const RESOURCE_HOST_KEYS = ["script", "stylesheet"];
const RESOURCE_REPLY_KEYS = [
  "body_base64",
  "body_bytes",
  "content_type",
  "error_code",
  "intent_id",
  "job_id",
  "kind",
  "status",
  "status_code",
  "version",
];
const RESOURCE_HOST_PATTERN = /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const RESERVED_HOST_SUFFIXES = [
  "localhost",
  "local",
  "localdomain",
  "lan",
  "home",
  "home.arpa",
  "internal",
  "intranet",
  "onion",
  "alt",
  "arpa",
  "test",
  "invalid",
  "example",
];
const MIME_TOKEN_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

export class RenderError extends Error {
  constructor(code, result = {}) {
    super(code);
    this.code = code;
    this.result = result;
  }
}

function fail(code) {
  throw new RenderError(code);
}

function assertExactKeys(value, expected, field) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${field} must be an object`);
  }
  const actual = Object.keys(value).sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new Error(`${field} fields do not match the V2 contract`);
  }
}

function requireInteger(value, minimum, maximum, field) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${field} is outside its V2 bounds`);
  }
}

function validateResourceHosts(hosts, field) {
  if (!Array.isArray(hosts)) {
    throw new Error(`${field} must be an array`);
  }
  let previous = "";
  for (const host of hosts) {
    if (
      typeof host !== "string" ||
      host.length > 253 ||
      !RESOURCE_HOST_PATTERN.test(host) ||
      net.isIP(host) !== 0 ||
      RESERVED_HOST_SUFFIXES.some((suffix) => host === suffix || host.endsWith(`.${suffix}`))
    ) {
      throw new Error(`${field} contains an invalid public DNS hostname`);
    }
    if (previous !== "" && previous >= host) {
      throw new Error(`${field} must be sorted and unique`);
    }
    previous = host;
  }
}

function parseAndNormalizeContentType(contentType) {
  if (
    typeof contentType !== "string" ||
    contentType === "" ||
    Buffer.byteLength(contentType, "utf8") > MAX_CONTENT_TYPE_BYTES ||
    contentType.trim() !== contentType ||
    /\p{Cc}/u.test(contentType)
  ) {
    throw new Error("resource content type is invalid");
  }

  let position = 0;
  const skipSpaces = () => {
    while (contentType[position] === " ") {
      position += 1;
    }
  };
  const readToken = () => {
    const start = position;
    while (position < contentType.length && MIME_TOKEN_PATTERN.test(contentType[position])) {
      position += 1;
    }
    return contentType.slice(start, position);
  };

  const type = readToken();
  if (type === "" || contentType[position] !== "/") {
    throw new Error("resource content type is invalid");
  }
  position += 1;
  const subtype = readToken();
  if (subtype === "") {
    throw new Error("resource content type is invalid");
  }

  let normalized;
  try {
    normalized = new MIMEType(`${type}/${subtype}`);
  } catch {
    throw new Error("resource content type is invalid");
  }
  const parameters = new Set();
  skipSpaces();
  while (position < contentType.length) {
    if (contentType[position] !== ";") {
      throw new Error("resource content type is invalid");
    }
    position += 1;
    skipSpaces();
    if (position === contentType.length) {
      break;
    }
    const name = readToken().toLowerCase();
    if (name === "" || parameters.has(name)) {
      throw new Error("resource content type is invalid");
    }
    parameters.add(name);
    skipSpaces();
    if (contentType[position] !== "=") {
      throw new Error("resource content type is invalid");
    }
    position += 1;
    skipSpaces();

    let value = "";
    if (contentType[position] === '"') {
      position += 1;
      let closed = false;
      while (position < contentType.length) {
        const character = contentType[position];
        position += 1;
        if (character === '"') {
          closed = true;
          break;
        }
        if (character === "\\") {
          if (position === contentType.length) {
            throw new Error("resource content type is invalid");
          }
          value += contentType[position];
          position += 1;
        } else {
          value += character;
        }
      }
      if (!closed) {
        throw new Error("resource content type is invalid");
      }
    } else {
      value = readToken();
      if (value === "") {
        throw new Error("resource content type is invalid");
      }
    }
    skipSpaces();
    normalized.params.set(name, value);
  }
  return normalized.toString();
}

function validUTF8(buffer) {
  return Buffer.from(buffer.toString("utf8"), "utf8").equals(buffer);
}

export function validateBrowserResourceRequest(rawURL, method, resourceType, approvedHosts) {
  if (
    typeof rawURL !== "string" ||
    rawURL === "" ||
    Buffer.byteLength(rawURL, "utf8") > MAX_RESOURCE_URL_BYTES ||
    Buffer.from(rawURL, "utf8").toString("utf8") !== rawURL ||
    method !== "GET" ||
    (resourceType !== "script" && resourceType !== "stylesheet") ||
    !(approvedHosts?.[resourceType] instanceof Set)
  ) {
    throw new Error("browser resource request is outside the V2 envelope");
  }

  let resourceURL;
  try {
    resourceURL = new URL(rawURL);
  } catch {
    throw new Error("browser resource URL is invalid");
  }
  if (
    resourceURL.href !== rawURL ||
    resourceURL.protocol !== "https:" ||
    resourceURL.hostname === "" ||
    resourceURL.username !== "" ||
    resourceURL.password !== "" ||
    resourceURL.port !== "" ||
    resourceURL.search !== "" ||
    resourceURL.hash !== "" ||
    !approvedHosts[resourceType].has(resourceURL.hostname)
  ) {
    throw new Error("browser resource URL is not canonical authorized HTTPS");
  }

  let decodedPath;
  try {
    decodedPath = decodeURIComponent(resourceURL.pathname);
  } catch {
    throw new Error("browser resource URL path is invalid");
  }
  if (
    decodedPath.includes("\\") ||
    decodedPath.includes("//") ||
    decodedPath.split("/").some((segment) => segment === "." || segment === "..") ||
    /%25/i.test(resourceURL.pathname)
  ) {
    throw new Error("browser resource URL path is ambiguous");
  }
  return resourceURL.href;
}

export function validateRequestPayload(payload) {
  if (!Buffer.isBuffer(payload) || payload.length === 0 || payload.length > MAX_FRAME_BYTES) {
    throw new Error("request frame size is invalid");
  }
  const text = payload.toString("utf8");
  if (!Buffer.from(text, "utf8").equals(payload)) {
    throw new Error("request is not valid UTF-8");
  }
  validateNoDuplicateMembers(text);
  const request = JSON.parse(text);
  assertExactKeys(request, REQUEST_KEYS, "request");
  assertExactKeys(request.limits, LIMIT_KEYS, "request.limits");
  assertExactKeys(request.resource_hosts, RESOURCE_HOST_KEYS, "request.resource_hosts");

  if (request.version !== PROTOCOL_VERSION) {
    throw new Error("request protocol version is unsupported");
  }
  if (request.kind !== "render_start") {
    throw new Error("request kind is invalid");
  }
  if (typeof request.job_id !== "string" || !/^[a-f0-9]{32}$/.test(request.job_id)) {
    throw new Error("request job ID is invalid");
  }
  if (request.mode !== "inline_only" && request.mode !== "brokered") {
    throw new Error("request mode is unsupported");
  }
  if (
    typeof request.effective_url !== "string" ||
    request.effective_url === "" ||
    Buffer.byteLength(request.effective_url, "utf8") > 2048 ||
    request.effective_url.includes("\0") ||
    request.effective_url.trim() !== request.effective_url
  ) {
    throw new Error("effective URL is invalid");
  }
  let effectiveURL;
  try {
    effectiveURL = new URL(request.effective_url);
  } catch {
    throw new Error("effective URL is invalid");
  }
  if (
    effectiveURL.protocol !== "https:" ||
    effectiveURL.hostname === "" ||
    effectiveURL.username !== "" ||
    effectiveURL.password !== "" ||
    (effectiveURL.port !== "" && effectiveURL.port !== "443") ||
    effectiveURL.hash !== ""
  ) {
    throw new Error("effective URL is outside the renderer transport envelope");
  }
  if (typeof request.html !== "string" || request.html === "" || Buffer.byteLength(request.html) > MAX_SOURCE_BYTES) {
    throw new Error("source HTML is invalid");
  }

  requireInteger(request.limits.max_render_time_ms, 1, 30000, "max_render_time_ms");
  requireInteger(request.limits.settle_time_ms, 0, 5000, "settle_time_ms");
  requireInteger(request.limits.max_resource_requests, 0, 64, "max_resource_requests");
  requireInteger(
    request.limits.max_aggregate_resource_bytes,
    0,
    32 * 1024 * 1024,
    "max_aggregate_resource_bytes",
  );
  requireInteger(request.limits.max_resource_body_bytes, 0, 5 * 1024 * 1024, "max_resource_body_bytes");
  requireInteger(request.limits.max_rendered_dom_bytes, 1, 5 * 1024 * 1024, "max_rendered_dom_bytes");
  requireInteger(request.limits.max_dom_nodes, 1, 100000, "max_dom_nodes");
  requireInteger(request.limits.max_redirect_hops, 0, 3, "max_redirect_hops");
  requireInteger(request.limits.max_console_bytes, 0, 64 * 1024, "max_console_bytes");

  validateResourceHosts(request.resource_hosts.script, "request.resource_hosts.script");
  validateResourceHosts(request.resource_hosts.stylesheet, "request.resource_hosts.stylesheet");
  const hasResourceHost = request.resource_hosts.script.length + request.resource_hosts.stylesheet.length > 0;
  if (request.mode === "inline_only") {
    if (
      hasResourceHost ||
      request.limits.max_resource_requests !== 0 ||
      request.limits.max_aggregate_resource_bytes !== 0 ||
      request.limits.max_resource_body_bytes !== 0 ||
      request.limits.max_redirect_hops !== 0
    ) {
      throw new Error("inline request resource hosts and limits must be empty");
    }
  } else if (
    !hasResourceHost ||
    request.limits.max_resource_requests <= 0 ||
    request.limits.max_aggregate_resource_bytes <= 0 ||
    request.limits.max_resource_body_bytes <= 0 ||
    request.limits.max_redirect_hops !== 0
  ) {
    throw new Error("brokered request resource hosts and limits are invalid");
  }
  return request;
}

export function buildContentSecurityPolicy(request) {
  const scriptHosts = request.resource_hosts.script.map((host) => `https://${host}`);
  const stylesheetHosts = request.resource_hosts.stylesheet.map((host) => `https://${host}`);
  return [
    "base-uri 'none'",
    "child-src 'none'",
    "connect-src 'none'",
    "default-src 'none'",
    "font-src 'none'",
    "form-action 'none'",
    "frame-ancestors 'none'",
    "frame-src 'none'",
    "img-src 'none'",
    "manifest-src 'none'",
    "media-src 'none'",
    "object-src 'none'",
    ["script-src", "'unsafe-inline'", ...scriptHosts].join(" "),
    "script-src-attr 'none'",
    ["style-src", "'unsafe-inline'", ...stylesheetHosts].join(" "),
    "worker-src 'none'",
  ].join("; ");
}

export function validateResourceReplyPayload(payload, jobID, intentID, limits, aggregateBytes = 0) {
  if (!Buffer.isBuffer(payload) || payload.length === 0 || payload.length > MAX_FRAME_BYTES) {
    throw new Error("resource reply frame size is invalid");
  }
  if (!validUTF8(payload)) {
    throw new Error("resource reply is not valid UTF-8");
  }
  const text = payload.toString("utf8");
  validateNoDuplicateMembers(text);
  const reply = JSON.parse(text);
  assertExactKeys(reply, RESOURCE_REPLY_KEYS, "resource reply");

  if (reply.version !== PROTOCOL_VERSION || reply.kind !== "resource_reply") {
    throw new Error("resource reply identity is invalid");
  }
  if (reply.job_id !== jobID || reply.intent_id !== intentID) {
    throw new Error("resource reply does not match the outstanding intent");
  }
  if (!Number.isSafeInteger(reply.intent_id) || reply.intent_id < 1) {
    throw new Error("resource reply intent ID is invalid");
  }

  if (reply.status === "error") {
    if (
      reply.status_code !== 0 ||
      reply.content_type !== "" ||
      reply.body_base64 !== "" ||
      reply.body_bytes !== 0 ||
      reply.error_code !== "resource_denied"
    ) {
      throw new Error("resource reply error is not neutral");
    }
    return { status: "error", body: Buffer.alloc(0), contentType: "", bodyBytes: 0 };
  }
  if (reply.status !== "ok" || reply.status_code !== 200 || reply.error_code !== "") {
    throw new Error("resource reply terminal state is invalid");
  }
  if (!Number.isSafeInteger(reply.body_bytes) || reply.body_bytes < 1) {
    throw new Error("resource reply byte count is invalid");
  }
  if (
    typeof reply.body_base64 !== "string" ||
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(reply.body_base64)
  ) {
    throw new Error("resource reply body is not canonical padded base64");
  }
  const body = Buffer.from(reply.body_base64, "base64");
  if (body.toString("base64") !== reply.body_base64 || body.length !== reply.body_bytes) {
    throw new Error("resource reply body byte count is invalid");
  }
  if (!validUTF8(body)) {
    throw new Error("resource reply body is not valid UTF-8");
  }
  if (
    !Number.isSafeInteger(aggregateBytes) ||
    aggregateBytes < 0 ||
    body.length > limits.max_resource_body_bytes ||
    aggregateBytes > limits.max_aggregate_resource_bytes ||
    body.length > limits.max_aggregate_resource_bytes - aggregateBytes
  ) {
    throw new Error("resource reply body exceeds its byte limit");
  }
  const contentType = parseAndNormalizeContentType(reply.content_type);
  return { status: "ok", body, contentType, bodyBytes: body.length };
}

export function validateNoDuplicateMembers(text) {
  let position = 0;

  function skipWhitespace() {
    while (position < text.length && /[\t\n\r ]/.test(text[position])) {
      position += 1;
    }
  }

  function parseString() {
    const start = position;
    position += 1;
    let escaped = false;
    while (position < text.length) {
      const character = text[position];
      position += 1;
      if (escaped) {
        escaped = false;
      } else if (character === "\\") {
        escaped = true;
      } else if (character === '"') {
        return JSON.parse(text.slice(start, position));
      } else if (character.charCodeAt(0) < 0x20) {
        throw new Error("unescaped control character in JSON string");
      }
    }
    throw new Error("unterminated JSON string");
  }

  function parsePrimitive() {
    const start = position;
    while (position < text.length && !/[\s,\]}]/.test(text[position])) {
      position += 1;
    }
    if (start === position) {
      throw new Error("invalid JSON primitive");
    }
    JSON.parse(text.slice(start, position));
  }

  function parseValue(jsonPath) {
    skipWhitespace();
    const character = text[position];
    if (character === "{") {
      parseObject(jsonPath);
    } else if (character === "[") {
      parseArray(jsonPath);
    } else if (character === '"') {
      parseString();
    } else {
      parsePrimitive();
    }
  }

  function parseObject(jsonPath) {
    position += 1;
    skipWhitespace();
    const keys = new Set();
    if (text[position] === "}") {
      position += 1;
      return;
    }
    while (position < text.length) {
      if (text[position] !== '"') {
        throw new Error(`object member at ${jsonPath} is not a string`);
      }
      const key = parseString();
      if (keys.has(key)) {
        throw new Error(`duplicate object member ${JSON.stringify(key)} at ${jsonPath}`);
      }
      keys.add(key);
      skipWhitespace();
      if (text[position] !== ":") {
        throw new Error(`object member at ${jsonPath} is missing a colon`);
      }
      position += 1;
      parseValue(`${jsonPath}.${key}`);
      skipWhitespace();
      if (text[position] === "}") {
        position += 1;
        return;
      }
      if (text[position] !== ",") {
        throw new Error(`object at ${jsonPath} is not closed`);
      }
      position += 1;
      skipWhitespace();
    }
    throw new Error(`object at ${jsonPath} is not closed`);
  }

  function parseArray(jsonPath) {
    position += 1;
    skipWhitespace();
    if (text[position] === "]") {
      position += 1;
      return;
    }
    let index = 0;
    while (position < text.length) {
      parseValue(`${jsonPath}[${index}]`);
      index += 1;
      skipWhitespace();
      if (text[position] === "]") {
        position += 1;
        return;
      }
      if (text[position] !== ",") {
        throw new Error(`array at ${jsonPath} is not closed`);
      }
      position += 1;
      skipWhitespace();
    }
    throw new Error(`array at ${jsonPath} is not closed`);
  }

  parseValue("$");
  skipWhitespace();
  if (position !== text.length) {
    throw new Error("trailing JSON value");
  }
}

function browserInitScript(bindingNames) {
  const NativePromise = Promise;
  const reportBlockedAttempt = globalThis[bindingNames.blockedAttempt];
  const reportContentPolicyViolation = globalThis[bindingNames.contentPolicyViolation];
  const reportResourceLoadFailure = globalThis[bindingNames.resourceLoadFailure];
  const mark = () => {
    try {
      reportBlockedAttempt("");
    } catch {
      // A missing binding makes instrumentation incomplete and is reported
      // during installation below.
    }
  };
  const markContentPolicyViolation = () => {
    try {
      reportContentPolicyViolation("");
    } catch {
      mark();
    }
  };
  const markResourceLoadFailure = () => {
    try {
      reportResourceLoadFailure("");
    } catch {
      markContentPolicyViolation();
    }
  };
  const composedPath = Event.prototype.composedPath;
  const preventDefault = Event.prototype.preventDefault;
  const eventTarget = Object.getOwnPropertyDescriptor(Event.prototype, "target")?.get;
  const localName = Object.getOwnPropertyDescriptor(Element.prototype, "localName")?.get;
  const anchorHref = Object.getOwnPropertyDescriptor(HTMLAnchorElement.prototype, "href")?.get;
  const areaHref = Object.getOwnPropertyDescriptor(HTMLAreaElement.prototype, "href")?.get;
  const anchorTarget = Object.getOwnPropertyDescriptor(HTMLAnchorElement.prototype, "target")?.get;
  const areaTarget = Object.getOwnPropertyDescriptor(HTMLAreaElement.prototype, "target")?.get;
  const linkRel = Object.getOwnPropertyDescriptor(HTMLLinkElement.prototype, "rel")?.get;
  const blockJavaScriptNavigation = (event) => {
    let path;
    try {
      path = composedPath.call(event);
    } catch {
      markContentPolicyViolation();
      return;
    }
    for (const candidate of path) {
      for (const [getHref, getTarget] of [[anchorHref, anchorTarget], [areaHref, areaTarget]]) {
        let href;
        let target;
        try {
          href = getHref?.call(candidate);
          target = getTarget?.call(candidate);
        } catch {
          continue;
        }
        if (target && target.toLowerCase() !== "_self") {
          mark();
          preventDefault.call(event);
          return;
        }
        if (href?.trimStart().toLowerCase().startsWith("javascript:")) {
          markContentPolicyViolation();
          preventDefault.call(event);
          return;
        }
      }
    }
  };
  const captureResourceLoadFailure = (event) => {
    let target;
    let name;
    try {
      target = eventTarget?.call(event);
      name = localName?.call(target);
    } catch {
      return;
    }
    if (name === "script") {
      markResourceLoadFailure();
      return;
    }
    if (name === "link") {
      let rel = "";
      try {
        rel = linkRel?.call(target) || "";
      } catch {
        markResourceLoadFailure();
        return;
      }
      if (rel.toLowerCase().split(/\s+/).includes("stylesheet")) {
        markResourceLoadFailure();
      }
    }
  };
  const install = (target, name, value) => {
    try {
      Object.defineProperty(target, name, { configurable: false, value });
    } catch {
      mark();
    }
  };
  const blockedConstructor = class {
    constructor() {
      mark();
      throw new DOMException("Blocked by render policy", "SecurityError");
    }
  };
  for (const name of [
    "WebSocket",
    "EventSource",
    "RTCPeerConnection",
    "webkitRTCPeerConnection",
    "Worker",
    "SharedWorker",
  ]) {
    install(globalThis, name, blockedConstructor);
  }
  install(
    globalThis,
    "fetch",
    () => {
      mark();
      return NativePromise.reject(new DOMException("Blocked by render policy", "SecurityError"));
    },
  );
  install(globalThis, "XMLHttpRequest", blockedConstructor);
  install(
    Element.prototype,
    "attachShadow",
    () => {
      markContentPolicyViolation();
      throw new DOMException("Shadow DOM is not serializable by render policy", "SecurityError");
    },
  );
  install(
    globalThis,
    "open",
    () => {
      mark();
      return null;
    },
  );
  install(
    Navigator.prototype,
    "sendBeacon",
    () => {
      mark();
      return false;
    },
  );
  const blockedServiceWorkerRegister = () => {
    mark();
    return NativePromise.reject(new DOMException("Blocked by render policy", "SecurityError"));
  };
  try {
    const blockedServiceWorkerContainer = Object.freeze({
      controller: null,
      register: blockedServiceWorkerRegister,
    });
    Object.defineProperty(Navigator.prototype, "serviceWorker", {
      configurable: false,
      get: () => blockedServiceWorkerContainer,
    });
  } catch {
    if (globalThis.ServiceWorkerContainer?.prototype) {
      install(globalThis.ServiceWorkerContainer.prototype, "register", blockedServiceWorkerRegister);
    } else {
      mark();
    }
  }
  if (globalThis.CookieStore?.prototype) {
    for (const name of ["set", "delete"]) {
      install(
        globalThis.CookieStore.prototype,
        name,
        () => {
          mark();
          return NativePromise.reject(new DOMException("Blocked by render policy", "SecurityError"));
        },
      );
    }
  }
  try {
    Object.defineProperty(Document.prototype, "cookie", {
      configurable: false,
      get: () => "",
      set: () => {
        mark();
        return true;
      },
    });
  } catch {
    mark();
  }
  globalThis.addEventListener("securitypolicyviolation", markContentPolicyViolation, {
    capture: true,
  });
  globalThis.addEventListener("error", captureResourceLoadFailure, { capture: true });
  globalThis.addEventListener("click", blockJavaScriptNavigation, { capture: true });
  globalThis.addEventListener("auxclick", blockJavaScriptNavigation, { capture: true });
}

function inspectDOM(root) {
  const pending = [root];
  const seen = new Set();
  let hasSecondaryDocument = false;
  let hasShadowRoot = false;

  while (pending.length > 0) {
    const node = pending.pop();
    const identifier = node.backendNodeId || node.nodeId;
    if (identifier && seen.has(identifier)) {
      continue;
    }
    if (identifier) {
      seen.add(identifier);
    }
    if (node.contentDocument) {
      hasSecondaryDocument = true;
      pending.push(node.contentDocument);
    }
    const authorShadowRoots = node.shadowRoots?.filter((root) => root.shadowRootType !== "user-agent") || [];
    if (authorShadowRoots.length) {
      hasShadowRoot = true;
      pending.push(...authorShadowRoots);
    }
    if (node.children?.length) {
      pending.push(...node.children);
    }
  }

  return { hasSecondaryDocument, hasShadowRoot };
}

async function countFrozenDOM(cdp, maxDOMNodes) {
  const { frameTree } = await cdp.send("Page.getFrameTree");
  const { executionContextId } = await cdp.send("Page.createIsolatedWorld", {
    frameId: frameTree.frame.id,
    worldName: "__mifolyo_dom_inspection__",
    grantUniveralAccess: false,
  });
  const { exceptionDetails, result } = await cdp.send("Runtime.evaluate", {
    contextId: executionContextId,
    disableBreaks: true,
    returnByValue: true,
    expression: `(() => {
      const nodeType = Object.getOwnPropertyDescriptor(Node.prototype, "nodeType").get;
      const firstChild = Object.getOwnPropertyDescriptor(Node.prototype, "firstChild").get;
      const nextSibling = Object.getOwnPropertyDescriptor(Node.prototype, "nextSibling").get;
      const namespaceURI = Object.getOwnPropertyDescriptor(Element.prototype, "namespaceURI").get;
      const localName = Object.getOwnPropertyDescriptor(Element.prototype, "localName").get;
      const templateContent = Object.getOwnPropertyDescriptor(HTMLTemplateElement.prototype, "content").get;
      const pending = [document];
      let count = 0;
      while (pending.length > 0) {
        const node = pending.pop();
        if (nodeType.call(node) === 1) {
          count += 1;
          if (count > ${maxDOMNodes}) return count;
          if (
            localName.call(node) === "template" &&
            namespaceURI.call(node) === "http://www.w3.org/1999/xhtml"
          ) {
            pending.push(templateContent.call(node));
          }
        }
        for (let child = firstChild.call(node); child; child = nextSibling.call(child)) {
          pending.push(child);
        }
      }
      return count;
    })()`,
  });
  if (exceptionDetails || result.type !== "number" || !Number.isSafeInteger(result.value)) {
    fail("render_failed");
  }
  return result.value;
}

function normalizeBrokerResource(resource, limits, aggregateBytes) {
  if (resource === null || typeof resource !== "object" || !Buffer.isBuffer(resource.body)) {
    throw new Error("broker resource is invalid");
  }
  if (resource.body.length === 0 || !validUTF8(resource.body)) {
    throw new Error("broker resource body is invalid");
  }
  if (
    resource.body.length > limits.max_resource_body_bytes ||
    aggregateBytes > limits.max_aggregate_resource_bytes ||
    resource.body.length > limits.max_aggregate_resource_bytes - aggregateBytes
  ) {
    throw new Error("broker resource body exceeds its byte limit");
  }
  return {
    body: resource.body,
    contentType: parseAndNormalizeContentType(resource.contentType),
  };
}

function requireResourceContentType(resourceType, contentType) {
  let essence;
  try {
    essence = new MIMEType(contentType).essence;
  } catch {
    throw new Error("broker resource content type is invalid");
  }
  if (
    (resourceType === "script" && essence !== "application/javascript" && essence !== "text/javascript") ||
    (resourceType === "stylesheet" && essence !== "text/css")
  ) {
    throw new Error("broker resource content type does not match its request type");
  }
}

function incrementCount(counts, key) {
  counts.set(key, (counts.get(key) || 0) + 1);
}

function containsAllCounts(expected, observed) {
  for (const [key, count] of expected) {
    if ((observed.get(key) || 0) < count) {
      return false;
    }
  }
  return true;
}

function asBrokerError(error) {
  if (error instanceof RenderError) {
    return error;
  }
  return new RenderError("resource_denied");
}

export async function renderDocument(request, brokerOrExecutablePath, explicitExecutablePath, renderOptions = {}) {
  const broker =
    typeof brokerOrExecutablePath === "function"
      ? brokerOrExecutablePath
      : typeof explicitExecutablePath === "function"
        ? explicitExecutablePath
        : undefined;
  const executablePath =
    typeof brokerOrExecutablePath === "string"
      ? brokerOrExecutablePath
      : typeof explicitExecutablePath === "string"
        ? explicitExecutablePath
        : process.env.CHROMIUM_EXECUTABLE_PATH || "/usr/bin/chromium";
  let browser;
  let timeout;
  let browserClosePromise;
  let cancellationError = null;
  let completedSuccessfully = false;
  let consoleBytes = 0;
  let consoleOverflow = false;
  let resourceRequests = 0;
  let resourceBytes = 0;
  let brokerFailure = null;
  let resourcePolicyFailure = null;

  const deadlineError = new RenderError("render_timeout");
  const deadlineAt = performance.now() + request.limits.max_render_time_ms;
  const closeBrowser = () => {
    if (!browser) {
      return Promise.resolve();
    }
    browserClosePromise ||= browser.close();
    return browserClosePromise;
  };
  const cancelOperation = (error) => {
    cancellationError ||= error;
    broker?.cancel?.(error);
    void closeBrowser().catch(() => {});
  };
  const deadline = new Promise((_, reject) => {
    timeout = setTimeout(() => {
      cancelOperation(deadlineError);
      reject(deadlineError);
    }, request.limits.max_render_time_ms);
  });
  // Setup work starts before the final render race. Attach a handler now so a
  // deadline that closes Chromium during setup is never an unhandled rejection.
  deadline.catch(() => {});

  let removeAbortListener = () => {};
  const externalAbort = new Promise((_, reject) => {
    const signal = renderOptions?.signal;
    if (!signal) {
      return;
    }
    const abort = () => {
      const error = new RenderError("render_failed");
      cancelOperation(error);
      reject(error);
    };
    if (signal.aborted) {
      abort();
      return;
    }
    signal.addEventListener("abort", abort, { once: true });
    removeAbortListener = () => signal.removeEventListener("abort", abort);
  });
  externalAbort.catch(() => {});

  const ensureBeforeDeadline = () => {
    if (cancellationError) {
      throw cancellationError;
    }
    if (performance.now() >= deadlineAt) {
      cancelOperation(deadlineError);
      throw deadlineError;
    }
  };

  try {
    if (request.mode === "brokered" && typeof broker !== "function") {
      fail("render_failed");
    }
    const effectiveURL = new URL(request.effective_url).href;
    const contentSecurityPolicy = buildContentSecurityPolicy(request);
    browser = await chromium.launch({
      executablePath,
      headless: true,
      chromiumSandbox: true,
      timeout: Math.max(1, Math.ceil(deadlineAt - performance.now())),
      args: [
        "--disable-background-networking",
        "--disable-breakpad",
        "--disable-client-side-phishing-detection",
        "--disable-component-update",
        "--disable-default-apps",
        "--disable-domain-reliability",
        "--disable-features=AutofillServerCommunication,CertificateTransparencyComponentUpdater,OptimizationHints,MediaRouter",
        "--disable-sync",
        "--disable-setuid-sandbox",
        "--dns-prefetch-disable",
        "--metrics-recording-only",
        "--no-default-browser-check",
        "--no-first-run",
      ],
    });
    ensureBeforeDeadline();
    const context = await browser.newContext({
      acceptDownloads: false,
      javaScriptEnabled: true,
      serviceWorkers: "block",
      viewport: { width: 1280, height: 720 },
    });
    await context.clearCookies();
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);
    await cdp.send("Page.enable");
    await cdp.send("Network.enable");
    await cdp.send("Network.setCacheDisabled", { cacheDisabled: true });
    await cdp.send("DOM.enable");
    await cdp.send("CSS.enable");
    page.setDefaultNavigationTimeout(request.limits.max_render_time_ms);
    page.setDefaultTimeout(request.limits.max_render_time_ms);

    let navigationAttempts = 0;
    let popupAttempts = 0;
    let blockedAPIAttempts = 0;
    let contentPolicyViolations = 0;
    let resourceLoadFailures = 0;
    let scriptParseFailures = 0;
    let runtimeExceptions = 0;
    let secondaryDocuments = 0;
    let debuggerPauses = 0;
    let activityGeneration = 0;
    let activeRouteHandlers = 0;
    let debuggerPauseRecovery = Promise.resolve();
    let initialRequestFulfilled = false;
    let initialNavigationComplete = false;
    let resourceAdmissionOpen = false;
    const bootstrapScriptHashes = new Set();
    const approvedPageScriptIDs = new Set();
    const approvedExternalScriptURLs = new Set();
    const approvedExternalStylesheetURLs = new Set();
    const expectedExternalScripts = new Map();
    const parsedExternalScripts = new Map();
    const expectedExternalStylesheets = new Map();
    const registeredExternalStylesheets = new Map();
    const trustedInspectionContextIDs = new Set();
    let provenancePhase = "bootstrap";
    let pageScriptStarted = false;

    cdp.on("Debugger.scriptParsed", (event) => {
      if (provenancePhase === "bootstrap") {
        if (event.embedderName === "") {
          bootstrapScriptHashes.add(event.hash);
        }
        return;
      }
      if (provenancePhase !== "render") {
        return;
      }
      if (trustedInspectionContextIDs.has(event.executionContextId)) {
        return;
      }
      const isPageScript =
        event.embedderName === effectiveURL ||
        (event.embedderName === "" && event.url === effectiveURL && !event.hasSourceURL);
      const isApprovedExternal =
        approvedExternalScriptURLs.has(event.embedderName) ||
        (event.embedderName === "" && !event.hasSourceURL && approvedExternalScriptURLs.has(event.url));
      if (isPageScript || isApprovedExternal) {
        if (isApprovedExternal) {
          const sourceURL = approvedExternalScriptURLs.has(event.embedderName) ? event.embedderName : event.url;
          incrementCount(parsedExternalScripts, sourceURL);
        }
        activityGeneration += 1;
        pageScriptStarted = true;
        approvedPageScriptIDs.add(event.scriptId);
        return;
      }
      if (pageScriptStarted && event.isModule && event.embedderName === "" && event.url === "") {
        approvedPageScriptIDs.add(event.scriptId);
        return;
      }
      if (event.stackTrace?.callFrames?.some((frame) => approvedPageScriptIDs.has(frame.scriptId))) {
        approvedPageScriptIDs.add(event.scriptId);
        return;
      }
      if (pageScriptStarted || !bootstrapScriptHashes.has(event.hash)) {
        contentPolicyViolations += 1;
      }
    });
    cdp.on("Debugger.scriptFailedToParse", (event) => {
      if (provenancePhase === "render" && !trustedInspectionContextIDs.has(event.executionContextId)) {
        scriptParseFailures += 1;
        activityGeneration += 1;
      }
    });
    cdp.on("Debugger.paused", () => {
      debuggerPauses += 1;
      debuggerPauseRecovery = debuggerPauseRecovery
        .then(async () => {
          await cdp.send("Debugger.setBreakpointsActive", { active: false });
          await cdp.send("Debugger.setSkipAllPauses", { skip: true });
          await cdp.send("Debugger.resume");
        })
        .catch(() => {});
    });
    await cdp.send("Debugger.enable");

    cdp.on("CSS.styleSheetAdded", ({ header }) => {
      if (provenancePhase !== "render" || header.origin !== "regular") {
        return;
      }
      if (
        approvedExternalStylesheetURLs.has(header.sourceURL)
      ) {
        incrementCount(registeredExternalStylesheets, header.sourceURL);
        activityGeneration += 1;
        return;
      }
      if (
        (header.isInline || header.sourceURL === effectiveURL) &&
        (header.sourceURL === "" || header.sourceURL === effectiveURL)
      ) {
        return;
      }
      contentPolicyViolations += 1;
    });

    cdp.on("Page.frameRequestedNavigation", (event) => {
      if (event.url.trimStart().toLowerCase().startsWith("javascript:")) {
        navigationAttempts += 1;
      }
    });

    cdp.on("Runtime.exceptionThrown", ({ exceptionDetails }) => {
      if (
        provenancePhase === "render" &&
        !trustedInspectionContextIDs.has(exceptionDetails.executionContextId)
      ) {
        runtimeExceptions += 1;
        activityGeneration += 1;
      }
    });

    const blockedAttemptBinding = "__mifolyoReportBlockedAPIAttempt";
    const contentPolicyViolationBinding = "__mifolyoReportContentPolicyViolation";
    const resourceLoadFailureBinding = "__mifolyoReportResourceLoadFailure";
    cdp.on("Runtime.bindingCalled", (event) => {
      if (event.name === blockedAttemptBinding) {
        blockedAPIAttempts += 1;
      } else if (event.name === contentPolicyViolationBinding) {
        contentPolicyViolations += 1;
      } else if (event.name === resourceLoadFailureBinding) {
        resourceLoadFailures += 1;
      }
      activityGeneration += 1;
    });
    await cdp.send("Runtime.enable");
    await cdp.send("Runtime.addBinding", { name: blockedAttemptBinding });
    await cdp.send("Runtime.addBinding", { name: contentPolicyViolationBinding });
    await cdp.send("Runtime.addBinding", { name: resourceLoadFailureBinding });
    await context.addInitScript(browserInitScript, {
      blockedAttempt: blockedAttemptBinding,
      contentPolicyViolation: contentPolicyViolationBinding,
      resourceLoadFailure: resourceLoadFailureBinding,
    });
    if (typeof context.routeWebSocket === "function") {
      await context.routeWebSocket("**/*", async (webSocketRoute) => {
        blockedAPIAttempts += 1;
        activityGeneration += 1;
        await webSocketRoute.close({ code: 1008, reason: "Blocked by render policy" }).catch(() => {});
      });
    }

    let brokerTail = Promise.resolve();
    let pendingBrokerOperations = 0;
    let totalAdmittedBrokerOperations = 0;
    const queueBrokerOperation = (operation) => {
      pendingBrokerOperations += 1;
      activityGeneration += 1;
      const queued = brokerTail.then(async () => {
        if (brokerFailure) {
          throw brokerFailure;
        }
        return operation();
      });
      brokerTail = queued.then(
        () => {
          pendingBrokerOperations -= 1;
          activityGeneration += 1;
        },
        (error) => {
          pendingBrokerOperations -= 1;
          brokerFailure ||= asBrokerError(error);
          resourceAdmissionOpen = false;
          activityGeneration += 1;
        },
      );
      return queued;
    };
    const drainBrokerOperations = async () => {
      let observed;
      do {
        observed = brokerTail;
        await observed;
        await Promise.resolve();
      } while (observed !== brokerTail);
      broker?.assertHealthy?.();
      if (brokerFailure) {
        throw brokerFailure;
      }
      broker?.assertHealthy?.();
    };
    const admitBrokerOperation = () => {
      if (!resourceAdmissionOpen) {
        return false;
      }
      if (totalAdmittedBrokerOperations >= request.limits.max_resource_requests) {
        resourceAdmissionOpen = false;
        resourcePolicyFailure ||= new RenderError("resource_request_limit");
        activityGeneration += 1;
        return false;
      }
      totalAdmittedBrokerOperations += 1;
      activityGeneration += 1;
      return true;
    };

    const approvedHosts = {
      script: new Set(request.resource_hosts.script),
      stylesheet: new Set(request.resource_hosts.stylesheet),
    };
    let bootstrapping = true;
    let bootstrapRequestFulfilled = false;
    await context.route("**/*", async (route) => {
      activeRouteHandlers += 1;
      activityGeneration += 1;
      try {
        const browserRequest = route.request();
        let owningPage;
        try {
          owningPage = browserRequest.frame().page();
        } catch {
          owningPage = null;
        }
        if (owningPage !== page) {
          if (browserRequest.isNavigationRequest()) {
            popupAttempts += 1;
          } else {
            resourcePolicyFailure ||= new RenderError("resource_request_denied");
          }
          await route.abort("blockedbyclient").catch(() => {});
          return;
        }
        const isInitialNavigation =
          browserRequest.isNavigationRequest() &&
          browserRequest.frame() === page.mainFrame() &&
          browserRequest.method() === "GET" &&
          browserRequest.url() === effectiveURL;
      if (bootstrapping) {
        if (!bootstrapRequestFulfilled && isInitialNavigation) {
          bootstrapRequestFulfilled = true;
          await route.fulfill({
            status: 200,
            contentType: "text/html; charset=utf-8",
            headers: { "cache-control": "no-store" },
            body: '<!doctype html><html><head><link rel="icon" href="data:,"></head><body></body></html>',
          });
          return;
        }
        await route.abort("blockedbyclient");
        return;
      }
      if (!initialRequestFulfilled && isInitialNavigation) {
        initialRequestFulfilled = true;
        await route.fulfill({
          status: 200,
          contentType: "text/html; charset=utf-8",
          headers: {
            "cache-control": "no-store",
            "content-security-policy": contentSecurityPolicy,
            "referrer-policy": "no-referrer",
            "x-dns-prefetch-control": "off",
            "x-content-type-options": "nosniff",
          },
          body: request.html,
        });
        return;
      }
      if (browserRequest.isNavigationRequest()) {
        navigationAttempts += 1;
        await route.abort("blockedbyclient");
        return;
      }

      if (request.mode === "inline_only") {
        resourcePolicyFailure ||= new RenderError("resource_request_denied");
        await route.abort("blockedbyclient");
        return;
      }
      if (!resourceAdmissionOpen) {
        resourcePolicyFailure ||= new RenderError("resource_request_denied");
        await route.abort("blockedbyclient");
        return;
      }

      const method = browserRequest.method();
      const resourceType = browserRequest.resourceType();
      let resourceURL;
      try {
        resourceURL = validateBrowserResourceRequest(
          browserRequest.url(),
          method,
          resourceType,
          approvedHosts,
        );
      } catch {
        resourcePolicyFailure ||= new RenderError("resource_request_denied");
        await route.abort("blockedbyclient");
        return;
      }

      // Reserve one of the bounded supported-resource operations before
      // appending anything to brokerTail. Browser request storms can therefore
      // retain at most max_resource_requests queued operations, even while the
      // first broker exchange is delayed. A reserved operation remains valid
      // if a later route closes admission; only operations skipped by an
      // earlier broker failure avoid becoming protocol resource intents.
      if (!admitBrokerOperation()) {
        await route.abort("blockedbyclient").catch(() => {});
        return;
      }

      try {
        await queueBrokerOperation(async () => {
          ensureBeforeDeadline();
          if (resourceRequests >= request.limits.max_resource_requests) {
            resourceAdmissionOpen = false;
            resourcePolicyFailure ||= new RenderError("resource_request_limit");
            throw resourcePolicyFailure;
          }
          resourceRequests += 1;
          activityGeneration += 1;
          let brokered;
          try {
            const resource = await broker({
              url: resourceURL,
              method: "GET",
              resourceType,
            });
            brokered = normalizeBrokerResource(resource, request.limits, resourceBytes);
            requireResourceContentType(resourceType, brokered.contentType);
          } catch (error) {
            throw asBrokerError(error);
          }
          // A successful broker reply is authoritative at this point. Count
          // its accepted bytes before deadline and browser-fulfillment checks
          // so every successful Go reply is represented in any terminal error.
          resourceBytes += brokered.body.length;
          activityGeneration += 1;
          ensureBeforeDeadline();
          if (resourceType === "script") {
            approvedExternalScriptURLs.add(resourceURL);
            incrementCount(expectedExternalScripts, resourceURL);
          } else {
            approvedExternalStylesheetURLs.add(resourceURL);
            incrementCount(expectedExternalStylesheets, resourceURL);
          }
          try {
            await route.fulfill({
              status: 200,
              headers: {
                "Content-Type": brokered.contentType,
                "Cache-Control": "no-store",
                "X-Content-Type-Options": "nosniff",
              },
              body: brokered.body,
            });
          } catch {
            throw new RenderError("render_failed");
          }
        });
      } catch {
        await route.abort("blockedbyclient").catch(() => {});
      }
      } finally {
        activeRouteHandlers -= 1;
        activityGeneration += 1;
      }
    });
    await page.goto(effectiveURL, { waitUntil: "load" });
    await page.waitForTimeout(0);
    if (!bootstrapRequestFulfilled) {
      fail("initial_document_failed");
    }
    await cdp.send("Debugger.setBreakpointsActive", { active: false });
    await cdp.send("Debugger.setSkipAllPauses", { skip: true });
    provenancePhase = "render";

    page.on("console", (message) => {
      if (provenancePhase !== "render") {
        return;
      }
      const messageBytes = Buffer.byteLength(message.text(), "utf8") + 1;
      const remaining = request.limits.max_console_bytes - consoleBytes;
      if (messageBytes > remaining) {
        consoleBytes = request.limits.max_console_bytes;
        consoleOverflow = true;
      } else {
        consoleBytes += messageBytes;
      }
    });
    page.on("popup", async (popup) => {
      popupAttempts += 1;
      activityGeneration += 1;
      await popup.close().catch(() => {});
    });
    page.on("download", async (download) => {
      resourcePolicyFailure ||= new RenderError("resource_request_denied");
      activityGeneration += 1;
      await download.cancel().catch(() => {});
    });
    page.on("requestfailed", () => {
      if (provenancePhase === "render") {
        resourceLoadFailures += 1;
        activityGeneration += 1;
      }
    });
    page.on("pageerror", () => {
      if (provenancePhase === "render") {
        runtimeExceptions += 1;
        activityGeneration += 1;
      }
    });
    page.on("crash", () => {
      if (provenancePhase === "render") {
        resourcePolicyFailure ||= new RenderError("render_failed");
        activityGeneration += 1;
      }
    });
    page.on("frameattached", (frame) => {
      if (frame !== page.mainFrame()) {
        secondaryDocuments += 1;
      }
    });
    page.on("framenavigated", (frame) => {
      if (initialNavigationComplete && frame === page.mainFrame()) {
        navigationAttempts += 1;
      }
    });
    bootstrapping = false;
    resourceAdmissionOpen = true;

    const enforcePolicyState = () => {
      if (!initialRequestFulfilled) {
        fail("initial_document_failed");
      }
      if (brokerFailure) {
        throw brokerFailure;
      }
      if (navigationAttempts > 0 || page.url() !== effectiveURL) {
        fail("navigation_denied");
      }
      if (popupAttempts > 0) {
        fail("popup_denied");
      }
      if (secondaryDocuments > 0) {
        fail("secondary_document_denied");
      }
      if (blockedAPIAttempts > 0) {
        fail("forbidden_api");
      }
      if (request.mode === "inline_only" && resourceRequests > 0) {
        fail("resource_request_denied");
      }
      if (resourcePolicyFailure) {
        throw resourcePolicyFailure;
      }
      if (contentPolicyViolations > 0 || debuggerPauses > 0) {
        fail("content_policy_denied");
      }
      if (scriptParseFailures > 0 || runtimeExceptions > 0) {
        fail("script_execution_failed");
      }
      if (
        resourceLoadFailures > 0 ||
        !containsAllCounts(expectedExternalScripts, parsedExternalScripts) ||
        !containsAllCounts(expectedExternalStylesheets, registeredExternalStylesheets)
      ) {
        fail("resource_load_failed");
      }
      if (consoleOverflow) {
        fail("console_limit");
      }
    };

    const waitForStableBrokerTurns = async (executionContextId = null) => {
      let stableTurns = 0;
      while (stableTurns < 2) {
        ensureBeforeDeadline();
        await drainBrokerOperations();
        const observedGeneration = activityGeneration;
        if (executionContextId !== null) {
          const { exceptionDetails } = await cdp.send("Runtime.evaluate", {
            contextId: executionContextId,
            awaitPromise: true,
            disableBreaks: true,
            returnByValue: true,
            expression: "new Promise((resolve) => setTimeout(resolve, 0))",
          });
          if (exceptionDetails) {
            fail("render_failed");
          }
        } else {
          await cdp.send("Page.getFrameTree");
        }
        await page.waitForTimeout(0);
        await drainBrokerOperations();
        if (
          activeRouteHandlers === 0 &&
          pendingBrokerOperations === 0 &&
          observedGeneration === activityGeneration
        ) {
          stableTurns += 1;
        } else {
          stableTurns = 0;
        }
      }
    };

    const renderWork = async () => {
      await page.goto(effectiveURL, { waitUntil: "domcontentloaded" });
      initialNavigationComplete = true;
      if (request.limits.settle_time_ms > 0) {
        await page.waitForTimeout(request.limits.settle_time_ms);
      }
      await page.waitForTimeout(0);
      await debuggerPauseRecovery;
      const { frameTree } = await cdp.send("Page.getFrameTree");
      const { executionContextId: quiescenceContextId } = await cdp.send("Page.createIsolatedWorld", {
        frameId: frameTree.frame.id,
        worldName: "__mifolyo_render_quiescence__",
        grantUniveralAccess: false,
      });
      trustedInspectionContextIDs.add(quiescenceContextId);
      await waitForStableBrokerTurns(quiescenceContextId);
      resourceAdmissionOpen = false;
      await cdp.send("Emulation.setScriptExecutionDisabled", { value: true });
      await waitForStableBrokerTurns();
      if (pendingBrokerOperations !== 0) {
        fail("render_failed");
      }
      enforcePolicyState();
      provenancePhase = "terminal";
      const { root } = await cdp.send("DOM.getDocument", { depth: -1, pierce: true });
      const inspection = inspectDOM(root);
      if (inspection.hasSecondaryDocument) {
        fail("secondary_document_denied");
      }
      if (inspection.hasShadowRoot) {
        fail("shadow_dom_denied");
      }
      const domNodes = await countFrozenDOM(cdp, request.limits.max_dom_nodes);
      if (domNodes < 1 || domNodes > request.limits.max_dom_nodes) {
        fail("dom_node_limit");
      }
      const { outerHTML: html } = await cdp.send("DOM.getOuterHTML", { nodeId: root.nodeId });
      if (Buffer.byteLength(html, "utf8") > request.limits.max_rendered_dom_bytes) {
        fail("dom_byte_limit");
      }
      await waitForStableBrokerTurns();
      if (pendingBrokerOperations !== 0) {
        fail("render_failed");
      }
      enforcePolicyState();
      const result = { html, domNodes, consoleBytes, resourceRequests, resourceBytes };
      await closeBrowser();
      ensureBeforeDeadline();
      broker?.assertHealthy?.();
      if (broker?.hasOutstandingIntent?.() || pendingBrokerOperations !== 0) {
        fail("render_failed");
      }
      return result;
    };

    const result = await Promise.race([renderWork(), deadline, externalAbort]);
    completedSuccessfully = true;
    return result;
  } catch (error) {
    broker?.cancel?.(error instanceof RenderError ? error : new RenderError("render_failed"));
    const cause = brokerFailure || resourcePolicyFailure || cancellationError || error;
    let renderError;
    if (cause instanceof RenderError) {
      renderError = cause;
    } else if (cause?.name === "TimeoutError") {
      renderError = new RenderError("render_timeout");
    } else {
      renderError = new RenderError("render_failed");
    }
    renderError.result = { consoleBytes, resourceRequests, resourceBytes };
    throw renderError;
  } finally {
    // Browser teardown is part of job completion. In particular, a canceled
    // job must keep the server busy until Chromium has exited so a hostile page
    // cannot overlap its terminating browser with the next render.
    await closeBrowser().catch(() => {});
    clearTimeout(timeout);
    removeAbortListener();
    if (completedSuccessfully && cancellationError) {
      throw cancellationError;
    }
  }
}

export function renderResult(jobID, status, result = {}, errorCode = "") {
  return {
    version: PROTOCOL_VERSION,
    kind: "render_result",
    job_id: jobID,
    status,
    html: result.html ?? "",
    dom_nodes: result.domNodes ?? 0,
    console_bytes: result.consoleBytes ?? 0,
    resource_requests: result.resourceRequests ?? 0,
    resource_bytes: result.resourceBytes ?? 0,
    error_code: errorCode,
  };
}

export function encodeFrame(value) {
  const payload = Buffer.from(JSON.stringify(value), "utf8");
  if (payload.length === 0 || payload.length > MAX_FRAME_BYTES) {
    throw new Error("response frame size is invalid");
  }
  const header = Buffer.alloc(4);
  header.writeUInt32BE(payload.length);
  return Buffer.concat([header, payload]);
}

export function createFrameReader(socket) {
  let buffer = Buffer.alloc(0);
  let expectedLength = null;
  let failure = null;
  let ended = false;
  let waiter = null;
  let phase = "idle";

  const failReader = (error, destroy = false) => {
    if (failure) {
      return;
    }
    failure = error;
    const pending = waiter;
    waiter = null;
    phase = "failed";
    pending?.reject(error);
    if (destroy && !socket.destroyed) {
      socket.destroy();
    }
  };
  const parseFrame = () => {
    if (failure || phase !== "awaiting") {
      return;
    }
    if (expectedLength === null) {
      if (buffer.length < 4) {
        return;
      }
      expectedLength = buffer.readUInt32BE(0);
      buffer = buffer.subarray(4);
      if (expectedLength === 0 || expectedLength > MAX_FRAME_BYTES) {
        failReader(new Error("invalid request frame length"), true);
        return;
      }
    }
    if (buffer.length < expectedLength) {
      return;
    }
    const payload = Buffer.from(buffer.subarray(0, expectedLength));
    buffer = buffer.subarray(expectedLength);
    expectedLength = null;
    if (buffer.length !== 0) {
      failReader(new Error("multiple or early frames violate the V2 receive phase"), true);
      return;
    }
    const pending = waiter;
    waiter = null;
    phase = "idle";
    pending.resolve(payload);
  };

  socket.setTimeout?.(35000);
  socket.on("timeout", () => failReader(new Error("request timeout"), true));
  socket.on("error", (error) => failReader(error));
  socket.on("end", () => {
    ended = true;
    if (phase === "awaiting" || expectedLength !== null || buffer.length !== 0) {
      failReader(new Error("truncated request frame"));
    }
  });
  socket.on("data", (chunk) => {
    if (failure) {
      return;
    }
    if (phase !== "awaiting") {
      failReader(new Error("unsolicited bytes violate the V2 receive phase"), true);
      return;
    }
    buffer = buffer.length === 0 ? Buffer.from(chunk) : Buffer.concat([buffer, chunk]);
    parseFrame();
  });

  return {
    read(label = "frame") {
      if (failure) {
        return Promise.reject(failure);
      }
      if (ended) {
        return Promise.reject(new Error("connection ended before the next frame"));
      }
      if (phase !== "idle" || waiter || expectedLength !== null || buffer.length !== 0) {
        return Promise.reject(new Error(`cannot await ${label} while another V2 frame is pending`));
      }
      phase = "awaiting";
      return new Promise((resolve, reject) => {
        waiter = { resolve, reject };
      });
    },
    assertIdle() {
      if (failure) {
        throw failure;
      }
      if (ended) {
        throw new Error("connection ended before the terminal frame");
      }
      if (phase !== "idle" || waiter || expectedLength !== null || buffer.length !== 0) {
        throw new Error("V2 frame reader is not idle");
      }
    },
    cancel(error = new Error("frame reader cancelled"), destroy = false) {
      failReader(error, destroy);
    },
    get isAwaiting() {
      return phase === "awaiting";
    },
  };
}

function writeSocketFrame(socket, value) {
  const frame = encodeFrame(value);
  return new Promise((resolve, reject) => {
    socket.write(frame, (error) => {
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    });
  });
}

export function createSocketBroker(socket, reader, request) {
  let nextIntentID = 1;
  let aggregateBytes = 0;
  let failure = null;
  let tail = Promise.resolve();
  let outstandingIntent = false;

  const socketBroker = (intent) => {
    const rawExchange = tail.then(async () => {
      if (failure) {
        throw failure;
      }
      if (
        intent === null ||
        typeof intent !== "object" ||
        typeof intent.url !== "string" ||
        intent.method !== "GET" ||
        (intent.resourceType !== "script" && intent.resourceType !== "stylesheet")
      ) {
        throw new RenderError("render_failed");
      }
      reader.assertIdle();
      const intentID = nextIntentID;
      nextIntentID += 1;
      outstandingIntent = true;
      const replyPromise = reader.read("resource reply");
      try {
        await writeSocketFrame(socket, {
          version: PROTOCOL_VERSION,
          kind: "resource_intent",
          job_id: request.job_id,
          intent_id: intentID,
          url: intent.url,
          method: intent.method,
          resource_type: intent.resourceType,
        });
        const payload = await replyPromise;
        const reply = validateResourceReplyPayload(
          payload,
          request.job_id,
          intentID,
          request.limits,
          aggregateBytes,
        );
        reader.assertIdle();
        if (reply.status === "error") {
          throw new RenderError("resource_denied");
        }
        aggregateBytes += reply.bodyBytes;
        return { body: reply.body, contentType: reply.contentType };
      } finally {
        if (reader.isAwaiting) {
          reader.cancel(new RenderError("render_failed"), true);
        }
        outstandingIntent = false;
      }
    });
    const exchange = rawExchange.catch((error) => {
      throw error instanceof RenderError ? error : new RenderError("render_failed");
    });
    tail = exchange.then(
      () => undefined,
      (error) => {
        failure ||= error instanceof RenderError ? error : new RenderError("render_failed");
      },
    );
    return exchange;
  };
  socketBroker.cancel = (error = new RenderError("render_failed")) => {
    failure ||= error instanceof RenderError ? error : new RenderError("render_failed");
    if (outstandingIntent) {
      reader.cancel(failure, true);
    }
  };
  socketBroker.assertHealthy = () => {
    reader.assertIdle();
    if (failure) {
      throw failure;
    }
    if (outstandingIntent) {
      throw new RenderError("render_failed");
    }
  };
  socketBroker.hasOutstandingIntent = () => outstandingIntent;
  return socketBroker;
}

export function createServer(socketPath = process.env.RENDER_SOCKET || DEFAULT_SOCKET_PATH) {
  if (!path.isAbsolute(socketPath) || path.normalize(socketPath) !== socketPath) {
    throw new Error("RENDER_SOCKET must be a clean absolute path");
  }
  const socketDirectory = path.dirname(socketPath);
  const directory = fs.statSync(socketDirectory);
  if (!directory.isDirectory()) {
    throw new Error("render socket parent is not a directory");
  }
  if (fs.existsSync(socketPath)) {
    const existing = fs.lstatSync(socketPath);
    if (!existing.isSocket()) {
      throw new Error("refusing to replace a non-socket render path");
    }
    fs.unlinkSync(socketPath);
  }

  let active = false;
  const server = net.createServer(async (socket) => {
    let jobID = "00000000000000000000000000000000";
    let broker;
    const reader = createFrameReader(socket);
    const abortController = new AbortController();
    socket.once("close", () => abortController.abort());
    try {
      const payload = await reader.read("render start");
      const request = validateRequestPayload(payload);
      reader.assertIdle();
      jobID = request.job_id;
      if (active) {
        socket.end(encodeFrame(renderResult(jobID, "error", {}, "worker_busy")));
        return;
      }
      active = true;
      try {
        broker = request.mode === "brokered" ? createSocketBroker(socket, reader, request) : undefined;
        const result = await renderDocument(request, broker, undefined, { signal: abortController.signal });
        if (broker?.hasOutstandingIntent?.()) {
          socket.destroy();
          return;
        }
        reader.assertIdle();
        socket.end(encodeFrame(renderResult(jobID, "ok", result)));
      } catch (error) {
        const code = error instanceof RenderError ? error.code : "render_failed";
        if (broker?.hasOutstandingIntent?.()) {
          socket.destroy();
          return;
        }
        if (!socket.destroyed) {
          try {
            reader.assertIdle();
          } catch {
            socket.destroy();
            return;
          }
          socket.end(encodeFrame(renderResult(jobID, "error", error?.result || {}, code)));
        }
      } finally {
        active = false;
      }
    } catch {
      if (!socket.destroyed) {
        socket.end(encodeFrame(renderResult(jobID, "error", {}, "invalid_request")));
      }
    }
  });
  server.listen(socketPath, () => fs.chmodSync(socketPath, 0o660));
  server.on("close", () => {
    try {
      if (fs.lstatSync(socketPath).isSocket()) {
        fs.unlinkSync(socketPath);
      }
    } catch (error) {
      if (error?.code !== "ENOENT") {
        throw error;
      }
    }
  });
  return server;
}

function main() {
  const server = createServer();
  const shutdown = () => server.close(() => process.exit(0));
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
