import { test, expect, type APIRequestContext } from "@playwright/test";

// This suite deliberately has no API mocks. It is opt-in because it mutates a
// configured Qdrant collection and needs the real Qdrant, Ollama, reranker,
// and (for the PDF case) Docling services.
const live = process.env.E2E_LIVE === "1";

async function requireReady(request: APIRequestContext) {
  const response = await request.get("/api/v1/ready");
  expect(response.status(), "full-stack dependencies must be ready").toBe(200);
}

test.describe("full-stack browser flows", () => {
  test("persists a generated session and replays its completed SSE stream", async ({ page, request }) => {
    test.skip(!live, "set E2E_LIVE=1 to run against Qdrant and Ollama");
    await requireReady(request);

    const deleteAll = await request.delete("/api/v1/sessions");
    expect(deleteAll.status()).toBe(200);

    await page.goto("/");
    const composer = page.getByPlaceholder("Ask about your documents…");
    await composer.fill("What is the secant formula?");
    await page.locator('button[aria-label="Send message"]').click();

    await expect(page.locator(".nadir-turn-a p").first()).toHaveText(/\S+/, { timeout: 180_000 });
    await expect(page).toHaveURL(/\/sessions\/[^/]+/);
    const sessionID = decodeURIComponent(new URL(page.url()).pathname.split("/").pop() ?? "");
    expect(sessionID).not.toBe("");

    const detailResponse = await request.get(`/api/v1/sessions/${encodeURIComponent(sessionID)}`);
    expect(detailResponse.status()).toBe(200);
    const detail = await detailResponse.json() as { turns: Array<{ turn_id?: string; query: string }> };
    expect(detail.turns).toHaveLength(1);
    expect(detail.turns[0].query).toBe("What is the secant formula?");
    expect(detail.turns[0].turn_id).toBeTruthy();

    const replay = await request.get(`/api/v1/turns/${detail.turns[0].turn_id}/events`, {
      headers: { "Last-Event-ID": "1" },
    });
    expect([200, 204]).toContain(replay.status());
    if (replay.status() === 200) expect(await replay.text()).toContain("event: done");

    await page.reload();
    await expect(page.getByText("What is the secant formula?", { exact: true })).toBeVisible();
    await expect(page.locator(".nadir-turn-a p").first()).toHaveText(/\S+/, { timeout: 30_000 });
  });

  test("indexes a PDF through the real Docling sidecar", async ({ request }) => {
    test.skip(!live || process.env.E2E_PDF !== "1", "set E2E_LIVE=1 and E2E_PDF=1 with Docling enabled");
    await requireReady(request);

    // Small one-page PDF payload. The sidecar, not the browser, owns parsing.
    const pdf = Buffer.from(
      "JVBERi0xLjQKMSAwIG9iago8PCAvVHlwZSAvQ2F0YWxvZyAvUGFnZXMgMiAwIFIgPj4KZW5kb2JqCjIgMCBvYmoKPDwgL1R5cGUgL1BhZ2VzIC9LaWRzIFszIDAgUl0gL0NvdW50IDEgPj4KZW5kb2JqCjMgMCBvYmoKPDwgL1R5cGUgL1BhZ2UgL1BhcmVudCAyIDAgUiAvTWVkaWFCb3ggWzAgMCA2MTIgNzkyXSAvUmVzb3VyY2VzIDQgMCBSIC9Db250ZW50cyA1IDAgUiA+PgplbmRvYmoKNCAwIG9iago8PCAvRm9udCA8PCAvRjEgNiAwIFIgPj4gPj4KZW5kb2JqCjUgMCBvYmoKPDwgL0xlbmd0aCA0NCA+PgpzdHJlYW0KQlQKL0YxIDI0IFRmCjEwMCA2MDAgVGQKKE5hZGlyIGxpdmUgdGVzdCkgVGoKRVQKZW5kc3RyZWFtCmVuZG9iago2IDAgb2JqCjw8IC9UeXBlIC9Gb250IC9TdWJ0eXBlIC9UeXBlMSAvQmFzZUZvbnQgL0hlbHZldGljYSA+PgplbmRvYmoKeHJlZgowIDcKMDAwMDAwMDAwMCA2NTUzNSBmIAowMDAwMDAwMDEwIDAwMDAwIG4gCjAwMDAwMDA2MyAwMDAwMCBuIAowMDAwMDAxMjIgMDAwMDAgbiAKMDAwMDAwMjQxIDAwMDAwIG4gCjAwMDAwMDMxMCAwMDAwMCBuIAowMDAwMDA0MDQgMDAwMDAgbiAKdHJhaWxlcgo8PCAvU2l6ZSA3IC9Sb290IDEgMCBSID4+CnN0YXJ0eHJlZQo0OTkKJSVFT0YK",
      "base64",
    );
    const response = await request.post("/api/v1/documents", {
      multipart: { files: { name: "nadir-live.pdf", mimeType: "application/pdf", buffer: pdf } },
    });
    expect(response.status()).toBe(200);
    const body = await response.json() as { processed: number; failed: number };
    expect(body).toMatchObject({ processed: 1, failed: 0 });
  });
});
