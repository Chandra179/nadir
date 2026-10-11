import { chromium, expect, test } from "@playwright/test";

// Opt-in: downloads real models (EmbeddingGemma-300m q8 ~300 MB, a MiniLM
// cross-encoder ~23 MB, onnxruntime-web wasm ~27 MB) on first run and keeps them
// in a persistent browser profile so later runs are fast. Retrieval only; the
// LLM needs WebGPU and is a separate, larger download.
//   E2E_REAL_MODELS=1 npx playwright test e2e/browser-runtime-real.spec.ts
const enabled = process.env.E2E_REAL_MODELS === "1";
const profile = process.env.E2E_PROFILE_DIR ?? "../../.local/pw-real-models-profile";

test("real embedder and reranker retrieve the right passage in the browser", async ({ baseURL }) => {
  test.skip(!enabled, "set E2E_REAL_MODELS=1 to download and run the real models");
  test.setTimeout(20 * 60_000);

  const context = await chromium.launchPersistentContext(profile, {
    executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH ?? "/usr/bin/google-chrome",
    headless: true,
  });
  try {
    const page = await context.newPage();
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
    context.on("response", (r) => r.status() === 404 && errors.push(`404 ${r.url()}`));
    const apiRequests: string[] = [];
    page.on("request", (r) => new URL(r.url()).origin === new URL(baseURL!).origin && new URL(r.url()).pathname.startsWith("/api/") && apiRequests.push(r.url()));

    await page.goto(`${baseURL}/?runtime=browser&llm=none&persist=0&store=real-${Date.now()}`);
    await expect(page.getByText("Ask your documents", { exact: true })).toBeVisible({ timeout: 15 * 60_000 });

    await page.getByRole("button", { name: "Add files" }).click();
    await page.locator('input[type="file"]').setInputFiles([
      { name: "space.md", mimeType: "text/markdown", buffer: Buffer.from("# Rockets\n\nA rocket reaches orbit by burning propellant to gain enough horizontal velocity to keep falling around the planet.\n") },
      { name: "pets.md", mimeType: "text/markdown", buffer: Buffer.from("# Cats\n\nCats are small carnivorous mammals that sleep for most of the day and purr when content.\n") },
      { name: "cooking.md", mimeType: "text/markdown", buffer: Buffer.from("# Bread\n\nSourdough bread rises slowly because wild yeast and bacteria ferment the dough over many hours.\n") },
    ]);
    await expect(page.getByText("cooking.md", { exact: true }).first()).toBeVisible({ timeout: 5 * 60_000 });

    // A paraphrase with almost no shared words: only a real embedding model can match it.
    const composer = page.getByPlaceholder("Ask about your documents…");
    await composer.fill("How do spacecraft stay up above the Earth?");
    await page.getByRole("button", { name: "Send message" }).click();
    // The sr-only region lists retrieved passages in rank order; the top one
    // must be the rocket passage even though the question shares no keywords.
    const passages = page.getByLabel("Retrieved passage text").locator("span");
    await expect(passages.first()).toBeVisible({ timeout: 3 * 60_000 });
    await expect(passages.first()).toContainText("propellant");
    expect(await passages.allTextContents()).not.toContain("");
    await page.screenshot({ path: "test-results/real-models.png" });

    expect(apiRequests, "no server calls").toEqual([]);
    const unexpected = errors.filter((e) => !/favicon|fonts\.googleapis|ERR_FAILED|status of 404/.test(e));
    expect(unexpected, unexpected.join("\n")).toEqual([]);
  } finally {
    await context.close();
  }
});
