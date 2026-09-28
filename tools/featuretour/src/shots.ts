// out/shots.json: every screenshot with the facts the deck needs to size
// it. The deck never sees a pixel count in its own code; it reads them here.

import { join } from "@std/path";
import type { Locator, Page } from "playwright";
import { OUT_DIR, SHOTS_DIR } from "./config.ts";

/**
 * card: an element screenshot of one section, already tight.
 * viewport: the visible window of a page at desktop width.
 * page: a full-page capture at desktop width.
 * phone: the visible window at phone width.
 */
export type Kind = "card" | "viewport" | "page" | "phone";

export interface Box {
    x: number;
    y: number;
    width: number;
    height: number;
}

export interface Shot {
    id: string;
    file: string;
    kind: Kind;
    /** CSS pixels. */
    width: number;
    height: number;
    dpr: number;
    /** The region worth showing, in CSS pixels of the capture. */
    focus?: Box;
}

const FILE = join(OUT_DIR, "shots.json");

export class Manifest {
    private shots = new Map<string, Shot>();

    static async load(): Promise<Manifest> {
        const m = new Manifest();
        try {
            const list = JSON.parse(await Deno.readTextFile(FILE)) as Shot[];
            for (const s of list) m.shots.set(s.id, s);
        } catch {
            // first run
        }
        return m;
    }

    get(id: string): Shot | undefined {
        return this.shots.get(id);
    }

    ids(): string[] {
        return [...this.shots.keys()];
    }

    async put(shot: Shot) {
        this.shots.set(shot.id, shot);
        await this.save();
    }

    async save() {
        await Deno.mkdir(OUT_DIR, { recursive: true });
        const list = [...this.shots.values()].sort((a, b) => a.id.localeCompare(b.id));
        await Deno.writeTextFile(FILE, JSON.stringify(list, null, 2) + "\n");
    }
}

export interface ShootOptions {
    kind: Kind;
    /** For card: the element to capture. For others: the region to focus on. */
    locator?: Locator;
}

/** Captures one screenshot and records it. */
export async function shoot(manifest: Manifest, page: Page, id: string, opts: ShootOptions) {
    await Deno.mkdir(SHOTS_DIR, { recursive: true });
    const file = join(SHOTS_DIR, id + ".png");
    await page.waitForLoadState("networkidle").catch(() => {});
    const viewport = page.viewportSize() ?? { width: 0, height: 0 };
    const dpr = await page.evaluate(() => globalThis.devicePixelRatio);
    let shot: Shot;
    if (opts.kind === "card") {
        if (!opts.locator) throw new Error(`shot ${id}: a card needs a locator`);
        await opts.locator.scrollIntoViewIfNeeded();
        await page.waitForTimeout(150);
        const box = await opts.locator.boundingBox();
        if (!box) throw new Error(`shot ${id}: element not visible`);
        await opts.locator.screenshot({ path: file, scale: "device" });
        shot = { id, file, kind: "card", width: box.width, height: box.height, dpr };
    } else {
        const fullPage = opts.kind === "page";
        const focusLocator = opts.locator ?? page.locator("main, .site-header").first();
        // A focused element goes to the top of the window, so the capture
        // can start at it rather than wherever the page happened to sit.
        if (opts.locator && !fullPage) {
            await opts.locator.evaluate((el) => el.scrollIntoView({ block: "start" }));
        }
        await page.waitForTimeout(150);
        const scrollY = fullPage ? await page.evaluate(() => globalThis.scrollY) : 0;
        const box = await focusLocator.boundingBox().catch(() => null);
        // When the page is too short to scroll the element to the top, the
        // capture is clipped to start just above it instead.
        const clipY = opts.locator && !fullPage && box ? Math.max(0, Math.floor(box.y) - 24) : 0;
        const clip = clipY > 0
            ? { x: 0, y: clipY, width: viewport.width, height: viewport.height - clipY }
            : undefined;
        await page.screenshot({ path: file, fullPage, scale: "device", clip });
        const height = fullPage
            ? await page.evaluate(() => document.documentElement.scrollHeight)
            : viewport.height - clipY;
        shot = {
            id,
            file,
            kind: opts.kind,
            width: viewport.width,
            height,
            dpr,
            focus: box
                ? { x: box.x, y: box.y + scrollY - clipY, width: box.width, height: box.height }
                : undefined,
        };
    }
    await manifest.put(shot);
    console.log(`  shot ${id}`);
}
