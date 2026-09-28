// Slide geometry. Every number that shapes a slide lives here, and image
// sizes are derived from the screenshot manifest, never typed per image.

import type { SlideDef } from "../content.ts";
import type { Shot } from "../shots.ts";
import { columnsOf, escapeHTML, inline, renderBlocks, splitTitle } from "./markdown.ts";

export const SLIDE_W = 1920;
export const SLIDE_H = 1080;
const PAD = 96;
const CONTENT_W = SLIDE_W - 2 * PAD; // 1728
const HEAD_H = 176; // a two-line title with a one-line lead, or the reverse
const CONTENT_H = 690;
const CAPTION_H = 64;

/** Image box per layout: width, and the height left after the caption. */
const BOX: Record<string, { w: number; h: number }> = {
    two: { w: (CONTENT_W - 48) / 2, h: CONTENT_H - CAPTION_H },
    one: { w: CONTENT_W, h: CONTENT_H - CAPTION_H },
    side: { w: CONTENT_W - 600 - 72, h: CONTENT_H - CAPTION_H },
    code: { w: (CONTENT_W - 48) / 2, h: CONTENT_H - CAPTION_H },
};

export const CSS = `
@page { size: ${SLIDE_W}px ${SLIDE_H}px; margin: 0; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body { font-family: -apple-system, "SF Pro Text", "Helvetica Neue", Inter, Arial, sans-serif;
  color: #15121f; background: #fff; -webkit-print-color-adjust: exact; print-color-adjust: exact; }
.slide { width: ${SLIDE_W}px; height: ${SLIDE_H}px; padding: ${PAD}px ${PAD}px 0; position: relative;
  overflow: hidden; page-break-after: always; break-after: page; background: #fff; }
.slide:last-child { page-break-after: auto; }
.head { height: ${HEAD_H}px; overflow: hidden; }
.head h2 { font-size: 52px; line-height: 1.1; margin: 0 0 12px; letter-spacing: -0.01em; }
.head p { font-size: 26px; line-height: 1.4; color: #4b4660; margin: 0; max-width: 1560px; }
.content { height: ${CONTENT_H}px; }
.foot { position: absolute; left: ${PAD}px; right: ${PAD}px; bottom: 36px; display: flex;
  justify-content: space-between; font-size: 20px; color: #8d88a3; }
.grid-two { display: grid; grid-template-columns: 1fr 1fr; gap: 48px; align-items: start; }
.grid-side { display: grid; grid-template-columns: 600px 1fr; gap: 72px; align-items: start; }
.copy p, .copy li { font-size: 28px; line-height: 1.45; color: #2e2a3d; margin: 0 0 22px; }
.copy ul { padding-left: 32px; margin: 0 0 22px; }
.copy h3 { font-size: 32px; margin: 0 0 16px; color: #5b3df5; }
code { font: 0.92em "SF Mono", Menlo, Consolas, monospace; }
.cols { display: grid; gap: 56px; }
.shot { overflow: hidden; border: 2px solid #d9d9df; border-radius: 12px; background: #fff;
  position: relative; }
.shot img { position: absolute; display: block; }
figure { margin: 0; }
figcaption { font-size: 22px; line-height: 1.35; color: #4b4660; margin-top: 14px;
  height: ${CAPTION_H - 14}px; overflow: hidden; }
pre { margin: 0; font: 20px/1.45 "SF Mono", Menlo, Consolas, monospace; background: #f5f3ff;
  border: 2px solid #e2ddfb; border-radius: 12px; padding: 24px 28px; white-space: pre-wrap;
  word-break: break-all; color: #2e2a3d; overflow: hidden; }
.cover, .section { color: #fff; padding: ${PAD}px; display: flex; flex-direction: column;
  justify-content: center; }
.cover { background: #5b3df5; }
.section { background: #1d1638; padding-right: 420px; }
.cover .mark, .section .mark { font-size: 26px; font-weight: 600; letter-spacing: 0.06em;
  text-transform: uppercase; color: rgba(255,255,255,0.75); margin-bottom: 28px; }
.cover h1 { font-size: 150px; margin: 0 0 20px; letter-spacing: -0.03em; line-height: 1; }
.section h1 { font-size: 84px; margin: 0 0 30px; line-height: 1.05; letter-spacing: -0.02em; }
.cover p, .section p { font-size: 34px; line-height: 1.45; margin: 0 0 20px; opacity: 0.92;
  max-width: 1300px; }
.cover p:first-of-type { font-size: 44px; }
.section ul { margin: 12px 0 0; padding-left: 28px; font-size: 26px; line-height: 1.7;
  color: #d9d2ff; }
.cover .foot, .section .foot { color: rgba(255,255,255,0.55); }
`;

export interface ShotLookup {
    (id: string): Shot;
}

/** The HTML of one slide, given its shots. */
export function renderSlide(slide: SlideDef, lookup: ShotLookup, n: number, total: number): string {
    const foot =
        `<div class="foot"><span>${n} / ${total}</span><span>Earful feature tour</span></div>`;
    switch (slide.layout) {
        case "cover":
            return `<section class="slide cover">${cover(slide.body)}${foot}</section>`;
        case "section":
            return `<section class="slide section">${cover(slide.body)}${foot}</section>`;
        case "text": {
            const { title, rest } = splitTitle(slide.body);
            const lead = rest.find((b) =>
                b.type === "p" && !rest.slice(0, rest.indexOf(b)).some((x) => x.type === "h3")
            );
            const bodyBlocks = lead ? rest.filter((b) => b !== lead) : rest;
            const cols = columnsOf(bodyBlocks);
            const count = slide.columns ?? cols.length;
            const html = cols.map((c) => `<div class="copy">${renderBlocks(c)}</div>`).join("");
            return `<section class="slide">${head(title, lead?.text)}
<div class="content"><div class="cols" style="grid-template-columns: repeat(${count}, 1fr)">${html}</div></div>${foot}</section>`;
        }
        case "side": {
            const { title, rest } = splitTitle(slide.body);
            const shot = lookup(slide.shots[0]);
            return `<section class="slide">${head(title)}
<div class="content grid-side"><div class="copy">${renderBlocks(rest)}</div>${
                figure(shot, BOX.side, slide.captions[0])
            }</div>${foot}</section>`;
        }
        case "two": {
            const { title, rest } = splitTitle(slide.body);
            const figures = slide.shots.map((id, i) =>
                figure(lookup(id), BOX.two, slide.captions[i])
            ).join("");
            return `<section class="slide">${head(title, leadOf(rest))}
<div class="content grid-two">${figures}</div>${foot}</section>`;
        }
        case "one": {
            const { title, rest } = splitTitle(slide.body);
            return `<section class="slide">${head(title, leadOf(rest))}
<div class="content">${
                figure(lookup(slide.shots[0]), BOX.one, slide.captions[0])
            }</div>${foot}</section>`;
        }
        case "code": {
            const { title, rest } = splitTitle(slide.body);
            const pre = `<pre>${escapeHTML(slide.codeText ?? "")}</pre>`;
            if (slide.shots.length === 1) {
                return `<section class="slide">${head(title, leadOf(rest))}
<div class="content grid-two">${
                    figure(lookup(slide.shots[0]), BOX.code, slide.captions[0])
                }<figure>${pre}<figcaption>${
                    inline(slide.code ?? "")
                }</figcaption></figure></div>${foot}</section>`;
            }
            return `<section class="slide">${head(title, leadOf(rest))}
<div class="content">${pre}</div>${foot}</section>`;
        }
    }
}

function head(title: string, lead?: string): string {
    return `<div class="head"><h2>${inline(title)}</h2>${
        lead ? `<p>${inline(lead)}</p>` : ""
    }</div>`;
}

function leadOf(rest: ReturnType<typeof splitTitle>["rest"]): string | undefined {
    return rest.map((b) => b.type === "p" ? b.text : b.type === "ul" ? b.items?.join(" ") : "")
        .filter(Boolean).join(" ") || undefined;
}

/** Cover and section slides: an optional `###` kicker ("Story 1"), the title, prose, a list. */
function cover(body: string): string {
    const { title, rest } = splitTitle(body);
    const kicker = rest.find((b) => b.type === "h3");
    const others = rest.filter((b) => b !== kicker);
    return (kicker ? `<div class="mark">${inline(kicker.text ?? "")}</div>` : "") +
        `<h1>${inline(title)}</h1>` + renderBlocks(others);
}

/** An image box sized from the manifest. */
function figure(shot: Shot, box: { w: number; h: number }, caption: string): string {
    const s = place(shot, box);
    return `<figure><div class="shot" style="width:${s.boxW}px;height:${s.boxH}px">` +
        `<img src="${escapeHTML(shot.file)}" alt="" style="width:${s.imgW}px;left:${-s
            .left}px;top:${-s.top}px">` +
        `</div><figcaption>${inline(caption)}</figcaption></figure>`;
}

interface Placement {
    boxW: number;
    boxH: number;
    imgW: number;
    left: number;
    top: number;
}

/**
 * card: the element fills the box width and is clipped at the bottom.
 * viewport/page: the focus region (the content column, or a chosen
 * element) is scaled to the box width and the view starts just above it.
 * phone: the whole height of the box, centred.
 */
export function place(shot: Shot, box: { w: number; h: number }): Placement {
    const pad = 32;
    if (shot.kind === "card") {
        const zoom = Math.min(box.w / shot.width, 1.7);
        return {
            boxW: box.w,
            boxH: Math.min(box.h, Math.round(shot.height * zoom)),
            imgW: Math.round(shot.width * zoom),
            left: 0,
            top: 0,
        };
    }
    if (shot.kind === "phone") {
        const zoom = box.h / shot.height;
        const w = Math.round(shot.width * zoom);
        return { boxW: w, boxH: box.h, imgW: w, left: 0, top: 0 };
    }
    const focus = shot.focus ?? { x: 0, y: 0, width: shot.width, height: shot.height };
    const zoom = Math.min(box.w / (focus.width + 2 * pad), 1.6);
    const imgW = Math.round(shot.width * zoom);
    const imgH = Math.round(shot.height * zoom);
    const boxW = box.w;
    const boxH = Math.min(box.h, imgH);
    const left = clamp(Math.round((focus.x - pad) * zoom), 0, Math.max(0, imgW - boxW));
    const top = clamp(Math.round((focus.y - 24) * zoom), 0, Math.max(0, imgH - boxH));
    return { boxW, boxH, imgW, left, top };
}

function clamp(v: number, lo: number, hi: number): number {
    return Math.max(lo, Math.min(hi, v));
}
