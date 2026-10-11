import { chromium, expect, test } from "@playwright/test";

// Opt-in: runs the real WebLLM generator on the machine's GPU. Downloads the
// embedder and reranker (~330 MB) plus the LLM (~1.2 GB for the default) on the
// first run and keeps them in a persistent browser profile.
//   E2E_REAL_LLM=1 npx playwright test e2e/browser-runtime-llm.spec.ts
// Headless Chrome on Linux only exposes a GPU adapter through Vulkan. Not every
// driver exposes shader-f16, so the default model is the f32 build; set
// E2E_LLM_MODEL to a q4f16_1 build where it is available.
const enabled = process.env.E2E_REAL_LLM === "1";
const profile = process.env.E2E_PROFILE_DIR ?? "../../.local/pw-real-models-profile";
const model = process.env.E2E_LLM_MODEL ?? "Qwen2.5-1.5B-Instruct-q4f32_1-MLC";

test("real WebLLM answers from the retrieved passage and streams it", async ({ baseURL }) => {
  test.skip(!enabled, "set E2E_REAL_LLM=1 to download and run the real LLM");
  test.setTimeout(30 * 60_000);

  const context = await chromium.launchPersistentContext(profile, {
    executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH ?? "/usr/bin/google-chrome",
    headless: true,
    args: ["--enable-unsafe-webgpu", "--enable-features=Vulkan", "--use-angle=vulkan", "--ignore-gpu-blocklist"],
  });
  try {
    const page = await context.newPage();
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
    const apiRequests: string[] = [];
    page.on("request", (r) => new URL(r.url()).origin === new URL(baseURL!).origin && new URL(r.url()).pathname.startsWith("/api/") && apiRequests.push(r.url()));

    await page.goto(`${baseURL}/?runtime=browser&llm=${encodeURIComponent(model)}&persist=0&store=llm-${Date.now()}`);
    await expect(page.getByText("Ask your documents", { exact: true })).toBeVisible({ timeout: 25 * 60_000 });

    await page.getByRole("button", { name: "Add files" }).click();
    await page.locator('input[type="file"]').setInputFiles([
      { name: "probe.md", mimeType: "text/markdown", buffer: Buffer.from("# Zephyr-9\n\nThe Zephyr-9 probe keeps its attitude stable with 41 reaction wheels mounted around the main truss.\n") },
      { name: "pets.md", mimeType: "text/markdown", buffer: Buffer.from("# Cats\n\nCats are small carnivorous mammals that sleep for most of the day and purr when content.\n") },
    ]);
    await expect(page.getByText("pets.md", { exact: true }).first()).toBeVisible({ timeout: 5 * 60_000 });

    await page.getByPlaceholder("Ask about your documents…").fill("How many reaction wheels does the Zephyr-9 probe have?");
    await page.getByRole("button", { name: "Send message" }).click();

    // The number exists only in the uploaded file, so a correct answer shows
    // generation is grounded in the retrieved passage. The answer element also
    // holds screen-reader-only passage text, so assert on rendered text
    // (innerText) and only after the turn has finished streaming.
    const answer = page.locator(".nadir-turn-a").first();
    await expect(page.getByRole("button", { name: "Send message" })).toBeVisible({ timeout: 10 * 60_000 });
    const text = (await answer.innerText()).split("Sources")[0].trim();
    console.log(`LLM ${model} answered: ${text}`);
    expect(text.length, "answer streamed some text").toBeGreaterThan(10);
    expect(text).toContain("41");
    await page.screenshot({ path: "test-results/real-llm.png" });

    expect(apiRequests, "no server calls").toEqual([]);
    const unexpected = errors.filter((e) => !/favicon|fonts\.googleapis|ERR_FAILED|status of 404/.test(e));
    expect(unexpected, unexpected.join("\n")).toEqual([]);
  } finally {
    await context.close();
  }
});
