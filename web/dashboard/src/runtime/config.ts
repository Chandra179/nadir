import { DEFAULT_WEB_MODELS, type RuntimeConfig } from "./types";

export type ResolvedRuntime = { config: RuntimeConfig; notices: string[] };

/** What the browser's WebGPU offers. f16 gates the q4f16_1 model builds. */
export type GpuSupport = { available: boolean; f16: boolean };

/** Asks for a real adapter: `navigator.gpu` can exist while no GPU is usable
 *  (blocklisted driver, missing Vulkan, remote desktop). */
export async function probeGpu(nav: { gpu?: unknown } = navigator): Promise<GpuSupport> {
  const gpu = nav.gpu as { requestAdapter(): Promise<{ features: { has(f: string): boolean } } | null> } | undefined;
  if (!gpu) return { available: false, f16: false };
  try {
    const adapter = await gpu.requestAdapter();
    return adapter ? { available: true, f16: adapter.features.has("shader-f16") } : { available: false, f16: false };
  } catch {
    return { available: false, f16: false };
  }
}

/**
 * Chooses the transport. Returns null for the default server runtime.
 * The browser runtime is selected with VITE_RUNTIME=browser or ?runtime=browser;
 * ?models=mock swaps in scripted models (tests, offline demos), ?llm=none runs
 * retrieval-only, ?llm=<WebLLM model id> picks the generator, ?persist=0 keeps
 * everything in memory and ?store=<name> namespaces the stored corpus.
 */
export function resolveRuntime(
  search: string,
  env: { VITE_RUNTIME?: string; BASE_URL?: string; VITE_ORT_BASE_URL?: string },
  gpu: GpuSupport,
): ResolvedRuntime | null {
  const params = new URLSearchParams(search);
  const mode = params.get("runtime") ?? env.VITE_RUNTIME;
  if (mode !== "browser") return null;

  const base = env.BASE_URL ?? "/";
  const notices: string[] = [];
  const mock = params.get("models") === "mock";
  let models: RuntimeConfig["models"] = { kind: "mock" };
  if (!mock) {
    const web = { ...DEFAULT_WEB_MODELS };
    if (env.VITE_ORT_BASE_URL) web.ortBaseURL = env.VITE_ORT_BASE_URL;
    const llm = params.get("llm");
    if (llm === "none") delete web.llmModel;
    else if (llm) web.llmModel = llm;
    if (web.llmModel && !gpu.available) {
      delete web.llmModel;
      notices.push("No usable GPU (WebGPU) was found in this browser, so answers are disabled: search results only.");
    } else if (web.llmModel?.includes("q4f16") && !gpu.f16) {
      // This GPU cannot run 16-bit shaders; the 32-bit build of the same model can.
      web.llmModel = web.llmModel.replace("q4f16", "q4f32");
      notices.push("This GPU lacks 16-bit shader support, so the 32-bit model build is used.");
    }
    if (params.get("rerank") === "0") delete web.rerankerModel;
    models = web;
  }
  return {
    notices,
    config: {
      wasmURL: `${base}nadir/nadir.wasm`,
      wasmExecURL: `${base}nadir/wasm_exec.js`,
      models,
      persist: params.get("persist") !== "0",
      storageName: params.get("store") ?? "default",
    },
  };
}
