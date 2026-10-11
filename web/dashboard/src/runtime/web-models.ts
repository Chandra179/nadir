import { RpcClient } from "./rpc";
import type { Models, Progress, WebModelsConfig } from "./types";

/** Models served by the ml and llm workers over message ports. */
export class WebModels implements Models {
  readonly dimensions: number;
  readonly embeddingModel: string;
  readonly generatorModel?: string;
  readonly rerank?: Models["rerank"];
  readonly generate?: Models["generate"];
  readonly complete?: Models["complete"];

  constructor(
    private config: WebModelsConfig,
    private ml: RpcClient,
    private llm?: RpcClient,
  ) {
    this.dimensions = config.embeddingDimensions;
    this.embeddingModel = config.embeddingModel;
    this.generatorModel = config.llmModel;
    if (config.rerankerModel) {
      this.rerank = (query, passages) => this.ml.call<number[]>("rerank", [query, passages]);
    }
    if (llm && config.llmModel) {
      this.generate = async (prompt, signal, emit) => {
        await llm.call("generate", [prompt, { maxTokens: config.maxOutputTokens }], {
          signal,
          onEvent: (name, value) => name === "token" && emit(value as string),
        });
      };
      this.complete = (system, user) => llm.call<string>("complete", [system, user]);
    }
  }

  async warmup(onProgress: (p: Progress) => void): Promise<void> {
    const forward = (name: string, value: unknown) => name === "progress" && onProgress(value as Progress);
    await this.ml.call(
      "load",
      [
        {
          embeddingModel: this.config.embeddingModel,
          dtype: this.config.embeddingDtype,
          device: this.config.device ?? "wasm",
          rerankerModel: this.config.rerankerModel,
          ortBaseURL: this.config.ortBaseURL,
        },
      ],
      { onEvent: forward },
    );
    if (this.llm && this.config.llmModel) {
      await this.llm.call("load", [{ model: this.config.llmModel, contextWindow: this.config.contextWindow }], { onEvent: forward });
    }
  }

  embed(texts: string[]): Promise<Float32Array[]> {
    return this.ml.call<Float32Array[]>("embed", [texts]);
  }
}
