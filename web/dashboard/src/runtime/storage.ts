/** Snapshot persistence for the in-page store. */

export type Snapshot = { documents?: Uint8Array; history?: Uint8Array };

export interface SnapshotStore {
  load(): Promise<Snapshot>;
  save(snapshot: Required<Snapshot>): Promise<void>;
}

type SyncHandle = {
  getSize(): number;
  read(buffer: Uint8Array, options?: { at: number }): number;
  write(buffer: Uint8Array, options?: { at: number }): number;
  truncate(size: number): void;
  flush(): void;
  close(): void;
};
type FileHandleWithSync = FileSystemFileHandle & { createSyncAccessHandle(): Promise<SyncHandle> };

/** Stores snapshots in the origin-private file system. Sync access handles are
 *  only available in dedicated workers, which is where the runtime lives. */
export class OpfsSnapshotStore implements SnapshotStore {
  constructor(private name: string) {}

  private async directory(): Promise<FileSystemDirectoryHandle> {
    const root = await navigator.storage.getDirectory();
    return root.getDirectoryHandle(`nadir-${this.name}`, { create: true });
  }

  private async read(dir: FileSystemDirectoryHandle, file: string): Promise<Uint8Array | undefined> {
    let handle: FileHandleWithSync;
    try {
      handle = (await dir.getFileHandle(file)) as FileHandleWithSync;
    } catch {
      return undefined;
    }
    const access = await handle.createSyncAccessHandle();
    try {
      const data = new Uint8Array(access.getSize());
      access.read(data, { at: 0 });
      return data.length ? data : undefined;
    } finally {
      access.close();
    }
  }

  private async write(dir: FileSystemDirectoryHandle, file: string, data: Uint8Array): Promise<void> {
    const handle = (await dir.getFileHandle(file, { create: true })) as FileHandleWithSync;
    const access = await handle.createSyncAccessHandle();
    try {
      access.truncate(0);
      access.write(data, { at: 0 });
      access.flush();
    } finally {
      access.close();
    }
  }

  async load(): Promise<Snapshot> {
    const dir = await this.directory();
    return { documents: await this.read(dir, "documents.json"), history: await this.read(dir, "history.json") };
  }

  async save(snapshot: Required<Snapshot>): Promise<void> {
    const dir = await this.directory();
    await this.write(dir, "documents.json", snapshot.documents);
    await this.write(dir, "history.json", snapshot.history);
  }
}

export class MemorySnapshotStore implements SnapshotStore {
  private data: Snapshot = {};
  async load(): Promise<Snapshot> {
    return this.data;
  }
  async save(snapshot: Required<Snapshot>): Promise<void> {
    this.data = snapshot;
  }
}
