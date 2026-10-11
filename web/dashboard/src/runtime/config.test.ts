import { describe, expect, it } from "vitest";
import { probeGpu, resolveRuntime, type GpuSupport } from "./config";

const full: GpuSupport = { available: true, f16: true };
const none: GpuSupport = { available: false, f16: false };

describe("resolveRuntime", () => {
  it("stays on the server runtime unless the browser runtime is requested", () => {
    expect(resolveRuntime("", {}, full)).toBeNull();
    expect(resolveRuntime("?runtime=server", { VITE_RUNTIME: "browser" }, full)).toBeNull();
  });

  it("selects mock models, persistence and storage namespace from the query", () => {
    const resolved = resolveRuntime("?runtime=browser&models=mock&persist=0&store=t1", { BASE_URL: "/app/" }, none)!;
    expect(resolved.config).toMatchObject({
      models: { kind: "mock" },
      persist: false,
      storageName: "t1",
      wasmURL: "/app/nadir/nadir.wasm",
    });
    expect(resolved.notices).toEqual([]);
  });

  it("drops the LLM with a notice when WebGPU is missing", () => {
    const resolved = resolveRuntime("?runtime=browser", {}, none)!;
    expect(resolved.config.models).toMatchObject({ kind: "web", embeddingDimensions: 768 });
    expect("llmModel" in resolved.config.models).toBe(false);
    expect(resolved.notices[0]).toMatch(/WebGPU/);
  });

  it("honours llm and rerank overrides", () => {
    const resolved = resolveRuntime("?runtime=browser&llm=Llama-3.2-1B-Instruct-q4f16_1-MLC&rerank=0", {}, full)!;
    expect(resolved.config.models).toMatchObject({ llmModel: "Llama-3.2-1B-Instruct-q4f16_1-MLC" });
    expect("rerankerModel" in resolved.config.models).toBe(false);
  });

  it("switches to the 32-bit model build when the GPU lacks shader-f16", () => {
    const resolved = resolveRuntime("?runtime=browser", {}, { available: true, f16: false })!;
    expect(resolved.config.models).toMatchObject({ llmModel: "Qwen2.5-1.5B-Instruct-q4f32_1-MLC" });
    expect(resolved.notices[0]).toMatch(/32-bit/);
  });
});

describe("probeGpu", () => {
  it("treats a missing adapter as no GPU even when navigator.gpu exists", async () => {
    expect(await probeGpu({ gpu: { requestAdapter: async () => null } })).toEqual(none);
    expect(await probeGpu({ gpu: { requestAdapter: async () => { throw new Error("x"); } } })).toEqual(none);
    expect(await probeGpu({})).toEqual(none);
  });

  it("reports shader-f16 support", async () => {
    const adapter = (f16: boolean) => ({ gpu: { requestAdapter: async () => ({ features: { has: () => f16 } }) } });
    expect(await probeGpu(adapter(true))).toEqual(full);
    expect(await probeGpu(adapter(false))).toEqual({ available: true, f16: false });
  });
});
