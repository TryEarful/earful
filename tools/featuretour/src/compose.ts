// The compose stack as seen from the tool: a psql pipe into the Postgres
// container, and bringing the app up with a chosen environment.

import { APP_ENV_KEYS, BASE, REPO_ROOT } from "./config.ts";

export interface RunResult {
    code: number;
    stdout: string;
    stderr: string;
}

export async function run(
    cmd: string,
    args: string[],
    opts: { env?: Record<string, string>; stdin?: string; quiet?: boolean } = {},
): Promise<RunResult> {
    const command = new Deno.Command(cmd, {
        args,
        cwd: REPO_ROOT,
        env: opts.env,
        clearEnv: opts.env !== undefined,
        stdin: opts.stdin === undefined ? "null" : "piped",
        stdout: "piped",
        stderr: "piped",
    });
    const child = command.spawn();
    if (opts.stdin !== undefined) {
        const writer = child.stdin.getWriter();
        await writer.write(new TextEncoder().encode(opts.stdin));
        await writer.close();
    }
    const { code, stdout, stderr } = await child.output();
    const result = {
        code,
        stdout: new TextDecoder().decode(stdout),
        stderr: new TextDecoder().decode(stderr),
    };
    if (code !== 0 && !opts.quiet) {
        throw new Error(`${cmd} ${args.join(" ")} failed (${code}):\n${result.stderr}`);
    }
    return result;
}

/** Runs SQL inside the compose Postgres. Errors abort (ON_ERROR_STOP). */
export async function psql(sql: string): Promise<string> {
    const result = await run("docker", [
        "compose",
        "exec",
        "-T",
        "postgres",
        "psql",
        "-U",
        "earful",
        "-d",
        "earful",
        "-v",
        "ON_ERROR_STOP=1",
        "-X",
        "-q",
        "-At",
    ], { stdin: sql });
    return result.stdout;
}

/** Runs one SELECT and returns its rows as objects. */
export async function query<T>(select: string): Promise<T[]> {
    const out = await psql(`SELECT coalesce(json_agg(t), '[]') FROM (${select}) t;`);
    return JSON.parse(out.trim() || "[]") as T[];
}

/** SQL string literal. */
export function lit(value: string | null | undefined): string {
    if (value === null || value === undefined) return "NULL";
    return "'" + value.replaceAll("'", "''") + "'";
}

/** A jsonb literal from any JSON-serialisable value. */
export function jsonb(value: unknown): string {
    return lit(JSON.stringify(value)) + "::jsonb";
}

/** Brings the named services up, recreating the app when its environment changed. */
export async function composeUp(services: string[], extra: Record<string, string> = {}) {
    const env = { ...Deno.env.toObject(), ...extra };
    await run("docker", ["compose", "--profile", "app", "up", "-d", "--wait", ...services], {
        env,
    });
}

/** Restores the app container to the compose file's own defaults. */
export async function composeRestore() {
    const env = Deno.env.toObject();
    for (const key of APP_ENV_KEYS) delete env[key];
    await run("docker", ["compose", "--profile", "app", "up", "-d", "--wait", "app"], { env });
}

/** Polls the app's health probe: --wait covers the container, this covers the socket. */
export async function waitHealthy(timeoutMs = 60_000) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        try {
            const res = await fetch(`${BASE}/healthz`);
            await res.body?.cancel();
            if (res.ok) return;
        } catch {
            // not up yet
        }
        await new Promise((r) => setTimeout(r, 500));
    }
    throw new Error(`app at ${BASE} did not become healthy within ${timeoutMs / 1000}s`);
}
