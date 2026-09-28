// reset: the demo accounts start from nothing, and so does out/.
//
// Only the story accounts are touched. Their surveys are soft-deleted the
// way the product does it (the purge job erases them later), their spent
// magic-link tokens go so the per-address hourly cap cannot block a rerun,
// and their export jobs go so the account page shows a fresh build.

import { AUTH_DIR, RESULTS_DIR, SHOTS_DIR } from "../config.ts";
import { lit, psql } from "../compose.ts";
import type { Surveys } from "../content.ts";
import { clearMailbox } from "../mailpit.ts";
import { removeState } from "../state.ts";

export async function reset(surveys: Surveys) {
    const emails = Object.values(surveys.stories).map((s) => s.email);
    const list = emails.map(lit).join(", ");
    await psql(`
BEGIN;
UPDATE surveys SET deleted_at = now()
 WHERE deleted_at IS NULL
   AND workspace_id IN (
     SELECT m.workspace_id FROM workspace_members m
       JOIN users u ON u.id = m.user_id
      WHERE u.email IN (${list}) AND u.deleted_at IS NULL);
DELETE FROM magic_link_tokens WHERE email IN (${list});
DELETE FROM export_jobs WHERE workspace_id IN (
     SELECT m.workspace_id FROM workspace_members m
       JOIN users u ON u.id = m.user_id
      WHERE u.email IN (${list}) AND u.deleted_at IS NULL);
COMMIT;
`);
    await clearMailbox();
    await removeState();
    for (const dir of [SHOTS_DIR, AUTH_DIR, RESULTS_DIR]) {
        await Deno.remove(dir, { recursive: true }).catch(() => {});
    }
    console.log(`reset: ${emails.length} accounts cleared, inbox emptied, out/ wiped`);
}
