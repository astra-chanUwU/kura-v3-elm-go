/* Kura browser folder-import bridge. Browser/stdlib only.
   File values stay in memory; nothing here writes storage, URLs, or logs.
   Only one picked preview is retained, generation-tagged: a stale or
   cancelled picker can never replace the files behind the preview the
   user sees, and released entries drop their File references at once. */
(function (global) {
  "use strict";

  var MAX_BYTES = 25 * 1024 * 1024;
  var ACCEPT_MIMES = ["image/jpeg", "image/png", "image/gif", "image/webp"];
  var ACCEPT_EXTS = [".jpg", ".jpeg", ".png", ".gif", ".webp"];

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
    filesInput.accept = "image/jpeg,image/png,image/gif,image/webp";
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
  }

  var api = { wire: wire, isValidHash: isValidHash, validateHashRequest: validateHashRequest, fileFact: fileFact, retainable: retainable, MAX_BYTES: MAX_BYTES };
  global.KuraBrowserImport = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(typeof window !== "undefined" ? window : globalThis);
