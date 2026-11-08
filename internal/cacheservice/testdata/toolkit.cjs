// Drives the real @actions/cache client against the service under test: save a
// folder, delete it, restore it, and check the bytes. toolkit_test.go starts
// the service and runs this; it is skipped unless the module is available.
//
// Environment: ZOOMIES_ACTIONS_CACHE_MODULE is the node_modules folder that
// holds @actions/cache; the rest is what a runner would set for a job.
const fs = require("fs");
const os = require("os");
const path = require("path");
const crypto = require("crypto");
const { createRequire } = require("module");

const req = createRequire(path.join(process.env.ZOOMIES_ACTIONS_CACHE_MODULE, "x.js"));
const cache = req("@actions/cache");

function fill(dir, megabytes) {
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "small.txt"), "hello from the cache\n");
  // Random bytes do not compress, so the archive really is this large.
  fs.writeFileSync(path.join(dir, "big.bin"), crypto.randomBytes(megabytes * 1024 * 1024));
}

function digest(dir) {
  const h = crypto.createHash("sha256");
  for (const f of fs.readdirSync(dir).sort()) {
    h.update(f);
    h.update(fs.readFileSync(path.join(dir, f)));
  }
  return h.digest("hex");
}

async function main() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "toolkit-"));
  process.env.RUNNER_TEMP = path.join(root, "runner-temp");
  fs.mkdirSync(process.env.RUNNER_TEMP, { recursive: true });
  const dir = path.join(root, "cached");
  const megabytes = parseInt(process.env.TOOLKIT_MEGABYTES || "8", 10);
  fill(dir, megabytes);
  const before = digest(dir);

  const key = "toolkit-" + Date.now();
  const saved = await cache.saveCache([dir], key);
  if (saved === -1) throw new Error("saveCache reported failure");

  fs.rmSync(dir, { recursive: true });
  const hit = await cache.restoreCache([dir], key);
  if (hit !== key) throw new Error("restoreCache by exact key returned " + hit);
  if (digest(dir) !== before) throw new Error("restored bytes differ from the saved bytes");

  fs.rmSync(dir, { recursive: true });
  const byPrefix = await cache.restoreCache([dir], key + "-newer", ["toolkit-"]);
  if (byPrefix !== key) throw new Error("restoreCache by restore key returned " + byPrefix);
  if (digest(dir) !== before) throw new Error("restore-key bytes differ");

  const miss = await cache.restoreCache([dir], "no-such-key");
  if (miss !== undefined) throw new Error("a missing key was reported as a hit: " + miss);
  console.log("toolkit ok: " + megabytes + " MiB saved, restored by key and by prefix, miss reported as a miss");
}

main().catch((e) => {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
