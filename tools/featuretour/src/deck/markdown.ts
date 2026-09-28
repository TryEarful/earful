// The few markdown constructs deck.md uses, rendered to HTML. A dependency
// would bring a full parser for headings, paragraphs, lists and emphasis.

export function escapeHTML(s: string): string {
    return s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll(
        '"',
        "&quot;",
    );
}

export function inline(text: string): string {
    return escapeHTML(text)
        .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
        .replace(/(^|[^*])\*([^*]+)\*/g, "$1<em>$2</em>")
        .replace(/`([^`]+)`/g, "<code>$1</code>");
}

export interface Block {
    type: "h1" | "h2" | "h3" | "p" | "ul";
    text?: string;
    items?: string[];
}

export function parseBlocks(markdown: string): Block[] {
    const blocks: Block[] = [];
    const lines = markdown.split("\n");
    let para: string[] = [];
    let list: string[] = [];
    const flush = () => {
        if (para.length) blocks.push({ type: "p", text: para.join(" ") });
        if (list.length) blocks.push({ type: "ul", items: list });
        para = [];
        list = [];
    };
    for (const raw of lines) {
        const line = raw.trimEnd();
        const heading = line.match(/^(#{1,3})\s+(.*)$/);
        if (heading) {
            flush();
            const type = (["h1", "h2", "h3"] as const)[heading[1].length - 1];
            blocks.push({ type, text: heading[2] });
        } else if (/^- /.test(line)) {
            if (para.length) flush();
            list.push(line.slice(2));
        } else if (line.trim() === "") {
            flush();
        } else {
            if (list.length) flush();
            para.push(line.trim());
        }
    }
    flush();
    return blocks;
}

export function renderBlocks(blocks: Block[]): string {
    return blocks.map((b) => {
        switch (b.type) {
            case "ul":
                return "<ul>" + b.items!.map((i) => `<li>${inline(i)}</li>`).join("") + "</ul>";
            default:
                return `<${b.type}>${inline(b.text!)}</${b.type}>`;
        }
    }).join("\n");
}

export function render(markdown: string): string {
    return renderBlocks(parseBlocks(markdown));
}

/** The first heading (any level) and everything else, for slides with a title row. */
export function splitTitle(markdown: string): { title: string; rest: Block[] } {
    const blocks = parseBlocks(markdown);
    const i = blocks.findIndex((b) => b.type === "h1" || b.type === "h2");
    if (i === -1) return { title: "", rest: blocks };
    const title = blocks[i].text ?? "";
    return { title, rest: [...blocks.slice(0, i), ...blocks.slice(i + 1)] };
}

/** Splits blocks into columns at each h3. */
export function columnsOf(blocks: Block[]): Block[][] {
    const columns: Block[][] = [];
    let current: Block[] = [];
    for (const b of blocks) {
        if (b.type === "h3" && current.length) {
            columns.push(current);
            current = [];
        }
        current.push(b);
    }
    if (current.length) columns.push(current);
    return columns;
}
