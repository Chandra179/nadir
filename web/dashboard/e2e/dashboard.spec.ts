import { test, expect, type Page, type Route } from "@playwright/test";

type SessionState = {
  id: string;
  title: string;
  turn_count: number;
};

const session = (id: string, turnCount: number): SessionState => ({
  id,
  title: "A chat",
  turn_count: turnCount,
});

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function installAPI(page: Page) {
  let sessions: SessionState[] = [];
  let turnCount = 0;
  let nextTurn = 1;
  let cancelledTurn = "";
  const editRequests: Array<Record<string, unknown>> = [];

  await page.route("**/api/v1/sessions", async (route) => {
    if (route.request().method() === "DELETE") {
      sessions = [];
      await json(route, { deleted: true });
      return;
    }
    await json(route, { enabled: true, sessions });
  });

  await page.route("**/api/v1/sessions/*", async (route) => {
    if (route.request().method() === "DELETE") {
      const id = route.request().url().split("/").pop();
      sessions = sessions.filter((item) => item.id !== id);
      await json(route, { deleted: true });
      return;
    }
    await json(route, { error: "not needed in this deterministic flow" }, 404);
  });

  await page.route("**/api/v1/documents/reset", async (route) => {
    await json(route, { deleted: true });
  });

  await page.route("**/api/v1/documents", async (route) => {
    await json(route, { processed: 1, skipped: 0, failed: 0, names: ["notes.md"] });
  });

  await page.route("**/api/v1/turns/*/cancel", async (route) => {
    cancelledTurn = route.request().url().split("/").at(-2) ?? "";
    await route.fulfill({ status: 204 });
  });

  await page.route("**/api/v1/turns", async (route) => {
    const body = JSON.parse(route.request().postData() ?? "{}") as Record<string, unknown>;
    const id = `turn-${nextTurn++}`;
    const sessionID = typeof body.session_id === "string" && body.session_id ? body.session_id : `session-${id}`;
    if (body.edit) editRequests.push(body);
    turnCount = body.edit ? 1 : turnCount + 1;
    sessions = [session(sessionID, turnCount)];
    await json(route, {
      query: body.query,
      session_id: sessionID,
      top_k: 5,
      generate: true,
      results: [{ file_path: "notes.md", line_start: 4, score: 0.9, text: "Grounded passage." }],
      count: 1,
      elapsed_ms: 10,
      from_cache: false,
      has_answer: false,
      turn_id: id,
      stream_url: `/api/v1/turns/${id}/events`,
      streaming: true,
    });
  });

  await page.route("**/api/v1/turns/*/events", async (route) => {
    const id = route.request().url().split("/").at(-2) ?? "";
    const answer = id === "turn-1" ? "first answer" : id === "turn-2" ? "second answer" : id === "turn-3" ? "edited answer" : "partial answer";
    const terminal = id !== "turn-4";
    const events = [`id: 1\nevent: token\ndata: ${answer}\n`];
    if (terminal) events.push("id: 2\nevent: done\ndata: 1\n");
    await route.fulfill({
      status: 200,
      headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" },
      body: `${events.join("\n")}\n`,
    });
  });

  return {
    editRequests,
    get cancelledTurn() {
      return cancelledTurn;
    },
  };
}

test.beforeEach(async ({ page }) => {
  page.on("dialog", (dialog) => dialog.accept());
});

test("runs the document, chat, edit-prune, and deletion workflows", async ({ page }) => {
  const api = await installAPI(page);
  await page.goto("/");

  await expect(page.getByText("Ask Nadir about your documents.")).toBeVisible();
  await page.locator('input[type="file"]').setInputFiles({
    name: "notes.md",
    mimeType: "text/markdown",
    buffer: Buffer.from("grounded test document"),
  });
  await expect(page.getByText("1 processed, 0 skipped, 0 failed.")).toBeVisible();

  const composer = page.getByPlaceholder("Ask a question…");
  await composer.fill("first question");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("first answer")).toBeVisible();

  await composer.fill("second question");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("second answer")).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).first().click();
  await expect(page.getByText("Editing turn 1; later turns will be replaced.")).toBeVisible();
  await expect(composer).toHaveValue("first question");
  await composer.fill("edited question");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("edited answer")).toBeVisible();
  await expect(page.getByText("second question")).not.toBeVisible();
  expect(api.editRequests).toHaveLength(1);
  expect(api.editRequests[0]).toMatchObject({ edit: true, edit_sequence: 0, session_id: "session-turn-1" });

  await page.getByRole("button", { name: "Delete A chat" }).click();
  await expect(page.getByText("A chat")).not.toBeVisible();

  await page.getByRole("button", { name: "Start a new chat" }).click();
  await composer.fill("replacement question");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("partial answer")).toBeVisible();

  await page.getByRole("button", { name: "Stop" }).click();
  await expect.poll(() => api.cancelledTurn).toBe("turn-4");
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Delete all conversations" }).click();
  await expect(page.getByText("A chat")).not.toBeVisible();

  await page.getByRole("button", { name: "Reset document index" }).click();
  await expect(page.getByText("Document index reset.")).toBeVisible();
});
