// Shared WebSocket client for every streaming surface: voice transcripts
// (M5), AI-drafted questions (M6-T3), insight summaries (M10).
//
// Like everything else in web/static/js, this is an enhancement. Each
// caller has a server-rendered path that works without it, so failing to
// connect must always end in "the plain form still works", never in a
// dead-end. Callers get onChunk/onDone/onError and never see a socket.
//
// Cloud Run closes connections at 60 minutes and the server closes them
// deliberately at 55 (ADR-0007), so reconnecting is normal operation
// rather than error handling.
window.EarfulSocket = (function () {
  "use strict";

  var MAX_ATTEMPTS = 5;

  // open(path, handlers) -> { send, sendBinary, close }
  //
  // handlers: onOpen, onChunk(text), onStatus(text), onDone,
  //           onError(message, code), onGone
  //
  // onGone fires when the socket is gone for good — the caller should
  // fall back to whatever works without it.
  function open(path, handlers) {
    handlers = handlers || {};
    var attempts = 0;
    var closedByUs = false;
    var finished = false; // the server sent done or error: the session is over
    var socket = null;
    var queue = [];

    function url() {
      var scheme = window.location.protocol === "https:" ? "wss://" : "ws://";
      return scheme + window.location.host + path;
    }

    function connect() {
      try {
        socket = new WebSocket(url());
      } catch (err) {
        giveUp();
        return;
      }
      socket.binaryType = "arraybuffer";

      socket.onopen = function () {
        attempts = 0;
        while (queue.length) socket.send(queue.shift());
        if (handlers.onOpen) handlers.onOpen();
      };

      socket.onmessage = function (event) {
        var frame;
        try {
          frame = JSON.parse(event.data);
        } catch (err) {
          return;
        }
        // done and error end the session: the server closes right after
        // sending either, and that close is the end of the conversation,
        // not a drop to recover from. Reconnecting here would replay the
        // caller's opening message into a fresh session — for voice, a
        // new take the respondent never started — and a server that
        // refuses each one (a stale form token, say) turns that into a
        // loop at the backoff rate until the rate limiter ends it.
        if (frame.type === "done" || frame.type === "error") finished = true;
        if (frame.type === "chunk" && handlers.onChunk) handlers.onChunk(frame.text || "");
        else if (frame.type === "status" && handlers.onStatus) handlers.onStatus(frame.text || "");
        else if (frame.type === "done" && handlers.onDone) handlers.onDone();
        else if (frame.type === "error" && handlers.onError) {
          handlers.onError(frame.error || "", frame.code || "");
        }
      };

      socket.onclose = function () {
        if (closedByUs || finished) return;
        attempts += 1;
        if (attempts > MAX_ATTEMPTS) {
          giveUp();
          return;
        }
        // Backoff: 250ms, 500ms, 1s, 2s, 4s.
        setTimeout(connect, 250 * Math.pow(2, attempts - 1));
      };

      socket.onerror = function () {
        // onclose always follows; reconnection is handled there.
      };
    }

    function giveUp() {
      closedByUs = true;
      if (handlers.onGone) handlers.onGone();
    }

    function rawSend(payload) {
      if (socket && socket.readyState === WebSocket.OPEN) socket.send(payload);
      else if (!closedByUs && !finished) queue.push(payload);
    }

    connect();

    return {
      send: function (message) {
        rawSend(JSON.stringify(message));
      },
      sendBinary: function (buffer) {
        // Never queued: buffered audio would arrive out of order behind a
        // reconnect, and a stale chunk is worse than a missing one.
        if (socket && socket.readyState === WebSocket.OPEN) socket.send(buffer);
      },
      close: function () {
        closedByUs = true;
        if (socket) socket.close();
      },
      isOpen: function () {
        return !!socket && socket.readyState === WebSocket.OPEN;
      },
    };
  }

  return { open: open, supported: typeof WebSocket !== "undefined" };
})();
