import { cleanup, render, screen } from "@testing-library/react";
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
    expect(screen.queryByText(/Reordering retrieved chunks/)).not.toBeInTheDocument();
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
});
