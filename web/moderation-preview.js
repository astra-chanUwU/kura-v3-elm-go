/* Kura moderation preview bridge. Browser/stdlib only.
   Fetches protected thumb-320 JPEGs with a bearer header and hands Elm
   transient object URLs. The credential is used only for the fetch to
   the coordinator API origin: it is never appended to a URL, stored,
   or logged, and no arbitrary URL from a caller is ever fetched — the
   path is built here from a validated numeric post id. Object URLs are
   revoked on release, on generation change, and on close/lock signals
   (releaseAll). Any failure reports an error event and the queue stays
   metadata-only. */
(function (global) {
  "use strict";

  var MAX_BYTES = 8 * 1024 * 1024;

  function intendedOrigin(apiBase) {
    var base = String(apiBase || "").trim();
    if (!base) return global.location.origin;
    try {
      var parsed = new URL(base, global.location.origin);
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
      return parsed.origin;
    } catch (e) {
      return null;
    }
  }

  function previewUrl(apiBase, postId) {
    var base = String(apiBase || "").trim().replace(/\/+$/, "");
    return base + "/api/posts/" + encodeURIComponent(postId) + "/moderation/preview";
  }

  function wire(app) {
    if (!app || !app.ports) return;
    var ports = app.ports;
    if (!ports.moderationPreviewRequested || !ports.moderationPreviewEvent) return;

    var urls = new Map(); // requestId -> { generation, postId, url }
    var currentGeneration = null;

    function revoke(entry) {
      try { URL.revokeObjectURL(entry.url); } catch (e) {}
    }

    function releaseWhere(predicate) {
      urls.forEach(function (entry, requestId) {
        if (predicate(entry)) { revoke(entry); urls.delete(requestId); }
      });
    }

    function send(generation, requestId, postId, url, error) {
      ports.moderationPreviewEvent.send({
        generation: generation, requestId: requestId, postId: postId, url: url, error: error
      });
    }

    if (ports.moderationPreviewReleased) {
      ports.moderationPreviewReleased.subscribe(function (value) {
        if (!value) return;
        if (value.all) { releaseWhere(function () { return true; }); currentGeneration = null; return; }
        if (typeof value.generation === "number") {
          var generation = value.generation;
          releaseWhere(function (entry) { return entry.generation === generation; });
          if (currentGeneration === generation) currentGeneration = null;
        }
      });
    }

    ports.moderationPreviewRequested.subscribe(function (value) {
      if (!value) return;
      var generation = value.generation;
      var requestId = value.requestId;
      var postId = String(value.postId || "");
      if (typeof generation !== "number" || typeof requestId !== "number" || !/^[0-9]+$/.test(postId)) {
        if (typeof generation === "number" && typeof requestId === "number") {
          send(generation, requestId, postId, null, "invalid preview request");
        }
        return;
      }
      // A new generation invalidates every earlier object URL.
      if (currentGeneration !== null && currentGeneration !== generation) {
        releaseWhere(function (entry) { return entry.generation !== generation; });
      }
      currentGeneration = generation;

      var origin = intendedOrigin(value.apiBase);
      if (!origin || typeof global.fetch !== "function") {
        send(generation, requestId, postId, null, "preview unavailable");
        return;
      }
      var url = previewUrl(value.apiBase, postId);
      var check;
      try { check = new URL(url, global.location.origin); } catch (e) {
        send(generation, requestId, postId, null, "preview unavailable");
        return;
      }
      if (check.origin !== origin) {
        send(generation, requestId, postId, null, "preview unavailable");
        return;
      }

      var headers = {};
      var credential = typeof value.credential === "string" ? value.credential.trim() : "";
      if (credential) headers.Authorization = "Bearer " + credential;

      var credentialsMode = origin === global.location.origin ? "same-origin" : "omit";

      global.fetch(url, { method: "GET", headers: headers, credentials: credentialsMode, redirect: "error" })
        .then(function (response) {
          if (!response.ok) throw new Error("preview status " + response.status);
          var type = String(response.headers.get("Content-Type") || "").toLowerCase();
          if (type.indexOf("image/jpeg") !== 0) throw new Error("preview is not jpeg");
          return response.blob();
        })
        .then(function (blob) {
          if (!blob || blob.size === 0 || blob.size > MAX_BYTES) throw new Error("preview size unavailable");
          if (currentGeneration !== generation) return; // superseded while fetching
          var previous = urls.get(requestId);
          if (previous) revoke(previous);
          var objectUrl = URL.createObjectURL(blob);
          urls.set(requestId, { generation: generation, postId: postId, url: objectUrl });
          send(generation, requestId, postId, objectUrl, null);
        })
        .catch(function () {
          send(generation, requestId, postId, null, "preview unavailable");
        });
    });
  }

  global.KuraModerationPreview = { wire: wire };
})(window);
