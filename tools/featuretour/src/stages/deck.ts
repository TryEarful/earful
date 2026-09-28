// deck: content/deck.md plus out/shots.json become one HTML document,
// which Chromium prints to a 16:9 PDF.

import { isAbsolute, join } from "@std/path";
import type { Browser } from "playwright";
import { OUT_DIR, today } from "../config.ts";
import { loadDeck, type SlideDef } from "../content.ts";
import { CSS, renderSlide, SLIDE_H, SLIDE_W } from "../deck/layouts.ts";
import type { Manifest } from "../shots.ts";

export async function deck(browser: Browser, manifest: Manifest): Promise<string> {
    const slides = await loadDeck();
    const missing = slides.flatMap((s) =>
        s.shots.filter((id) => !manifest.get(id)).map((id) => `${s.id}: ${id}`)
    );
    if (missing.length) {
        throw new Error(
            "deck.md refers to screenshots that were not taken:\n  " + missing.join("\n  "),
        );
    }
    for (const slide of slides) {
        if (slide.layout === "code" && slide.code) slide.codeText = await readCode(slide);
    }
    const lookup = (id: string) => manifest.get(id)!;
    const html = slides.map((s, i) => renderSlide(s, lookup, i + 1, slides.length)).join("\n");
    const doc =
        `<!doctype html><html><head><meta charset="utf-8"><title>Earful feature tour</title>` +
        `<style>${CSS}</style></head><body>${html}</body></html>`;
    const htmlPath = join(OUT_DIR, "deck.html");
    await Deno.writeTextFile(htmlPath, doc);

    const page = await browser.newPage({ viewport: { width: SLIDE_W, height: SLIDE_H } });
    await page.goto("file://" + htmlPath, { waitUntil: "networkidle" });
    await page.evaluate(() => document.fonts.ready);
    const problems = await page.evaluate(({ w, h }) => {
        const out: string[] = [];
        document.querySelectorAll<HTMLElement>(".slide").forEach((slide, i) => {
            if (slide.scrollHeight > h || slide.scrollWidth > w) {
                out.push(`slide ${i + 1} overflows`);
            }
            slide.querySelectorAll("img").forEach((img) => {
                if (!img.naturalWidth) out.push(`slide ${i + 1}: image failed to load`);
            });
        });
        return out;
    }, { w: SLIDE_W, h: SLIDE_H });
    if (problems.length) {
        await page.close();
        throw new Error("deck: " + problems.join("; "));
    }
    const pdfPath = join(OUT_DIR, `earful-feature-tour-${today()}.pdf`);
    await page.pdf({
        path: pdfPath,
        width: `${SLIDE_W}px`,
        height: `${SLIDE_H}px`,
        printBackground: true,
        preferCSSPageSize: true,
    });
    await page.close();
    console.log(`deck: ${slides.length} slides -> ${pdfPath}`);
    return pdfPath;
}

async function readCode(slide: SlideDef): Promise<string> {
    const path = isAbsolute(slide.code!) ? slide.code! : join(OUT_DIR, slide.code!);
    let text: string;
    try {
        text = await Deno.readTextFile(path);
    } catch {
        throw new Error(`deck.md: slide ${slide.id} refers to ${slide.code}, which does not exist`);
    }
    const lines = text.split("\n");
    const keep = slide.lines ?? lines.length;
    const shown = lines.slice(0, keep).map((l) => l.length > 110 ? l.slice(0, 107) + "..." : l);
    return shown.join("\n") + (lines.length > keep ? "\n..." : "");
}
