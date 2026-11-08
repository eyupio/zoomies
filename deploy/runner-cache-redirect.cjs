'use strict';
// Sends the Actions cache's calls to the host's cache service, and nothing else.
//
// deploy/runner-node-wrapper.sh loads this into every JavaScript action when the
// agent has given the runner the address of its host's cache service
// (ZOOMIES_CACHE_SERVICE_URL). It cannot do that by changing
// ACTIONS_RESULTS_URL, which is the obvious thing: the same address is where
// upload-artifact and download-artifact find the artifact service, and a cache
// service that is sent artifact calls fails every job that stores an artifact.
// So the address is left alone and this rewrites a request only when it is
// addressed to the cache service by name, the Twirp service the toolkit's cache
// client calls. Whatever else the action sends goes where it always did.
//
// A request that cannot be rewritten is sent unchanged, and one that is
// rewritten to a service that is not there fails like any other cache call,
// which the toolkit reports as a miss and a slower build.

const http = require('http');
const https = require('https');

const CACHE_SERVICE = '/twirp/github.actions.results.api.v1.CacheService/';

let target = null;
try {
  const url = new URL(process.env.ZOOMIES_CACHE_SERVICE_URL || '');
  if (url.protocol === 'http:' || url.protocol === 'https:') target = url;
} catch (_) {
  // No usable address: leave every request alone.
}

if (target) {
  const targetLib = target.protocol === 'https:' ? https : http;
  const send = targetLib.request;

  // The calls the toolkit makes are request(options, callback), but the other
  // shapes Node accepts are rewritten the same way.
  const rewrite = (args) => {
    let [input, options, callback] = args;
    if (typeof options === 'function') {
      callback = options;
      options = undefined;
    }
    let o;
    if (typeof input === 'string' || input instanceof URL) {
      const u = new URL(input);
      o = { protocol: u.protocol, hostname: u.hostname, port: u.port, path: u.pathname + u.search, ...options };
    } else {
      o = { ...input, ...options };
    }
    if (typeof o.path !== 'string' || !o.path.startsWith(CACHE_SERVICE)) return null;

    delete o.agent;
    delete o.host;
    delete o.servername;
    o.protocol = target.protocol;
    o.hostname = target.hostname;
    o.port = target.port || (target.protocol === 'https:' ? 443 : 80);
    if (o.headers) {
      // The host header names the service the request was meant for.
      o.headers = Object.fromEntries(Object.entries(o.headers).filter(([k]) => k.toLowerCase() !== 'host'));
    }
    return [o, callback];
  };

  for (const mod of [http, https]) {
    for (const name of ['request', 'get']) {
      const original = mod[name];
      mod[name] = function (...args) {
        let redirected = null;
        try {
          redirected = rewrite(args);
        } catch (_) {
          // Fall through to the request as it was made.
        }
        if (!redirected) return original.apply(this, args);
        const req = send(redirected[0], redirected[1]);
        if (name === 'get') req.end();
        return req;
      };
    }
  }

  if (typeof globalThis.fetch === 'function') {
    const original = globalThis.fetch;
    globalThis.fetch = function (input, init) {
      try {
        const asUrl = typeof input === 'string' || input instanceof URL;
        const u = new URL(asUrl ? input : input.url);
        if (u.pathname.startsWith(CACHE_SERVICE)) {
          u.protocol = target.protocol;
          u.host = target.host;
          input = asUrl ? u.href : new Request(u.href, input);
        }
      } catch (_) {
        // Not a URL this can read: leave it.
      }
      return original.call(this, input, init);
    };
  }
}
