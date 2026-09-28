// seed: a month of responses and the counters that go with them, written
// directly to Postgres in the shapes the respondent path writes.
//
// The generator is deterministic (a seeded PRNG per story), so the same
// content files give the same distributions run after run.

import { jsonb, lit, psql } from "../compose.ts";
import type { QuestionDef, StoryDef, Surveys } from "../content.ts";
import { latest, saveState, type State, type StoryState, type VersionRef } from "../state.ts";

type Value =
    | { text: string }
    | { choice: string }
    | { choices: string[] }
    | { number: number }
    | { bool: boolean };

export async function seed(surveys: Surveys, state: State) {
    for (const [name, story] of Object.entries(surveys.stories)) {
        const st = state.stories[name];
        if (!st) throw new Error(`seed: no state for ${name}; run the build stage`);
        console.log(`seed: ${name}`);
        const sql = new SeedSQL(name, story, st);
        await psql(sql.render());
        st.seeded = sql.summary();
        await saveState(state);
    }
}

// --- deterministic randomness ---------------------------------------------

class Random {
    private s: number;
    constructor(seed: string) {
        let h = 2166136261;
        for (const c of seed) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
        this.s = h >>> 0 || 1;
    }
    next(): number {
        // mulberry32
        this.s = (this.s + 0x6D2B79F5) >>> 0;
        let t = this.s;
        t = Math.imul(t ^ (t >>> 15), t | 1);
        t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    }
    int(min: number, max: number): number {
        return min + Math.floor(this.next() * (max - min + 1));
    }
    weighted<T extends string>(weights: Record<T, number>): T {
        const entries = Object.entries(weights) as [T, number][];
        const total = entries.reduce((s, [, w]) => s + w, 0);
        let r = this.next() * total;
        for (const [k, w] of entries) {
            r -= w;
            if (r <= 0) return k;
        }
        return entries[entries.length - 1][0];
    }
    shuffle<T>(items: T[]): T[] {
        const a = [...items];
        for (let i = a.length - 1; i > 0; i--) {
            const j = this.int(0, i);
            [a[i], a[j]] = [a[j], a[i]];
        }
        return a;
    }
}

// --- one story -------------------------------------------------------------

class SeedSQL {
    private rng: Random;
    private lines: string[] = [];
    private daily = new Map<string, number>();
    private audience = new Map<string, number>();
    private pools = new Map<string, string[]>();
    private earliest = Infinity;
    private latestAt = -Infinity;
    private count = 0;

    constructor(private name: string, private story: StoryDef, private st: StoryState) {
        this.rng = new Random(name);
    }

    render(): string {
        const s = this.st;
        const survey = lit(s.surveyId);
        this.lines.push("BEGIN;");
        // Idempotence: a rerun replaces this survey's data rather than adding to it.
        this.lines.push(
            `DELETE FROM answer_translations WHERE answer_id IN (SELECT a.id FROM answers a JOIN responses r ON r.id = a.response_id WHERE r.survey_id = ${survey});`,
            `DELETE FROM answers WHERE response_id IN (SELECT id FROM responses WHERE survey_id = ${survey});`,
            `DELETE FROM responses WHERE survey_id = ${survey};`,
            `DELETE FROM survey_stats WHERE survey_id = ${survey};`,
            `DELETE FROM survey_stats_daily WHERE survey_id = ${survey};`,
            `UPDATE participants SET submitted_at = NULL WHERE survey_id = ${survey};`,
            // Insight runs are append-only except to the purge job, which
            // announces itself with this transaction-local setting; a cached
            // reading of replaced answers would otherwise be served again.
            "SET LOCAL earful.purging = 'on';",
            `DELETE FROM insight_runs WHERE survey_id = ${survey};`,
        );
        this.responses();
        this.counters();
        this.backdate();
        this.lines.push("COMMIT;");
        return this.lines.join("\n") + "\n";
    }

    summary() {
        return {
            responses: this.count,
            from: new Date(this.earliest).toISOString(),
            to: new Date(this.latestAt).toISOString(),
        };
    }

    private responses() {
        const story = this.story;
        const st = this.st;
        if (story.anonymity === "invited") {
            const submitted = story.submitted ?? [];
            const days = story.spread_days;
            for (const email of submitted) {
                const participant = st.participants.find((p) => p.email === email);
                if (!participant) throw new Error(`${this.name}: ${email} is not a participant`);
                const when = this.stamp(this.rng.int(0, days - 1));
                this.response(latest(st), when, participant.id);
                this.lines.push(
                    `UPDATE participants SET submitted_at = ${lit(when.toISOString())} WHERE id = ${
                        lit(participant.id)
                    };`,
                );
            }
            return;
        }
        const perVersion = story.responses_by_version
            ? Object.entries(story.responses_by_version).map(([n, c]) => [Number(n), c] as const)
            : [[latest(st).number, story.responses ?? 0] as const];
        const stamps = this.spread(perVersion.reduce((s, [, c]) => s + c, 0), story.spread_days);
        let i = 0;
        for (const [number, count] of perVersion) {
            const version = st.versions.find((v) => v.number === number);
            if (!version) throw new Error(`${this.name}: no published version ${number}`);
            for (let k = 0; k < count; k++) this.response(version, stamps[i++], null);
        }
    }

    /** Submission times over the last `days` days, older first, busier at weekends. */
    private spread(n: number, days: number): Date[] {
        const weights: Record<string, number> = {};
        for (let d = 0; d < days; d++) {
            const day = this.dayOf(days - 1 - d);
            const weekend = day.getUTCDay() === 5 || day.getUTCDay() === 6;
            weights[String(d)] = (weekend ? 1.9 : 1.0) * (0.6 + 0.4 * (d / days));
        }
        const stamps: Date[] = [];
        for (let k = 0; k < n; k++) {
            stamps.push(this.stamp(days - 1 - Number(this.rng.weighted(weights))));
        }
        return stamps.sort((a, b) => a.getTime() - b.getTime());
    }

    private dayOf(daysAgo: number): Date {
        const now = new Date();
        return new Date(
            Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - daysAgo),
        );
    }

    private stamp(daysAgo: number): Date {
        const day = this.dayOf(daysAgo);
        day.setUTCHours(this.rng.int(9, 22), this.rng.int(0, 59), this.rng.int(0, 59));
        if (day.getTime() > Date.now()) day.setTime(Date.now() - 60_000);
        return day;
    }

    private response(version: VersionRef, when: Date, participantId: string | null) {
        const id = crypto.randomUUID();
        const iso = when.toISOString();
        this.earliest = Math.min(this.earliest, when.getTime());
        this.latestAt = Math.max(this.latestAt, when.getTime());
        this.count++;
        this.lines.push(
            `INSERT INTO responses (id, survey_id, version_id, participant_id, duration_secs, submitted_at) VALUES (${
                lit(id)
            }, ${lit(this.st.surveyId)}, ${lit(version.id)}, ${lit(participantId)}, ${
                this.rng.int(80, 420)
            }, ${lit(iso)});`,
        );
        let lastIdentity = "";
        for (const q of version.questions) {
            const def = this.story.questions.find((d) => d.key === q.key)!;
            const value = this.answer(def);
            if (value === null) continue;
            this.lines.push(
                `INSERT INTO answers (id, response_id, question_id, question_identity_id, value) VALUES (${
                    lit(crypto.randomUUID())
                }, ${lit(id)}, ${lit(q.id)}, ${lit(q.identityId)}, ${jsonb(value)});`,
            );
            lastIdentity = q.identityId;
        }
        const day = iso.slice(0, 10);
        this.bump(`completion||${day}`);
        if (lastIdentity) this.bump(`reached|${lastIdentity}|${day}`);
        // Opens outnumber submissions.
        const opens = this.story.stats.opened_per_response;
        for (let k = 0; k < Math.floor(opens); k++) this.bump(`start||${day}`);
        if (this.rng.next() < opens - Math.floor(opens)) this.bump(`start||${day}`);
        for (const metric of ["browser", "device", "country"] as const) {
            const bucket = this.rng.weighted(this.story.stats[metric]);
            const key = `${metric}|${bucket}`;
            this.audience.set(key, (this.audience.get(key) ?? 0) + 1);
        }
    }

    private answer(def: QuestionDef): Value | null {
        const spec = def.answers ?? {};
        if (!def.required && spec.skip_rate && this.rng.next() < spec.skip_rate) return null;
        switch (def.type) {
            case "rating_scale":
            case "nps":
                return {
                    number: Number(this.rng.weighted(spec.weights ?? this.scaleWeights(def))),
                };
            case "yes_no":
                return { bool: this.rng.weighted(spec.weights ?? { yes: 3, no: 1 }) === "yes" };
            case "single_choice":
            case "dropdown":
                return { choice: this.rng.weighted(spec.weights ?? this.optionWeights(def)) };
            case "multiple_choice": {
                const [min, max] = spec.pick ?? [1, 2];
                const weights = { ...(spec.weights ?? this.optionWeights(def)) };
                const chosen: string[] = [];
                const n = this.rng.int(min, Math.min(max, Object.keys(weights).length));
                while (chosen.length < n) {
                    const pick = this.rng.weighted(weights);
                    chosen.push(pick);
                    delete weights[pick];
                }
                return { choices: chosen };
            }
            case "long_text":
            case "short_text": {
                const pool = spec.pool ?? [];
                if (pool.length === 0) return null;
                let queue = this.pools.get(def.key);
                if (!queue || queue.length === 0) {
                    queue = this.rng.shuffle(pool);
                    this.pools.set(def.key, queue);
                }
                return { text: queue.pop()! };
            }
        }
    }

    private scaleWeights(def: QuestionDef): Record<string, number> {
        const [min, max] = def.type === "nps" ? [0, 10] : def.scale ?? [1, 5];
        const w: Record<string, number> = {};
        for (let v = min; v <= max; v++) w[String(v)] = 1 + (v - min);
        return w;
    }

    private optionWeights(def: QuestionDef): Record<string, number> {
        const w: Record<string, number> = {};
        for (const o of def.options ?? []) w[o] = 1;
        return w;
    }

    private bump(key: string) {
        this.daily.set(key, (this.daily.get(key) ?? 0) + 1);
    }

    private counters() {
        const survey = lit(this.st.surveyId);
        for (const [key, count] of this.daily) {
            const [metric, bucket, day] = key.split("|");
            this.lines.push(
                `INSERT INTO survey_stats_daily (survey_id, metric, bucket, day, count) VALUES (${survey}, ${
                    lit(metric)
                }, ${lit(bucket)}, ${lit(day)}, ${count});`,
            );
        }
        for (const [key, count] of this.audience) {
            const [metric, bucket] = key.split("|");
            this.lines.push(
                `INSERT INTO survey_stats (survey_id, metric, bucket, count) VALUES (${survey}, ${
                    lit(metric)
                }, ${lit(bucket)}, ${count});`,
            );
        }
    }

    /**
     * The survey must predate its responses or the stats page clamps its
     * range to the survey's life. Versions are immutable by trigger; the
     * trigger is lifted only inside this transaction and only for the
     * published_at column.
     */
    private backdate() {
        const created = this.dayOf(this.story.created_days_ago);
        created.setUTCHours(10, 0, 0, 0);
        this.lines.push(
            `UPDATE surveys SET created_at = ${lit(created.toISOString())} WHERE id = ${
                lit(this.st.surveyId)
            };`,
            "ALTER TABLE survey_versions DISABLE TRIGGER survey_versions_immutable;",
        );
        for (const v of this.st.versions) {
            const at = v.number === 1
                ? new Date(created.getTime() + 3600_000)
                : new Date(this.firstResponseOf(v) - 2 * 3600_000);
            this.lines.push(
                `UPDATE survey_versions SET published_at = ${lit(at.toISOString())} WHERE id = ${
                    lit(v.id)
                };`,
            );
        }
        this.lines.push("ALTER TABLE survey_versions ENABLE TRIGGER survey_versions_immutable;");
        if (this.st.participants.length) {
            const invited = new Date(created.getTime() + 2 * 3600_000).toISOString();
            this.lines.push(
                `UPDATE participants SET created_at = ${lit(invited)}, invited_at = ${
                    lit(invited)
                } WHERE survey_id = ${lit(this.st.surveyId)};`,
            );
        }
    }

    private firstResponseOf(v: VersionRef): number {
        // Responses were emitted in version order with ascending stamps; the
        // earliest for a later version is the first INSERT naming it.
        const marker = `, ${lit(v.id)}, `;
        for (const line of this.lines) {
            if (line.startsWith("INSERT INTO responses") && line.includes(marker)) {
                const m = line.match(/'(\d{4}-\d{2}-\d{2}T[^']+)'\);$/);
                if (m) return new Date(m[1]).getTime();
            }
        }
        return this.earliest;
    }
}
