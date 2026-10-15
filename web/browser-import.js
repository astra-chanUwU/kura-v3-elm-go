/* Kura browser folder-import bridge. Browser/stdlib only.
   File values stay in memory; nothing here writes storage, URLs, or logs.
   Only one picked preview is retained, generation-tagged: a stale or
   cancelled picker can never replace the files behind the preview the
   user sees, and released entries drop their File references at once. */
(function (global) {
  "use strict";

  var MAX_BYTES = 25 * 1024 * 1024;
  var ACCEPT_MIMES = ["image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "video/mp4", "video/webm"];
  var ACCEPT_EXTS = [".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".mp4", ".webm"];

  function isValidHash(value) {
    return typeof value === "string" && /^sha256:[0-9a-f]{64}$/.test(value);
  }

  function fileFact(file, path) {
    return {
      file: file,
      name: file.name || "",
      path: path || file.name || "",
      size: file.size || 0,
      type: file.type || "",
      lastModified: file.lastModified || 0
    };
  }

  function retainable(file) {
    if (!file || !(file.size > 0) || file.size > MAX_BYTES) return false;
    var mime = String(file.type || "").trim().toLowerCase();
    if (mime) return ACCEPT_MIMES.indexOf(mime) !== -1;
    var name = String(file.name || "").toLowerCase();
    return ACCEPT_EXTS.some(function (ext) { return name.endsWith(ext); });
  }

  function validateHashRequest(entryId, file) {
    if (typeof entryId !== "number") return "missing entry id";
    if (!file || typeof file.arrayBuffer !== "function") return "missing file";
    if (!(file.size > 0)) return "The file is empty.";
    if (file.size > MAX_BYTES) return "The file exceeds 25 MiB.";
    if (!global.crypto || !global.crypto.subtle || typeof global.crypto.subtle.digest !== "function") {
      return "Secure-context SHA-256 is unavailable in this browser.";
    }
    return null;
  }

  function toHex(buffer) {
    return Array.prototype.map
      .call(new Uint8Array(buffer), function (b) {
        return ("0" + b.toString(16)).slice(-2);
      })
      .join("");
  }

  function wire(app) {
    if (!app || !app.ports) return;
    var ports = app.ports;
    if (!ports.importPickRequested || !ports.importFilesPicked) return;

    var folderInput = document.createElement("input");
    folderInput.type = "file";
    folderInput.multiple = true;
    folderInput.setAttribute("webkitdirectory", "");
    folderInput.style.display = "none";

    var filesInput = document.createElement("input");
    filesInput.type = "file";
    filesInput.multiple = true;
    filesInput.accept = "image/jpeg,image/png,image/gif,image/webp,image/avif,video/mp4,video/webm,.avif,.mp4,.webm";
    filesInput.style.display = "none";
    document.body.appendChild(folderInput);
    document.body.appendChild(filesInput);

    var currentFiles = [];
    var activeGeneration = null;
    var pickerOpen = false;
    var pickerGeneration = null;
    var focusTimer = null;

    function clearFocusTimer() {
      if (focusTimer !== null) { clearTimeout(focusTimer); focusTimer = null; }
    }

    function notifyCancelled() {
      var generation = pickerGeneration;
      pickerOpen = false;
      pickerGeneration = null;
      clearFocusTimer();
      if (generation !== null && ports.importPickCancelled) {
        ports.importPickCancelled.send(generation);
      }
    }

    function sendFiles(input) {
      if (!pickerOpen) return;
      var generation = pickerGeneration;
      pickerOpen = false;
      pickerGeneration = null;
      clearFocusTimer();
      var all = Array.prototype.slice.call(input.files || []);
      var picked = all.map(function (file) {
        return fileFact(file, file.webkitRelativePath || file.name || "");
      });
      input.value = "";
      if (!picked.length) {
        if (ports.importPickCancelled) ports.importPickCancelled.send(generation);
        return;
      }
      activeGeneration = generation;
      currentFiles = all.map(function (file) { return retainable(file) ? file : null; });
      ports.importFilesPicked.send({ generation: generation, files: picked });
    }
    folderInput.addEventListener("change", function () { sendFiles(folderInput); });
    filesInput.addEventListener("change", function () { sendFiles(filesInput); });
    folderInput.addEventListener("cancel", notifyCancelled);
    filesInput.addEventListener("cancel", notifyCancelled);
    if (typeof global.addEventListener === "function") {
      global.addEventListener("focus", function () {
        if (!pickerOpen) return;
        clearFocusTimer();
        focusTimer = setTimeout(function () {
          focusTimer = null;
          if (pickerOpen) notifyCancelled();
        }, 350);
      });
    }

    ports.importPickRequested.subscribe(function (request) {
      if (pickerOpen) return;
      var mode = request && request.mode;
      var generation = request && request.generation;
      if (typeof generation !== "number") return;
      pickerOpen = true;
      pickerGeneration = generation;
      if (mode === "folder") folderInput.click();
      else filesInput.click();
    });

    if (ports.importFilesReleased) {
      ports.importFilesReleased.subscribe(function (request) {
        if (!request || request.generation !== activeGeneration) return;
        var ids = Array.isArray(request.ids) ? request.ids : [];
        ids.forEach(function (id) {
          if (typeof id === "number" && id >= 0 && id < currentFiles.length) currentFiles[id] = null;
        });
        if (currentFiles.every(function (file) { return !file; })) {
          currentFiles = [];
          activeGeneration = null;
        }
      });
    }

    if (ports.importHashRequested && ports.importHashCompleted) {
      ports.importHashRequested.subscribe(function (request) {
        var entryId = request && request.entryId;
        var generation = request && request.generation;
        function fail(message) {
          ports.importHashCompleted.send({ entryId: typeof entryId === "number" ? entryId : 0, generation: typeof generation === "number" ? generation : -1, error: message });
        }
        if (typeof generation !== "number" || generation !== activeGeneration) {
          fail("This file is no longer part of the current preview.");
          return;
        }
        var file = currentFiles[entryId];
        var invalid = validateHashRequest(entryId, file);
        if (invalid) { fail(invalid); return; }
        var sizeBefore = file.size;
        var modifiedBefore = file.lastModified || 0;
        file
          .arrayBuffer()
          .then(function (buffer) { return global.crypto.subtle.digest("SHA-256", buffer); })
          .then(function (digest) {
            if (generation !== activeGeneration || currentFiles[entryId] !== file) {
              fail("This file is no longer part of the current preview.");
              return;
            }
            if (file.size !== sizeBefore || (file.lastModified || 0) !== modifiedBefore) {
              ports.importHashCompleted.send({
                entryId: entryId,
                generation: generation,
                error: "This file changed while it was being hashed. Pick it again before importing."
              });
              return;
            }
            var hash = "sha256:" + toHex(digest);
            if (!isValidHash(hash)) throw new Error("invalid hash");
            ports.importHashCompleted.send({
              entryId: entryId,
              generation: generation,
              hash: hash,
              size: file.size,
              lastModified: file.lastModified || 0
            });
          })
          .catch(function () {
            ports.importHashCompleted.send({
              entryId: typeof entryId === "number" ? entryId : 0,
              generation: generation,
              error: "This file could not be hashed in the browser."
            });
          });
      });
    }

    wirePersistence(
      app,
      function () {
        var snapshot = { generation: activeGeneration };
        currentFiles.forEach(function (file, index) { if (file) snapshot[index] = file; });
        return snapshot;
      },
      function (generation, verified) {
        if (generation === null || generation === undefined) {
          currentFiles = [];
          activeGeneration = null;
          return;
        }
        activeGeneration = generation;
        currentFiles = [];
        (verified || []).forEach(function (item) {
          if (item && item.file) currentFiles[item.id] = item.file;
        });
      }
    );
  }

  /* ---- Durable saved-import persistence (opt-in, IndexedDB) ----
     One saved import per origin. A single record {checkpoint, blobs}
     is written in one transaction, so plain state and file bytes can
     never be half-saved. Nothing is reported saved until the
     transaction commits. Credentials, tokens, access state, secret
     headers, and object URLs are rejected, never stored. */

  var PERSIST_SCHEMA = 1;
  var MAX_FILES = 1000;
  var MAX_TOTAL_BYTES = 250 * 1024 * 1024;
  var DB_NAME = "kura-browser-import";
  var STORE_NAME = "checkpoints";
  var RECORD_KEY = "current";
  var FORBIDDEN_KEY = /credential|token|secret|authorization|password|api[-_]?key|objecturl|access[-_]?key/i;
  var BLOB_PHASES = { pendingHash: true, hashing: true, hashed: true, queued: true, importing: true };

  function assertNoSecrets(value, seen) {
    if (!value || typeof value !== "object") return;
    if (seen && seen.indexOf(value) !== -1) return;
    var next = (seen || []).concat([value]);
    Object.keys(value).forEach(function (key) {
      if (FORBIDDEN_KEY.test(key)) throw new Error("Refusing to save import data containing “" + key + "”.");
      assertNoSecrets(value[key], next);
    });
  }

  function checkpointTotals(checkpoint) {
    var entries = (checkpoint && checkpoint.entries) || [];
    var total = 0;
    entries.forEach(function (entry) { total += entry.size || 0; });
    return { fileCount: entries.length, totalBytes: total };
  }

  function summaryOf(checkpoint) {
    var totals = checkpointTotals(checkpoint);
    return {
      checkpointId: checkpoint.checkpointId,
      fileCount: totals.fileCount,
      totalBytes: totals.totalBytes,
      apiBase: checkpoint.apiBase,
      started: !!checkpoint.started,
      generation: checkpoint.generation
    };
  }

  function validateCheckpointForSave(checkpoint, blobsById) {
    if (!checkpoint || checkpoint.schema !== PERSIST_SCHEMA) return "The import checkpoint format is not supported.";
    if (!Array.isArray(checkpoint.entries)) return "The import checkpoint is damaged.";
    if (checkpoint.entries.length > MAX_FILES) return "A saved import may contain at most 1000 files.";
    if (checkpointTotals(checkpoint).totalBytes > MAX_TOTAL_BYTES) return "A saved import may be at most 250 MiB in total.";
    try { assertNoSecrets(checkpoint); } catch (error) { return error.message; }
    for (var i = 0; i < checkpoint.entries.length; i += 1) {
      var entry = checkpoint.entries[i];
      var kind = entry.phase && entry.phase.kind;
      if (BLOB_PHASES[kind]) {
        var blob = blobsById && blobsById[entry.id];
        if (!blob) return "The file “" + (entry.relativePath || entry.name || "") + "” is no longer available to save.";
        if (blob.size !== entry.size) return "The file “" + (entry.relativePath || entry.name || "") + "” changed before it could be saved.";
      }
    }
    return null;
  }

  function storageErrorMessage(error) {
    if (!error) return "Browser storage is unavailable.";
    if (error.name === "QuotaExceededError") return "Browser storage quota exceeded. Free space or discard the saved import.";
    if (error.name === "InvalidStateError" || error.name === "UnknownError") return "Browser storage is unavailable or damaged.";
    return error.message || "Browser storage is unavailable.";
  }

  function createPersistence(options) {
    var opts = options || {};
    var factory = opts.indexedDB || null;
    var dbName = opts.dbName || DB_NAME;
    var dbPromise = null;

    function unavailable() {
      return Promise.reject(new Error("IndexedDB is unavailable in this browser, so imports cannot be kept for reload."));
    }

    function openDb() {
      if (!factory || typeof factory.open !== "function") return unavailable();
      if (!dbPromise) {
        dbPromise = new Promise(function (resolve, reject) {
          var request;
          try { request = factory.open(dbName, 1); } catch (error) { reject(error); return; }
          request.onupgradeneeded = function () {
            var db = request.result;
            if (db && db.objectStoreNames && !db.objectStoreNames.contains(STORE_NAME)) db.createObjectStore(STORE_NAME);
            else if (db && !db.objectStoreNames) db.createObjectStore(STORE_NAME);
          };
          request.onsuccess = function () { resolve(request.result); };
          request.onerror = function () { reject(request.error || new Error("Browser storage could not be opened.")); };
          request.onblocked = function () { reject(new Error("Browser storage is blocked by another tab. Close other Kura tabs and try again.")); };
        });
        dbPromise.catch(function () { dbPromise = null; });
      }
      return dbPromise;
    }

    function run(mode, operation) {
      return openDb().then(function (db) {
        return new Promise(function (resolve, reject) {
          var tx;
          try { tx = db.transaction(STORE_NAME, mode); } catch (error) { reject(error); return; }
          var result;
          var request = operation(tx.objectStore(STORE_NAME));
          if (request) {
            if (typeof request.addEventListener === "function") {
              request.addEventListener("success", function () { result = request.result; });
              request.addEventListener("error", function () { reject(request.error || new Error("Browser storage operation failed.")); });
            } else {
              request.onsuccess = function () { result = request.result; };
              request.onerror = function () { reject(request.error || new Error("Browser storage operation failed.")); };
            }
          }
          function done() { resolve(result); }
          function failed() { reject((tx && tx.error) || new Error("Browser storage operation failed.")); }
          if (typeof tx.addEventListener === "function") {
            tx.addEventListener("complete", done);
            tx.addEventListener("error", failed);
            tx.addEventListener("abort", failed);
          } else {
            tx.oncomplete = done;
            tx.onerror = failed;
            tx.onabort = failed;
          }
        });
      });
    }

    return {
      probe: function () {
        return run("readonly", function (store) { return store.get(RECORD_KEY); }).then(function (record) {
          if (!record || !record.checkpoint) return null;
          if (record.checkpoint.schema !== PERSIST_SCHEMA) throw new Error("The saved import uses an unsupported format.");
          return record;
        });
      },
      save: function (checkpoint, blobs) {
        var blobsById = {};
        (blobs || []).forEach(function (item) { blobsById[item.id] = item.blob; });
        var invalid = validateCheckpointForSave(checkpoint, blobsById);
        if (invalid) return Promise.reject(new Error(invalid));
        var record = { schema: PERSIST_SCHEMA, checkpoint: checkpoint, blobs: blobs || [] };
        // Resolve only on transaction commit: never report durable early.
        return run("readwrite", function (store) { return store.put(record, RECORD_KEY); }).then(function () { return record; });
      },
      load: function () {
        return run("readonly", function (store) { return store.get(RECORD_KEY); }).then(function (record) {
          if (!record || !record.checkpoint || !Array.isArray(record.blobs)) return null;
          if (record.checkpoint.schema !== PERSIST_SCHEMA) throw new Error("The saved import uses an unsupported format.");
          return record;
        });
      },
      discard: function () {
        return run("readwrite", function (store) { return store.delete(RECORD_KEY); }).then(function () { return null; });
      },
      close: function () {
        if (dbPromise) {
          var pending = dbPromise;
          dbPromise = null;
          return pending.then(function (db) { if (db && typeof db.close === "function") db.close(); }, function () {});
        }
        return Promise.resolve();
      }
    };
  }

  function wirePersistence(app, getFiles, setRestoredFiles) {
    var ports = app.ports;
    if (!ports.importPersistenceRequested || !ports.importPersistenceEvent) return;
    var persistence = createPersistence({ indexedDB: global.indexedDB || null });
    var epoch = 0;
    var savedGeneration = null;

    function send(value) { ports.importPersistenceEvent.send(value); }

    persistence.probe().then(function (record) {
      if (!record) { send({ kind: "probe", found: false }); return; }
      savedGeneration = record.checkpoint.generation;
      send({ kind: "probe", found: true, summary: summaryOf(record.checkpoint) });
    }).catch(function (error) {
      send({ kind: "probeFailed", message: storageErrorMessage(error) });
    });

    ports.importPersistenceRequested.subscribe(function (request) {
      if (!request || typeof request.action !== "string") return;
      if (request.action === "save") {
        var saveEpoch = epoch;
        var checkpoint = request.checkpoint;
        var entryIds = Array.isArray(request.entryIds) ? request.entryIds : [];
        var files = getFiles();
        var blobs = [];
        var missing = null;
        entryIds.forEach(function (id) {
          var file = files[id];
          if (!file) missing = id;
          else blobs.push({ id: id, blob: file });
        });
        if (missing !== null || !checkpoint) {
          send({ kind: "storeFailed", checkpointId: request.checkpointId || 0, message: "A picked file is no longer available to save." });
          return;
        }
        persistence.save(checkpoint, blobs).then(function () {
          if (saveEpoch !== epoch) return;
          savedGeneration = checkpoint.generation;
          send({ kind: "stored", checkpointId: request.checkpointId });
        }).catch(function (error) {
          if (saveEpoch !== epoch) return;
          send({ kind: "storeFailed", checkpointId: request.checkpointId || 0, message: storageErrorMessage(error) });
        });
      } else if (request.action === "restore") {
        var restoreEpoch = epoch;
        persistence.load().then(function (record) {
          if (!record) { send({ kind: "restoreFailed", message: "There is no saved import in this browser." }); return null; }
          var cp = record.checkpoint;
          var blobsById = {};
          record.blobs.forEach(function (item) { blobsById[item.id] = item.blob; });
          var jobs = cp.entries.map(function (entry) {
            var kind = entry.phase && entry.phase.kind;
            var blob = blobsById[entry.id];
            if (!BLOB_PHASES[kind] || !blob) return Promise.resolve({ id: entry.id, file: null, hasBlob: false, hashVerified: false });
            var file = blob;
            try { file = new File([blob], entry.name || "file", { type: entry.mime || blob.type || "", lastModified: entry.lastModified || 0 }); } catch (error) { file = blob; }
            if (!entry.hash || !global.crypto || !global.crypto.subtle) return Promise.resolve({ id: entry.id, file: file, hasBlob: true, hashVerified: false });
            return file.arrayBuffer().then(function (buffer) { return global.crypto.subtle.digest("SHA-256", buffer); }).then(function (digest) {
              return { id: entry.id, file: file, hasBlob: true, hashVerified: "sha256:" + toHex(digest) === entry.hash };
            }).catch(function () { return { id: entry.id, file: file, hasBlob: true, hashVerified: false }; });
          });
          return Promise.all(jobs).then(function (verified) {
            if (restoreEpoch !== epoch) return;
            savedGeneration = cp.generation;
            setRestoredFiles(cp.generation, verified);
            send({
              kind: "restore",
              checkpoint: cp,
              files: verified.filter(function (item) { return item.file; }).map(function (item) {
                return { id: item.id, file: item.file, hasBlob: item.hasBlob, hashVerified: item.hashVerified };
              })
            });
          });
        }).catch(function (error) {
          if (restoreEpoch !== epoch) return;
          send({ kind: "restoreFailed", message: storageErrorMessage(error) });
        });
      } else if (request.action === "discard") {
        epoch += 1;
        var discardEpoch = epoch;
        persistence.discard().then(function () {
          if (discardEpoch !== epoch) return;
          // Only the stored browser copies are removed; files already
          // live in this tab keep working as a memory-only import.
          // The epoch bump rejects any save/restore still in flight
          // for the discarded generation.
          savedGeneration = null;
          send({ kind: "discarded" });
        }).catch(function (error) {
          send({ kind: "discardFailed", message: storageErrorMessage(error) });
        });
      }
    });
  }

  var api = {
    wire: wire,
    wirePersistence: wirePersistence,
    createPersistence: createPersistence,
    validateCheckpointForSave: validateCheckpointForSave,
    checkpointTotals: checkpointTotals,
    summaryOf: summaryOf,
    assertNoSecrets: assertNoSecrets,
    storageErrorMessage: storageErrorMessage,
    isValidHash: isValidHash,
    validateHashRequest: validateHashRequest,
    fileFact: fileFact,
    retainable: retainable,
    MAX_BYTES: MAX_BYTES,
    MAX_FILES: MAX_FILES,
    MAX_TOTAL_BYTES: MAX_TOTAL_BYTES,
    PERSIST_SCHEMA: PERSIST_SCHEMA
  };
  global.KuraBrowserImport = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(typeof window !== "undefined" ? window : globalThis);
