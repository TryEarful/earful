// mailpit is the inbox of the compose stack: magic links and invitations
// land there, and this reads them back the way a person would.

import { MAILPIT } from "./config.ts";

interface Search {
    messages?: { ID: string }[];
}

/** Empties the inbox so "the newest message to this address" is unambiguous. */
export async function clearMailbox() {
    const res = await fetch(`${MAILPIT}/api/v1/messages`, { method: "DELETE" });
    await res.body?.cancel();
    if (!res.ok) throw new Error(`mailpit: clearing the inbox failed (${res.status})`);
}

async function search(addr: string): Promise<{ ID: string }[]> {
    const res = await fetch(
        `${MAILPIT}/api/v1/search?query=${encodeURIComponent("to:" + addr)}`,
    );
    const body = (await res.json()) as Search;
    return body.messages ?? [];
}

/** The ids already in the inbox for addr, to exclude from a later wait. */
export async function seenMessages(addr: string): Promise<Set<string>> {
    return new Set((await search(addr)).map((m) => m.ID));
}

export interface FoundLink {
    id: string;
    link: string;
}

/** Waits for a message to addr that is not in `before` and returns the first link matching pattern. */
export async function waitForLink(
    addr: string,
    pattern: RegExp,
    before: Set<string> = new Set(),
    timeoutMs = 20_000,
): Promise<FoundLink> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        for (const m of await search(addr)) {
            if (before.has(m.ID)) continue;
            const res = await fetch(`${MAILPIT}/api/v1/message/${m.ID}`);
            const body = (await res.json()) as { Text: string };
            const match = body.Text.match(pattern);
            if (match) return { id: m.ID, link: match[0] };
        }
        await new Promise((r) => setTimeout(r, 250));
    }
    throw new Error(`mailpit: no new message with a matching link arrived for ${addr}`);
}

/** The inbox UI's view of one message, for a screenshot. */
export function messageURL(id: string): string {
    return `${MAILPIT}/view/${id}`;
}
