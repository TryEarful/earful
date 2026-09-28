// A stand-in for an OpenAI-compatible model server, enough for the app's
// `openai` provider: chat completions over SSE and whisper-style
// transcription. Prepared text instead of a model keeps every run identical.

import { MOCK_MODEL, MOCK_PORT } from "../config.ts";
import type { Answers, Message } from "./answers.ts";

export interface MockServer {
    stop(): Promise<void>;
}

export function startMock(answers: Answers): MockServer {
    const controller = new AbortController();
    const server = Deno.serve(
        // 0.0.0.0, because the app container reaches this through host.docker.internal.
        { hostname: "0.0.0.0", port: MOCK_PORT, signal: controller.signal, onListen: () => {} },
        (req) => handle(req, answers),
    );
    console.log(`mock model server on :${MOCK_PORT}`);
    return {
        async stop() {
            controller.abort();
            await server.finished;
        },
    };
}

async function handle(req: Request, answers: Answers): Promise<Response> {
    const url = new URL(req.url);
    if (req.method !== "POST") return new Response("not found", { status: 404 });
    if (url.pathname.endsWith("/audio/transcriptions")) {
        await req.formData(); // drain the multipart body the way a real endpoint would
        return Response.json({ text: answers.transcript() });
    }
    if (url.pathname.endsWith("/chat/completions")) {
        const body = (await req.json()) as { messages?: Message[] };
        return stream(answers.reply(body.messages ?? []));
    }
    return new Response("not found", { status: 404 });
}

/** Word-sized deltas with a short pause, so the app's streaming UI shows text arriving. */
function stream(text: string): Response {
    const pieces = text.match(/\S+\s*|\s+/g) ?? [];
    const encoder = new TextEncoder();
    const readable = new ReadableStream<Uint8Array>({
        async start(controller) {
            for (const piece of pieces) {
                const event = {
                    id: "mock",
                    object: "chat.completion.chunk",
                    model: MOCK_MODEL,
                    choices: [{ index: 0, delta: { content: piece }, finish_reason: null }],
                };
                controller.enqueue(encoder.encode("data: " + JSON.stringify(event) + "\n\n"));
                await new Promise((r) => setTimeout(r, 12));
            }
            controller.enqueue(encoder.encode("data: [DONE]\n\n"));
            controller.close();
        },
    });
    return new Response(readable, {
        headers: { "content-type": "text/event-stream", "cache-control": "no-cache" },
    });
}
