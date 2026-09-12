import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import WorkspacePage from "./WorkspacePage";

const jsonResponse = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status,
  headers: { "Content-Type": "application/json" },
});

describe("WorkspacePage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("shows the submitted question while retrieval is still pending", async () => {
    const user = userEvent.setup();
    let resolveTurn: (response: Response) => void = () => {};
    const turnResponse = new Promise<Response>((resolve) => { resolveTurn = resolve; });
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/sessions")) {
        return Promise.resolve(jsonResponse({ enabled: true, sessions: [] }));
      }
      if (url.endsWith("/api/v1/turns")) return turnResponse;
      return Promise.resolve(jsonResponse({ error: "unexpected request" }, 404));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<WorkspacePage />);
    const composer = await screen.findByPlaceholderText("Ask about your documents…");
    await user.type(composer, "What is the formula?");
    await user.click(screen.getByRole("button", { name: "Send message" }));

    expect(screen.getByText("What is the formula?")).toBeInTheDocument();
    expect(screen.getByText("Retrieving relevant passages…")).toBeInTheDocument();

    resolveTurn(jsonResponse({
      query: "What is the formula?",
      session_id: "session-1",
      top_k: 5,
      generate: true,
      results: [],
      count: 0,
      elapsed_ms: 10,
      from_cache: false,
      has_answer: false,
      streaming: false,
    }));
    await waitFor(() => expect(screen.queryByText("Retrieving relevant passages…")).not.toBeInTheDocument());
  });
});
