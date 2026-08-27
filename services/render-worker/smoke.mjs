import assert from "node:assert/strict";
import http from "node:http";

import { renderDocument } from "./worker.mjs";

const request = {
  version: 2,
  kind: "render_start",
  job_id: "0123456789abcdef0123456789abcdef",
  mode: "inline_only",
  effective_url: "https://render.example.org/app",
  html: `<!doctype html><html><body><main id="root"></main><script>
    document.querySelector('#root').textContent = 'rendered inline fixture';
  </script></body></html>`,
  resource_hosts: {
    script: [],
    stylesheet: [],
  },
  limits: {
    max_render_time_ms: 10000,
    settle_time_ms: 50,
    max_resource_requests: 0,
    max_aggregate_resource_bytes: 0,
    max_resource_body_bytes: 0,
    max_rendered_dom_bytes: 1048576,
    max_dom_nodes: 1000,
    max_redirect_hops: 0,
    max_console_bytes: 1024,
  },
};

const result = await renderDocument(request);
assert.match(result.html, /rendered inline fixture/);
assert.equal(result.resourceRequests, 0);
assert.equal(result.resourceBytes, 0);
assert.ok(result.domNodes > 0);

for (const effectiveURL of [
  "https://render.example.org:443/app",
  "https://render.example.org",
  "HTTPS://RENDER.EXAMPLE.ORG/app",
]) {
  const canonicalURL = structuredClone(request);
  canonicalURL.effective_url = effectiveURL;
  canonicalURL.html = "<!doctype html><html><body>canonical URL fixture</body></html>";
  canonicalURL.limits.max_console_bytes = 0;
  const canonicalURLResult = await renderDocument(canonicalURL);
  assert.match(canonicalURLResult.html, /canonical URL fixture/);
  assert.equal(canonicalURLResult.consoleBytes, 0);
}

const dynamicInline = structuredClone(request);
dynamicInline.html = `<!doctype html><html><body><script>
  const script = document.createElement('script');
  script.textContent = "document.body.dataset.dynamicInline = 'true'";
  document.body.append(script);
</script></body></html>`;
const dynamicInlineResult = await renderDocument(dynamicInline);
assert.match(dynamicInlineResult.html, /data-dynamic-inline="true"/);

const dynamicInlineModule = structuredClone(request);
dynamicInlineModule.html = `<!doctype html><html><body><script>
  const script = document.createElement('script');
  script.type = 'module';
  script.textContent = "document.body.dataset.dynamicModule = 'true'";
  document.body.append(script);
</script></body></html>`;
const dynamicInlineModuleResult = await renderDocument(dynamicInlineModule);
assert.match(dynamicInlineModuleResult.html, /data-dynamic-module="true"/);

const debuggerStatement = structuredClone(request);
debuggerStatement.html = `<!doctype html><html><body><script>
  for (let index = 0; index < 50000; index += 1) { debugger; }
  document.body.dataset.debuggerResumed = 'true';
</script></body></html>`;
const debuggerStatementResult = await renderDocument(debuggerStatement);
assert.match(debuggerStatementResult.html, /data-debugger-resumed="true"/);

const formControls = structuredClone(request);
formControls.html = `<!doctype html><html><body>
  <input value="fixture"><select><option>one</option></select><textarea>two</textarea>
</body></html>`;
const formControlsResult = await renderDocument(formControls);
assert.match(formControlsResult.html, /<input value="fixture">/);

const externalRequest = structuredClone(request);
externalRequest.html = "<!doctype html><html><body><script src='https://cdn.example.org/app.js'></script></body></html>";
await assert.rejects(
  renderDocument(externalRequest),
  (error) => error?.code === "content_policy_denied" || error?.code === "resource_request_denied",
);

const forbiddenAPI = structuredClone(request);
forbiddenAPI.html = "<!doctype html><html><body><script>fetch('/data').catch(() => {})</script></body></html>";
await assert.rejects(
  renderDocument(forbiddenAPI),
  (error) => error?.code === "forbidden_api",
);

const serviceWorkerAttempt = structuredClone(request);
serviceWorkerAttempt.html = `<!doctype html><html><body><script>
  navigator.serviceWorker.register('/service-worker.js').catch(() => {});
</script></body></html>`;
await assert.rejects(
  renderDocument(serviceWorkerAttempt),
  (error) => error?.code === "forbidden_api",
);

const cookieStoreAttempt = structuredClone(request);
cookieStoreAttempt.html = `<!doctype html><html><body><script>
  if (globalThis.cookieStore) {
    cookieStore.set('fixture', 'blocked').catch(() => {});
  } else {
    document.cookie = 'fixture=blocked';
  }
</script></body></html>`;
await assert.rejects(
  renderDocument(cookieStoreAttempt),
  (error) => error?.code === "forbidden_api",
);

let popupCanaryHits = 0;
const popupCanary = http.createServer((_incoming, response) => {
  popupCanaryHits += 1;
  response.writeHead(200, { "content-type": "text/html" });
  response.end("<!doctype html><html><body>network bypass</body></html>");
});
await new Promise((resolve, reject) => {
  popupCanary.once("error", reject);
  popupCanary.listen(0, "127.0.0.1", resolve);
});
try {
  const popupAttempt = structuredClone(request);
  popupAttempt.html = `<!doctype html><html><body>
    <a id="popup" target="_blank" href="http://127.0.0.1:${popupCanary.address().port}/popup">open</a>
    <script>document.querySelector('#popup').click()</script>
  </body></html>`;
  await assert.rejects(
    renderDocument(popupAttempt),
    (error) => error?.code === "forbidden_api" || error?.code === "popup_denied",
  );
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.equal(popupCanaryHits, 0, "popup must not reach the loopback canary");
} finally {
  await new Promise((resolve) => popupCanary.close(resolve));
}

const tamperedBindingAPI = structuredClone(request);
tamperedBindingAPI.html = `<!doctype html><html><body><script>
  try {
    globalThis.__mifolyoReportBlockedAPIAttempt = () => {};
    delete globalThis.__mifolyoReportBlockedAPIAttempt;
  } catch {}
  fetch('/data').catch(() => {});
</script></body></html>`;
await assert.rejects(
  renderDocument(tamperedBindingAPI),
  (error) => error?.code === "forbidden_api",
);

const tamperedBindingCSP = structuredClone(request);
tamperedBindingCSP.html = `<!doctype html><html><body><script>
  try {
    globalThis.__mifolyoReportContentPolicyViolation = () => {};
    delete globalThis.__mifolyoReportContentPolicyViolation;
  } catch {}
  const stylesheet = document.createElement('link');
  stylesheet.rel = 'stylesheet';
  stylesheet.href = 'data:text/css,body{color:red}';
  document.head.append(stylesheet);
</script></body></html>`;
await assert.rejects(
  renderDocument(tamperedBindingCSP),
  (error) => error?.code === "content_policy_denied",
);

for (const [name, html] of [
  [
    "data script",
    "<!doctype html><html><body><script src=\"data:text/javascript,document.body.dataset.executed='true'\"></script></body></html>",
  ],
  [
    "blob script",
    `<!doctype html><html><body><script>
      const source = new Blob(["document.body.dataset.executed = 'true'"], {type: 'text/javascript'});
      const script = document.createElement('script');
      script.src = URL.createObjectURL(source);
      document.body.append(script);
    </script></body></html>`,
  ],
  [
    "dynamic data import",
    "<!doctype html><html><body><script>import(\"data:text/javascript,document.body.dataset.executed='true'\").catch(() => {})</script></body></html>",
  ],
  [
    "data stylesheet",
    "<!doctype html><html><head><link rel='stylesheet' href='data:text/css,body{color:red}'></head><body></body></html>",
  ],
]) {
  const contentPolicyBypass = structuredClone(request);
  contentPolicyBypass.html = html;
  await assert.rejects(
    renderDocument(contentPolicyBypass),
    (error) => error?.code === "content_policy_denied",
    name,
  );
}

for (const [name, html] of [
  ["blank iframe", "<!doctype html><html><body><iframe></iframe></body></html>"],
  [
    "srcdoc iframe",
    "<!doctype html><html><body><iframe srcdoc='<script>document.body.textContent=1</script>'></iframe></body></html>",
  ],
  [
    "data iframe",
    "<!doctype html><html><body><iframe src='data:text/html,child'></iframe></body></html>",
  ],
]) {
  const secondaryDocument = structuredClone(request);
  secondaryDocument.html = html;
  await assert.rejects(
    renderDocument(secondaryDocument),
    (error) => error?.code === "secondary_document_denied",
    name,
  );
}

const shadowDOM = structuredClone(request);
shadowDOM.html = `<!doctype html><html><body><div id="host"></div><script>
  try { document.querySelector('#host').attachShadow({mode: 'closed'}); } catch {}
</script></body></html>`;
await assert.rejects(
  renderDocument(shadowDOM),
  (error) => error?.code === "content_policy_denied" || error?.code === "shadow_dom_denied",
);

const suppressedCSPEvent = structuredClone(request);
suppressedCSPEvent.html = `<!doctype html><html><body><script>
  window.addEventListener('securitypolicyviolation', (event) => event.stopImmediatePropagation(), true);
  const script = document.createElement('script');
  script.src = "data:text/javascript,document.body.dataset.executed='true'";
  document.body.append(script);
</script></body></html>`;
await assert.rejects(
  renderDocument(suppressedCSPEvent),
  (error) => error?.code === "content_policy_denied",
);

const javascriptNavigation = structuredClone(request);
javascriptNavigation.html = `<!doctype html><html><body>
  <a id="execute" href="javascript:document.body.dataset.executed='true'">execute</a>
  <script>document.querySelector('#execute').click()</script>
</body></html>`;
await assert.rejects(
  renderDocument(javascriptNavigation),
  (error) => error?.code === "content_policy_denied" || error?.code === "navigation_denied",
);

const directJavaScriptNavigation = structuredClone(request);
directJavaScriptNavigation.html = `<!doctype html><html><body><script>
  location.href = "javascript:document.body.dataset.executed='true'";
</script></body></html>`;
await assert.rejects(
  renderDocument(directJavaScriptNavigation),
  (error) => error?.code === "content_policy_denied" || error?.code === "navigation_denied",
);

const sourceURLJavaScriptNavigation = structuredClone(request);
sourceURLJavaScriptNavigation.html = `<!doctype html><html><body><script>
  setTimeout(() => {
    location.href = "javascript:document.body.dataset.executed%3D'true'%0A%2F%2F%23%20sourceURL%3Dhttps%3A%2F%2Frender.example.org%2Fapp";
  }, 0);
</script></body></html>`;
await assert.rejects(
  renderDocument(sourceURLJavaScriptNavigation),
  (error) => error?.code === "content_policy_denied" || error?.code === "navigation_denied",
);

const spoofedDOMCount = structuredClone(request);
spoofedDOMCount.limits.max_dom_nodes = 100;
spoofedDOMCount.html = `<!doctype html><html><body><script>
  document.getElementsByTagName = () => ({length: 1});
  for (let index = 0; index < 150; index += 1) {
    document.body.append(document.createElement('div'));
  }
  Object.defineProperty(Node.prototype, 'firstChild', {get: () => null});
  Object.defineProperty(Node.prototype, 'nextSibling', {get: () => null});
</script></body></html>`;
await assert.rejects(
  renderDocument(spoofedDOMCount),
  (error) => error?.code === "dom_node_limit",
);

const templateDOMCount = structuredClone(request);
templateDOMCount.limits.max_dom_nodes = 10;
templateDOMCount.html = `<!doctype html><html><body><template>${"<div></div>".repeat(150)}</template></body></html>`;
await assert.rejects(
  renderDocument(templateDOMCount),
  (error) => error?.code === "dom_node_limit",
);

const manyTemplates = structuredClone(request);
manyTemplates.limits.max_dom_nodes = 70000;
manyTemplates.limits.max_rendered_dom_bytes = 2 * 1024 * 1024;
manyTemplates.html = `<!doctype html><html><body>${"<template><div></div></template>".repeat(30000)}</body></html>`;
const manyTemplatesResult = await renderDocument(manyTemplates);
assert.equal(manyTemplatesResult.domNodes, 60003);

const rawTextDOMCount = structuredClone(request);
rawTextDOMCount.limits.max_dom_nodes = 10;
rawTextDOMCount.html = `<!doctype html><html><body><style id="host"></style><script>
  for (let index = 0; index < 150; index += 1) {
    document.querySelector('#host').append(document.createElement('div'));
  }
</script></body></html>`;
await assert.rejects(
  renderDocument(rawTextDOMCount),
  (error) => error?.code === "dom_node_limit",
);

const invalidNestingDOMCount = structuredClone(request);
invalidNestingDOMCount.limits.max_dom_nodes = 6;
invalidNestingDOMCount.html = `<!doctype html><html><body><p id="host"></p><script>
  document.querySelector('#host').append(document.createElement('div'));
</script></body></html>`;
const invalidNestingResult = await renderDocument(invalidNestingDOMCount);
assert.equal(invalidNestingResult.domNodes, 6);

const brokered = structuredClone(request);
brokered.mode = "brokered";
brokered.resource_hosts = {
  script: ["scripts.cdn.example.org"],
  stylesheet: ["styles.cdn.example.org"],
};
brokered.limits.max_resource_requests = 4;
brokered.limits.max_aggregate_resource_bytes = 4096;
brokered.limits.max_resource_body_bytes = 2048;
brokered.html = `<!doctype html><html><head>
  <link rel="stylesheet" href="https://styles.cdn.example.org/site.css">
</head><body><main id="styled">brokered fixture</main>
  <script src="https://scripts.cdn.example.org/app.js"></script>
  <script>
    document.body.dataset.styleApplied =
      getComputedStyle(document.querySelector('#styled')).color === 'rgb(1, 2, 3)' ? 'true' : 'false';
  </script>
</body></html>`;
const scriptBody = Buffer.from("document.body.dataset.externalScript = 'true';", "utf8");
const stylesheetBody = Buffer.from("#styled { color: rgb(1, 2, 3); }", "utf8");
const brokerCalls = [];
const brokeredResult = await renderDocument(brokered, async (intent) => {
  brokerCalls.push(intent);
  assert.equal(intent.method, "GET");
  if (intent.url === "https://scripts.cdn.example.org/app.js" && intent.resourceType === "script") {
    return { body: scriptBody, contentType: "application/javascript; charset=utf-8" };
  }
  if (intent.url === "https://styles.cdn.example.org/site.css" && intent.resourceType === "stylesheet") {
    return { body: stylesheetBody, contentType: "text/css; charset=utf-8" };
  }
  throw new Error("unexpected broker intent");
});
assert.match(brokeredResult.html, /data-external-script="true"/);
assert.match(brokeredResult.html, /data-style-applied="true"/);
assert.equal(brokeredResult.resourceRequests, 2);
assert.equal(brokeredResult.resourceBytes, scriptBody.length + stylesheetBody.length);
assert.equal(brokerCalls.length, 2);

const badScriptSRI = structuredClone(brokered);
badScriptSRI.html = `<!doctype html><html><body><main>must not be returned</main>
  <script src="https://scripts.cdn.example.org/sri.js"
    integrity="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="></script>
</body></html>`;
await assert.rejects(
  renderDocument(badScriptSRI, async () => ({
    body: Buffer.from("document.body.dataset.sriScript = 'executed';", "utf8"),
    contentType: "application/javascript; charset=utf-8",
  })),
  (error) => error?.code === "resource_load_failed" && error?.result?.html === undefined,
);

const badStylesheetSRI = structuredClone(brokered);
badStylesheetSRI.html = `<!doctype html><html><head>
  <link rel="stylesheet" href="https://styles.cdn.example.org/sri.css"
    integrity="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=">
</head><body><main>must not be returned</main></body></html>`;
await assert.rejects(
  renderDocument(badStylesheetSRI, async () => ({
    body: Buffer.from("body { color: rgb(1, 2, 3); }", "utf8"),
    contentType: "text/css; charset=utf-8",
  })),
  (error) => error?.code === "resource_load_failed" && error?.result?.html === undefined,
);

for (const [name, source] of [
  ["syntax", "const broken = ;"],
  ["runtime", "throw new Error('external runtime failure');"],
]) {
  const failedScript = structuredClone(brokered);
  failedScript.html = `<!doctype html><html><body><main>must not be returned</main>
    <script src="https://scripts.cdn.example.org/${name}.js"></script>
  </body></html>`;
  await assert.rejects(
    renderDocument(failedScript, async () => ({
      body: Buffer.from(source, "utf8"),
      contentType: "application/javascript; charset=utf-8",
    })),
    (error) => error?.code === "script_execution_failed" && error?.result?.html === undefined,
    `${name} failure must reject the whole render`,
  );
}

const failedModuleDependency = structuredClone(brokered);
failedModuleDependency.html = `<!doctype html><html><body><main>must not be returned</main>
  <script type="module" src="https://scripts.cdn.example.org/root-module.js"></script>
</body></html>`;
const rootModuleBody = Buffer.from(
  `import "https://scripts.cdn.example.org/failed-dependency.js";`,
  "utf8",
);
await assert.rejects(
  renderDocument(failedModuleDependency, async (intent) => {
    if (intent.url.endsWith("/root-module.js")) {
      return { body: rootModuleBody, contentType: "application/javascript; charset=utf-8" };
    }
    throw new Error("module dependency denied");
  }),
  (error) =>
    error?.code === "resource_denied" &&
    error?.result?.resourceRequests === 2 &&
    error?.result?.resourceBytes === rootModuleBody.length &&
    error?.result?.html === undefined,
);

const failedCSSImport = structuredClone(brokered);
failedCSSImport.html = `<!doctype html><html><head>
  <link rel="stylesheet" href="https://styles.cdn.example.org/root-import.css">
</head><body><main>must not be returned</main></body></html>`;
const rootCSSBody = Buffer.from(
  `@import url("https://styles.cdn.example.org/failed-import.css");`,
  "utf8",
);
await assert.rejects(
  renderDocument(failedCSSImport, async (intent) => {
    if (intent.url.endsWith("/root-import.css")) {
      return { body: rootCSSBody, contentType: "text/css; charset=utf-8" };
    }
    throw new Error("CSS import denied");
  }),
  (error) =>
    error?.code === "resource_denied" &&
    error?.result?.resourceRequests === 2 &&
    error?.result?.resourceBytes === rootCSSBody.length &&
    error?.result?.html === undefined,
);

for (let iteration = 0; iteration < 5; iteration += 1) {
  const delayedDynamicScript = structuredClone(brokered);
  delayedDynamicScript.limits.settle_time_ms = 100;
  delayedDynamicScript.html = `<!doctype html><html><body><script>
    setTimeout(() => {
      const script = document.createElement('script');
      script.src = 'https://scripts.cdn.example.org/delayed.js';
      document.body.append(script);
    }, 10);
  </script></body></html>`;
  let delayedCalls = 0;
  const delayedBody = Buffer.from(
    `document.body.dataset.delayedBrokerIteration = '${iteration}';`,
    "utf8",
  );
  const delayedResult = await renderDocument(delayedDynamicScript, async () => {
    delayedCalls += 1;
    await new Promise((resolve) => setTimeout(resolve, 25));
    return { body: delayedBody, contentType: "application/javascript; charset=utf-8" };
  });
  assert.match(delayedResult.html, new RegExp(`data-delayed-broker-iteration="${iteration}"`));
  assert.equal(delayedResult.resourceRequests, 1);
  assert.equal(delayedResult.resourceBytes, delayedBody.length);
  assert.equal(delayedCalls, 1);
}

const deniedResource = structuredClone(brokered);
deniedResource.resource_hosts.stylesheet = [];
deniedResource.html = "<!doctype html><html><body><script src='https://scripts.cdn.example.org/denied.js'></script></body></html>";
let deniedBrokerCalls = 0;
await assert.rejects(
  renderDocument(deniedResource, async () => {
    deniedBrokerCalls += 1;
    throw new Error("denied by fake broker");
  }),
  (error) => error?.code === "resource_denied",
);
assert.equal(deniedBrokerCalls, 1);

const concurrentFirstDenial = structuredClone(brokered);
concurrentFirstDenial.html = `<!doctype html><html><body>
  <script async src="https://scripts.cdn.example.org/first.js"></script>
  <script async src="https://scripts.cdn.example.org/queued.js"></script>
</body></html>`;
let concurrentDeniedCalls = 0;
await assert.rejects(
  renderDocument(concurrentFirstDenial, async () => {
    concurrentDeniedCalls += 1;
    await new Promise((resolve) => setTimeout(resolve, 25));
    throw new Error("first intent denied");
  }),
  (error) =>
    error?.code === "resource_denied" &&
    error?.result?.resourceRequests === 1 &&
    error?.result?.resourceBytes === 0,
);
assert.equal(concurrentDeniedCalls, 1);

const requestLimited = structuredClone(brokered);
requestLimited.limits.max_resource_requests = 1;
requestLimited.limits.max_render_time_ms = 5000;
requestLimited.html = `<!doctype html><html><body><script>
  for (let index = 0; index < 256; index += 1) {
    const script = document.createElement('script');
    script.async = true;
    script.src = 'https://scripts.cdn.example.org/storm-' + index + '.js';
    document.head.append(script);
  }
</script></body></html>`;
const limitedBody = Buffer.from("void 0;", "utf8");
let limitedBrokerCalls = 0;
let requestStormGuard;
try {
  await assert.rejects(
    Promise.race([
      renderDocument(requestLimited, async () => {
        limitedBrokerCalls += 1;
        await new Promise((resolve) => setTimeout(resolve, 100));
        return { body: limitedBody, contentType: "application/javascript; charset=utf-8" };
      }),
      new Promise((_, reject) => {
        requestStormGuard = setTimeout(
          () => reject(new Error("supported-resource request storm did not terminate within its bounded window")),
          7000,
        );
      }),
    ]),
    (error) =>
      error?.code === "resource_request_limit" &&
      error?.result?.resourceRequests === 1 &&
      error?.result?.resourceBytes === limitedBody.length,
  );
} finally {
  clearTimeout(requestStormGuard);
}
assert.equal(limitedBrokerCalls, 1);

const acceptedReplyDeadline = structuredClone(brokered);
acceptedReplyDeadline.limits.max_render_time_ms = 5000;
acceptedReplyDeadline.html = `<!doctype html><html><body>
  <script src="https://scripts.cdn.example.org/deadline.js"></script>
</body></html>`;
const acceptedReplyBody = Buffer.from("document.body.dataset.deadline = 'too-late';", "utf8");
const acceptedReplyStartedAt = performance.now();
let acceptedReplyCalls = 0;
await assert.rejects(
  renderDocument(acceptedReplyDeadline, async () => {
    acceptedReplyCalls += 1;
    // Block Node's timer turn until just after the render deadline. The broker
    // promise then resolves first, making the reply valid and accepted before
    // ensureBeforeDeadline observes that the wall-time budget has elapsed.
    const remaining = Math.max(
      1,
      acceptedReplyStartedAt + acceptedReplyDeadline.limits.max_render_time_ms + 100 - performance.now(),
    );
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, remaining);
    return {
      body: acceptedReplyBody,
      contentType: "application/javascript; charset=utf-8",
    };
  }),
  (error) =>
    error?.code === "render_timeout" &&
    error?.result?.resourceRequests === 1 &&
    error?.result?.resourceBytes === acceptedReplyBody.length &&
    error?.result?.html === undefined,
);
assert.equal(acceptedReplyCalls, 1);

const consoleLimited = structuredClone(request);
consoleLimited.limits.max_console_bytes = 5;
consoleLimited.html = `<!doctype html><html><body><script>
  console.log('console overflow fixture');
</script></body></html>`;
await assert.rejects(
  renderDocument(consoleLimited),
  (error) =>
    error?.code === "console_limit" &&
    error?.result?.consoleBytes === consoleLimited.limits.max_console_bytes &&
    error?.result?.resourceRequests === 0,
);

const unsupportedImage = structuredClone(brokered);
unsupportedImage.html = "<!doctype html><html><body><img src='https://scripts.cdn.example.org/image.png'></body></html>";
let imageBrokerCalls = 0;
await assert.rejects(
  renderDocument(unsupportedImage, async () => {
    imageBrokerCalls += 1;
    throw new Error("image must not reach broker");
  }),
  (error) => error?.code === "content_policy_denied" || error?.code === "resource_request_denied",
);
assert.equal(imageBrokerCalls, 0);

const unsupportedFetch = structuredClone(brokered);
unsupportedFetch.html = `<!doctype html><html><body><script>
  fetch('https://scripts.cdn.example.org/data').catch(() => {});
</script></body></html>`;
let fetchBrokerCalls = 0;
await assert.rejects(
  renderDocument(unsupportedFetch, async () => {
    fetchBrokerCalls += 1;
    throw new Error("fetch must not reach broker");
  }),
  (error) => error?.code === "forbidden_api",
);
assert.equal(fetchBrokerCalls, 0);

console.log("networkless Chromium V2 inline and brokered allow/deny smoke tests passed");
