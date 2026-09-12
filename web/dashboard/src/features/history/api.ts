import { request } from "../../lib/http";
import type { SessionDetail, SessionsResponse } from "../../lib/api-contract";

export function listSessions(): Promise<SessionsResponse> {
  return request<SessionsResponse>("/api/v1/sessions");
}

export function getSession(id: string): Promise<SessionDetail> {
  return request<SessionDetail>(`/api/v1/sessions/${encodeURIComponent(id)}`);
}

export function deleteSession(id: string): Promise<void> {
  return request<void>(`/api/v1/sessions/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function deleteAllSessions(): Promise<void> {
  return request<void>("/api/v1/sessions", { method: "DELETE" });
}
