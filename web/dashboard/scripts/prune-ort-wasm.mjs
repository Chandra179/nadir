// Cloudflare static assets reject files over 25 MiB, and onnxruntime-web's
// wasm (~27 MB) exceeds that. The runtime loads it from VITE_ORT_BASE_URL (or
// jsDelivr by default), so the bundled copy is unused and is removed from dist.
import { readdirSync, rmSync, statSync } from "node:fs";
import { join } from "node:path";

const dir = "dist/assets";
for (const name of readdirSync(dir)) {
  if (/^ort-wasm.*\.wasm$/.test(name)) {
    const size = statSync(join(dir, name)).size;
    rmSync(join(dir, name));
    console.log(`pruned ${name} (${(size / 1048576).toFixed(1)} MiB)`);
  }
}
