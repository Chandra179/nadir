import { request } from "../../lib/http";
import type { IngestResponse } from "../../lib/api-contract";

export function resetDocuments(): Promise<void> {
  return request<void>("/api/v1/documents/reset", { method: "POST" });
}

export function ingestDocuments(files: File[]): Promise<IngestResponse> {
  const form = new FormData();
  for (const file of files) form.append("files", file);
  return request<IngestResponse>("/api/v1/documents", { method: "POST", body: form });
}
