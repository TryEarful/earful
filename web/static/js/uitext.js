// The interface's wording, for scripts.
//
// A script says things: what a button reads while it works, what is
// announced when a recording ends. The words are not in the script. The
// page carries the messages its scripts need, in the language it is
// worded in, as a block of JSON (the Content-Security-Policy allows a
// page data inline and no code), and this reads them.
//
//   EarfulText.t("js.generate.working")
//   EarfulText.t("respond.question.position", { Current: 2, Total: 5 })
//   EarfulText.n("js.respond.versions", 3)
//   EarfulText.parts("js.voice.hold", { Key: keycap })
//
// A message names its values as it does everywhere else: {{.Current}}.
//
// A message the page did not carry is shown by its name. That is the
// wrong thing to show and the right thing to find: it says which
// message is missing, and the page that should have carried it.
(function () {
  "use strict";

  var lang = document.documentElement.lang || "en";
  var messages = {};
  var source = document.getElementById("interface-text");
  if (source) {
    try {
      var carried = JSON.parse(source.textContent);
      lang = carried.lang || lang;
      messages = carried.messages || {};
    } catch (err) {
      // Nothing to read: every message is shown by its name.
    }
  }

  var rules = null;
  try {
    rules = new Intl.PluralRules(lang);
  } catch (err) {
    // A browser without the rules of this language uses the form every
    // message has.
  }

  var VALUE = /\{\{\s*\.([A-Za-z0-9_]+)\s*\}\}/g;

  // The wording of a message: its one wording, or the form of it that
  // suits count.
  function wording(id, count) {
    var message = messages[id];
    if (message === undefined) {
      if (window.console && console.error) console.error("no message: " + id);
      return id;
    }
    if (typeof message === "string") return message;
    var form = "other";
    if (count !== undefined && rules) form = rules.select(count);
    return message[form] !== undefined ? message[form] : message.other;
  }

  function fill(text, values) {
    return text.replace(VALUE, function (whole, name) {
      if (values && Object.prototype.hasOwnProperty.call(values, name)) return String(values[name]);
      return whole;
    });
  }

  function t(id, values) {
    return fill(wording(id), values);
  }

  // The form of a message that suits count, which the message can show
  // as {{.Count}}.
  function n(id, count, values) {
    var all = { Count: count };
    for (var name in values || {}) {
      if (Object.prototype.hasOwnProperty.call(values, name)) all[name] = values[name];
    }
    return fill(wording(id, count), all);
  }

  // A message as a list of its pieces, for a sentence with something in
  // it that is not text: a keycap, an icon. The text between the values
  // comes back as strings and each value as it was given, in the order
  // the language puts them.
  function parts(id, values) {
    var text = wording(id);
    var out = [];
    var last = 0;
    // The space either side of a value belongs to the sentence, and the
    // pieces are laid out by whoever asked for them: each is given
    // without it, and a piece that was only space is not given.
    function words(from, to) {
      var piece = text.slice(from, to).trim();
      if (piece) out.push(piece);
    }
    text.replace(VALUE, function (whole, name, at) {
      words(last, at);
      out.push(values && Object.prototype.hasOwnProperty.call(values, name) ? values[name] : whole);
      last = at + whole.length;
      return whole;
    });
    words(last, text.length);
    return out;
  }

  window.EarfulText = { lang: lang, t: t, n: n, parts: parts };
})();
