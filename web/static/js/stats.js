// The stats page (issue #2): a chart over the numbers the page already
// holds, a series picker, and sortable question rows.
//
// The page is complete without this file. The trend's numbers are a
// table until this draws them, and the table stays reachable behind a
// disclosure afterwards, so nothing a reader needs exists only in pixels.
//
// Hand-written on purpose: the dashboard has no bundler and no vendored
// libraries, the CSP allows only same-origin scripts, and a line over a
// few hundred points does not need one.
(function () {
  "use strict";

  // The wording, which the page carries (uitext.js). Without it there
  // is nothing to say, and the page is left as it works without a
  // script.
  var T = window.EarfulText;
  if (!T) return;

  // ---- trend chart -------------------------------------------------------

  // A series is named twice: as a heading, and as it is said after a
  // number. English makes the second from the first by putting it in
  // lower case, which is not how every language gets from one to the
  // other.
  var SERIES = [
    { key: "submissions", label: T.t("stats.submissions.label"), counted: T.t("js.stats.series.submissions") },
    { key: "opened", label: T.t("stats.opened.label"), counted: T.t("js.stats.series.opened") },
  ];
  var SVG = "http://www.w3.org/2000/svg";

  function drawTrend(section, dataNode) {
    var points;
    try {
      points = JSON.parse(dataNode.textContent || "[]");
    } catch (_) {
      return;
    }
    if (!Array.isArray(points) || points.length === 0) return;

    var chart = section.querySelector(".js-trend-chart");
    var table = section.querySelector(".js-trend-table");
    var controls = section.querySelector(".js-trend-controls");
    if (!chart || !table || !controls) return;

    // The series picker only exists once there is a chart to switch.
    var select = document.createElement("select");
    select.setAttribute("aria-label", T.t("js.stats.series.label"));
    SERIES.forEach(function (series) {
      var option = document.createElement("option");
      option.value = series.key;
      option.textContent = series.label;
      select.appendChild(option);
    });
    controls.appendChild(select);

    // The table keeps every number reachable; it just stops leading.
    var details = document.createElement("details");
    var summary = document.createElement("summary");
    summary.textContent = T.t("js.stats.table");
    details.appendChild(summary);
    table.parentNode.insertBefore(details, table);
    details.appendChild(table);

    function render() {
      var key = select.value;
      var series = SERIES.filter(function (s) { return s.key === key; })[0];
      chart.setAttribute("aria-label", T.t("js.stats.chart.aria", { Series: series.label }));
      chart.replaceChildren(lineChart(points, key, series));
    }
    select.addEventListener("change", render);
    render();
  }

  // lineChart draws one series as a filled line in a fixed viewBox that
  // scales with its container. Text sizes are in viewBox units, chosen
  // so the labels stay readable at phone width.
  function lineChart(points, key, series) {
    var width = 640, height = 220;
    var pad = { top: 12, right: 12, bottom: 28, left: 36 };
    var innerW = width - pad.left - pad.right;
    var innerH = height - pad.top - pad.bottom;

    var max = 0;
    points.forEach(function (p) { if (p[key] > max) max = p[key]; });
    var top = niceCeiling(max);

    var svg = el("svg", { viewBox: "0 0 " + width + " " + height, class: "trend-svg js-trend-svg", role: "presentation" });
    var title = el("title");
    title.textContent = T.t("js.stats.chart.title", { Series: series.label });
    svg.appendChild(title);

    var x = function (i) {
      return points.length === 1 ? pad.left + innerW / 2 : pad.left + (i / (points.length - 1)) * innerW;
    };
    var y = function (v) { return pad.top + innerH - (v / top) * innerH; };

    // Gridlines and the y axis.
    var ticks = yTicks(top);
    ticks.forEach(function (t) {
      svg.appendChild(el("line", { x1: pad.left, x2: width - pad.right, y1: y(t), y2: y(t), class: "trend-grid" }));
      var text = el("text", { x: pad.left - 8, y: y(t), class: "trend-tick", "text-anchor": "end", "dominant-baseline": "middle" });
      text.textContent = String(t);
      svg.appendChild(text);
    });

    // The x axis shows a handful of dates, never all of them.
    var every = Math.max(1, Math.ceil(points.length / 6));
    points.forEach(function (p, i) {
      if (i % every !== 0 && i !== points.length - 1) return;
      var text = el("text", { x: x(i), y: height - 8, class: "trend-tick", "text-anchor": "middle" });
      text.textContent = p.label;
      svg.appendChild(text);
    });

    // Area under the line, then the line itself.
    var line = points.map(function (p, i) { return (i === 0 ? "M" : "L") + x(i).toFixed(1) + " " + y(p[key]).toFixed(1); }).join(" ");
    var area = line + " L" + x(points.length - 1).toFixed(1) + " " + y(0).toFixed(1) + " L" + x(0).toFixed(1) + " " + y(0).toFixed(1) + " Z";
    svg.appendChild(el("path", { d: area, class: "trend-area" }));
    svg.appendChild(el("path", { d: line, class: "trend-line" }));

    // One hoverable point per day, with the value as its title.
    points.forEach(function (p, i) {
      var dot = el("circle", { cx: x(i), cy: y(p[key]), r: points.length > 120 ? 2 : 3.5, class: "trend-dot js-trend-dot" });
      var t = el("title");
      t.textContent = T.t("js.stats.chart.point", { Day: p.label, Count: p[key], Series: series.counted });
      dot.appendChild(t);
      svg.appendChild(dot);
    });
    return svg;
  }

  // niceCeiling rounds a maximum up to something the eye reads as a
  // scale: 1, 2, 5 times a power of ten, and never below 4 so a chart
  // of ones and twos still has room.
  function niceCeiling(max) {
    if (max < 4) return 4;
    var power = Math.pow(10, Math.floor(Math.log10(max)));
    var candidates = [1, 2, 4, 5, 10];
    for (var i = 0; i < candidates.length; i++) {
      if (candidates[i] * power >= max) return candidates[i] * power;
    }
    return 10 * power;
  }

  function yTicks(top) {
    var step = top / 4;
    var ticks = [];
    for (var v = 0; v <= top; v += step) ticks.push(Number.isInteger(step) ? v : Math.round(v * 10) / 10);
    return ticks;
  }

  function el(name, attrs) {
    var node = document.createElementNS(SVG, name);
    Object.keys(attrs || {}).forEach(function (k) { node.setAttribute(k, attrs[k]); });
    return node;
  }

  // ---- sortable question rows -------------------------------------------

  function makeSortable(table) {
    var headers = Array.prototype.slice.call(table.querySelectorAll("th[data-sort]"));
    var body = table.tBodies[0];
    if (!body) return;
    var original = Array.prototype.slice.call(body.rows);

    headers.forEach(function (th, index) {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "sort-button";
      button.textContent = th.textContent;
      th.textContent = "";
      th.appendChild(button);

      button.addEventListener("click", function () {
        var current = th.getAttribute("aria-sort");
        var direction = current === "ascending" ? "descending" : "ascending";
        headers.forEach(function (other) { other.removeAttribute("aria-sort"); });
        th.setAttribute("aria-sort", direction);

        var kind = th.getAttribute("data-sort");
        var rows = original.slice();
        rows.sort(function (a, b) {
          var av = cellValue(a.cells[index], kind), bv = cellValue(b.cells[index], kind);
          var cmp = kind === "number" ? av - bv : av.localeCompare(bv);
          return direction === "ascending" ? cmp : -cmp;
        });
        rows.forEach(function (row) { body.appendChild(row); });
      });
    });
  }

  function cellValue(cell, kind) {
    if (!cell) return kind === "number" ? 0 : "";
    if (kind === "number") return Number(cell.getAttribute("data-value") || cell.textContent) || 0;
    return (cell.textContent || "").trim().toLowerCase();
  }

  // ---- entry point --------------------------------------------------------
  // Last, so every `var` above holds its value by the time it is read.

  var trend = document.getElementById("trend");
  var dataNode = document.getElementById("trend-data");
  if (trend && dataNode) drawTrend(trend, dataNode);

  var stops = document.querySelector(".js-stops[data-sortable]");
  if (stops) makeSortable(stops);
})();
