import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import TurnCard from "./TurnCard";
import type { Turn } from "../../lib/api-contract";

const turn: Turn = {
  query: "What is the formula?",
  session_id: "session-1",
  top_k: 5,
  generate: true,
  results: [{ file_path: "notes.md", line_start: 4, score: 0.9, text: "<script>not markup</script>" }],
  count: 1,
  elapsed_ms: 10,
  from_cache: false,
  answer: "The answer is grounded in the document.",
  has_answer: true,
  streaming: false,
};

describe("TurnCard", () => {
  afterEach(() => cleanup());

  it("shows retrieval progress while the turn request is pending", () => {
    render(
      <TurnCard
        turn={{ ...turn, answer: undefined, has_answer: false, results: [], count: 0 }}
        pending
        onEdit={vi.fn()}
      />,
    );

    expect(screen.getByText("What is the formula?")).toBeInTheDocument();
    expect(screen.getByText("Retrieving relevant passages…")).toBeInTheDocument();
    expect(screen.queryByText("No matching chunks.")).not.toBeInTheDocument();
  });

  it("hands the trace area to the answer after streaming starts", () => {
    render(
      <TurnCard
        turn={{ ...turn, answer: "Partial answer", streaming: true }}
        onEdit={vi.fn()}
      />,
    );

    expect(screen.getByText("Partial answer")).toBeInTheDocument();
    expect(screen.queryByText(/Selecting relevant evidence/)).not.toBeInTheDocument();
  });

  it("renders model text as text and copies the answer", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });

    render(<TurnCard turn={turn} onEdit={vi.fn()} />);

    expect(screen.getByText("<script>not markup</script>")).toBeInTheDocument();
    expect(document.querySelector("script")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith(turn.answer);
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("links inline citations to admitted evidence independently of retrieval rank", async () => {
    const user = userEvent.setup();
    render(<TurnCard turn={{
      ...turn,
      answer: "Use the admitted formula [2]. Unverified marker [7].",
      citations: [{ number: 2, retrieval_rank: 1, file_path: "formula.md", line_start: 12, chunk_index: 8, text: "x = <formula>\n[truncated]", truncated: true }],
    }} onEdit={vi.fn()} />);

    const link = screen.getByRole("link", { name: "Source 2: formula.md, line 12" });
    expect(link).toHaveTextContent("[2]");
    expect(screen.getAllByRole("link")).toHaveLength(1);
    const target = document.getElementById(link.getAttribute("href")!.slice(1));
    expect(target).toHaveTextContent("[2] formula.md · L12");
    expect(within(target!).getByText("x = <formula> [truncated]")).not.toBeVisible();
    await user.click(link);
    expect(within(target!).getByText("x = <formula> [truncated]")).toBeVisible();
    expect(within(target!).getByText("Retrieved #1 · Excerpt shortened")).toBeVisible();
    expect(screen.getByRole("button", { name: "[2] formula.md · L12" })).toHaveAttribute("aria-expanded", "true");
    expect(document.querySelector("formula")).toBeNull();
  });

  it("keeps legacy answer markers as text and offers the saved retrieved passages", async () => {
    const user = userEvent.setup();
    render(<TurnCard turn={{ ...turn, answer: "An old answer [1]." }} onEdit={vi.fn()} />);

    expect(screen.getByText("An old answer [1].")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    await user.click(screen.getByText("Retrieved passages"));
    expect(await screen.findByText("This saved answer has no citation mapping.")).toBeVisible();
    expect(screen.getByText("notes.md · L4")).toBeVisible();
  });

  it("keeps citation targets unique across turns and stable while streaming", () => {
    const cited = { ...turn, citations: [{ number: 1, retrieval_rank: 3, file_path: "one.md", line_start: 4, chunk_index: 0, text: "Evidence" }], answer: "Partial [1]", streaming: true };
    const { rerender } = render(<><TurnCard turn={cited} onEdit={vi.fn()} /><TurnCard turn={cited} onEdit={vi.fn()} /></>);
    const before = screen.getAllByRole("link").map((link) => link.getAttribute("href"));
    expect(new Set(before).size).toBe(2);
    rerender(<><TurnCard turn={{ ...cited, answer: "Complete answer [1]", streaming: false }} onEdit={vi.fn()} /><TurnCard turn={cited} onEdit={vi.fn()} /></>);
    expect(screen.getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual(before);
  });
});
