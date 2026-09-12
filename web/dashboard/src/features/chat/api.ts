import { request } from "../../lib/http";
import type { StartTurnRequest, Turn } from "../../lib/api-contract";

export function startTurn(body: StartTurnRequest): Promise<Turn> {
  return request<Turn>("/api/v1/turns", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function cancelTurn(id: string): Promise<void> {
  return request<void>(`/api/v1/turns/${encodeURIComponent(id)}/cancel`, { method: "POST" });
}
