// Makes the kind of call upload-artifact and download-artifact make: a Twirp
// request to the artifact service, at the address the runner gave the action,
// with the toolkit's own HTTP client. The test asserts where it arrived.
const path = require("path");
const { createRequire } = require("module");
const req = createRequire(path.join(process.env.ZOOMIES_ACTIONS_CACHE_MODULE, "x.js"));
const { HttpClient } = req("@actions/http-client");
const { BearerCredentialHandler } = req("@actions/http-client/lib/auth");

async function main() {
  const client = new HttpClient("artifact-check", [new BearerCredentialHandler(process.env.ACTIONS_RUNTIME_TOKEN)]);
  const url = new URL("/twirp/github.actions.results.api.v1.ArtifactService/ListArtifacts", process.env.ACTIONS_RESULTS_URL).href;
  const res = await client.post(url, JSON.stringify({}), { "Content-Type": "application/json" });
  const body = await res.readBody();
  console.log("artifact call answered " + res.message.statusCode + " " + body);
  if (res.message.statusCode !== 200) process.exit(1);
}

main().catch((e) => {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
