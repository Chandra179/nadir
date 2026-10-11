import { existsSync } from "node:fs";
import { expect, test } from "@playwright/test";

// Runs the whole product in the page: the Go core as WASM in a worker, with
// scripted stand-ins for the models (no downloads, no GPU, no server). Needs
// `make wasm-build` first.
const wasmBuilt = existsSync("public/nadir/nadir.wasm");
const store = () => `e2e-${Date.now()}-${Math.random().toString(36).slice(2)}`;

test("uploads, answers from the uploaded document, and survives a reload", async ({ page, baseURL }) => {
  test.skip(!wasmBuilt, "run `make wasm-build` to build public/nadir/nadir.wasm");
  const name = store();
  const consoleErrors: string[] = [];
  page.on("pageerror", (error) => consoleErrors.push(error.message));
  // Nothing may reach a server: the API is entirely in-page.
  const apiRequests: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin === new URL(baseURL!).origin && url.pathname.startsWith("/api/")) apiRequests.push(request.url());
  });

  await page.goto(`/?runtime=browser&models=mock&store=${name}`);
  await expect(page.getByText("Ask your documents", { exact: true })).toBeVisible({ timeout: 30_000 });

  await page.getByRole("button", { name: "Add files" }).click();
  await page.locator('input[type="file"]').setInputFiles([
    { name: "space.md", mimeType: "text/markdown", buffer: Buffer.from("# Space\n\nRockets burn fuel to reach orbit around the planet.\n") },
    { name: "pets.md", mimeType: "text/markdown", buffer: Buffer.from("# Pets\n\nCats purr and sleep all day in the sun.\n") },
  ]);
  await expect(page.getByText("space.md", { exact: true }).first()).toBeVisible();

  const composer = page.getByPlaceholder("Ask about your documents…");
  await composer.fill("How do rockets reach orbit?");
  await page.getByRole("button", { name: "Send message" }).click();
  await expect(page.locator(".nadir-turn-a").first()).toContainText("Mock answer", { timeout: 20_000 });
  await expect(page.locator(".nadir-turn-a").first()).toContainText("Rockets");
  await expect(page.getByText("space.md").first()).toBeVisible();

  // Persistence: reload the tab and ask again without re-uploading.
  await page.waitForTimeout(1500); // debounce for the snapshot write
  await page.reload();
  await expect(page.getByText("Ask your documents", { exact: true }).or(page.getByText("How do rockets reach orbit?").first())).toBeVisible({ timeout: 30_000 });
  const again = page.getByPlaceholder("Ask about your documents…");
  await again.fill("Where do cats sleep?");
  await page.getByRole("button", { name: "Send message" }).click();
  await expect(page.locator(".nadir-turn-a").last()).toContainText("Cats", { timeout: 20_000 });

  expect(apiRequests, "the in-page runtime must not call a server").toEqual([]);
  expect(consoleErrors).toEqual([]);
});
