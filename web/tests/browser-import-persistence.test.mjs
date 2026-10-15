/* Standalone tests for the IndexedDB saved-import adapter in
   browser-import.js, run against a small in-memory fake IndexedDB.
   Run: node --test tests/browser-import-persistence.test.mjs */
import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const api = require("../browser-import.js");

/* ---- Minimal fake IndexedDB (open/upgrade, one store, get/put/delete,
   transaction complete/error events). Enough for the adapter surface. */
class FakeRequest extends EventTarget {
  constructor() {
    super();
    this.result = undefined;
    this.error = null;
  }
}

class FakeStore {
  constructor(db, tx) {
    this.db = db;
    this.tx = tx;
  }

  _request(op) {
    const request = new FakeRequest();
    queueMicrotask(() => {
      try {
        if (this.db.failWith) {
          const error = this.db.failWith;
          this.db.failWith = null;
          throw error;
        }
        request.result = op();
        request.dispatchEvent(new Event("success"));
        queueMicrotask(() => this.tx.dispatchEvent(new Event("complete")));
      } catch (error) {
        request.error = error;
        this.tx.error = error;
        request.dispatchEvent(new Event("error"));
        queueMicrotask(() => this.tx.dispatchEvent(new Event("error")));
      }
    });
    return request;
  }

  get(key) {
    return this._request(() => this.db.records.get(key));
  }

  put(value, key) {
    return this._request(() => {
      this.db.records.set(key, value);
      return key;
    });
  }

  delete(key) {
    return this._request(() => {
      this.db.records.delete(key);
      return undefined;
    });
  }
}

class FakeTransaction extends EventTarget {
  constructor(db) {
    super();
    this.db = db;
    this.error = null;
  }

  objectStore() {
    return new FakeStore(this.db, this);
  }
}

class FakeDb {
  constructor() {
    this.records = new Map();
    this.failWith = null;
    this.objectStoreNames = { contains: () => true };
  }

  createObjectStore() {}

  transaction() {
    return new FakeTransaction(this);
  }

  close() {}
}

function fakeIndexedDB(db) {
  return {
    open() {
      const request = new FakeRequest();
      queueMicrotask(() => {
        request.result = db;
        if (request.onupgradeneeded) request.onupgradeneeded();
        request.dispatchEvent(new Event("success"));
        if (request.onsuccess) request.onsuccess();
      });
      return request;
    },
  };
}

function checkpoint(overrides = {}) {
  return {
    schema: 1,
    checkpointId: 1,
    apiBase: "http://localhost:8080",
    generation: 1,
    started: false,
    tagsDraft: "",
    artistDraft: "",
    sourcePrefix: "folder-import",
    collectionId: null,
    duplicatePolicy: "skip",
    capturedDuplicatePolicy: "skip",
    capturedCollectionId: null,
    overflow: 0,
    entries: [
      {
        id: 0,
        name: "a.jpg",
        relativePath: "photos/a.jpg",
        size: 3,
        mime: "image/jpeg",
        lastModified: 7,
        hash: "sha256:" + "a".repeat(64),
        duplicatePostId: null,
        tags: ["cat"],
        source: "folder-import/photos/a.jpg",
        artist: "",
        phase: { kind: "hashed" },
      },
    ],
    ...overrides,
  };
}

function setup() {
  const db = new FakeDb();
  const persistence = api.createPersistence({ indexedDB: fakeIndexedDB(db), dbName: "test" });
  return { db, persistence };
}

test("save commits checkpoint and blob bytes atomically and probe summarizes it", async () => {
  const { persistence } = setup();
  const blob = new Blob(["abc"], { type: "image/jpeg" });
  await persistence.save(checkpoint(), [{ id: 0, blob }]);
  const probed = await persistence.probe();
  assert.equal(probed.checkpoint.apiBase, "http://localhost:8080");
  assert.equal(probed.blobs.length, 1);
  assert.equal(probed.blobs[0].blob.size, 3);
  assert.deepEqual(api.summaryOf(probed.checkpoint), {
    checkpointId: 1,
    fileCount: 1,
    totalBytes: 3,
    apiBase: "http://localhost:8080",
    started: false,
    generation: 1,
  });
});

test("secrets are refused and nothing is stored", async () => {
  const { persistence } = setup();
  const poisoned = checkpoint({ entries: [{ ...checkpoint().entries[0], credential: "hunter2" }] });
  await assert.rejects(() => persistence.save(poisoned, [{ id: 0, blob: new Blob(["abc"]) }]), /Refusing to save/);
  assert.equal(await persistence.probe(), null);
});

test("file-count and aggregate byte bounds are enforced before writing", async () => {
  const { persistence } = setup();
  const many = checkpoint({
    entries: Array.from({ length: 1001 }, (_, id) => ({ ...checkpoint().entries[0], id, phase: { kind: "imported", postId: "1" } })),
  });
  await assert.rejects(() => persistence.save(many, []), /at most 1000 files/);
  const huge = checkpoint({
    entries: [{ ...checkpoint().entries[0], size: 250 * 1024 * 1024 + 1, phase: { kind: "imported", postId: "1" } }],
  });
  await assert.rejects(() => persistence.save(huge, []), /250 MiB/);
  assert.equal(await persistence.probe(), null);
});

test("blob size mismatch is rejected", () => {
  const message = api.validateCheckpointForSave(checkpoint(), { 0: { size: 99 } });
  assert.match(message, /changed before it could be saved/);
});

test("quota failure leaves the previous saved import intact", async () => {
  const { db, persistence } = setup();
  await persistence.save(checkpoint(), [{ id: 0, blob: new Blob(["abc"]) }]);
  const quota = new Error("quota");
  quota.name = "QuotaExceededError";
  db.failWith = quota;
  await assert.rejects(
    () => persistence.save(checkpoint({ checkpointId: 2 }), [{ id: 0, blob: new Blob(["abc"]) }]),
    /quota/i,
  );
  const probed = await persistence.probe();
  assert.equal(probed.checkpoint.checkpointId, 1);
  assert.match(api.storageErrorMessage(quota), /quota exceeded/i);
});

test("corrupt or unsupported saved data is reported, not trusted", async () => {
  const { db, persistence } = setup();
  db.records.set("current", { schema: 1, checkpoint: checkpoint({ schema: 99 }), blobs: [] });
  await assert.rejects(() => persistence.probe(), /unsupported format/);
});

test("discard removes only the stored copy and probe then finds nothing", async () => {
  const { persistence } = setup();
  await persistence.save(checkpoint(), [{ id: 0, blob: new Blob(["abc"]) }]);
  await persistence.discard();
  assert.equal(await persistence.probe(), null);
  assert.equal(await persistence.load(), null);
});

test("unavailable IndexedDB fails honestly", async () => {
  const persistence = api.createPersistence({ indexedDB: null });
  await assert.rejects(() => persistence.probe(), /IndexedDB is unavailable/);
  await assert.rejects(() => persistence.save(checkpoint(), [{ id: 0, blob: new Blob(["abc"]) }]), /IndexedDB is unavailable/);
});
