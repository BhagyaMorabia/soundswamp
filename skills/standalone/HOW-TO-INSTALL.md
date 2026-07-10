# Vibecoder Audit Skills — Install & Use (Claude Code)

Three drop-in skills for [Claude Code](https://claude.com/claude-code). Point Claude at your app and it works through **100 expert prompts, one at a time, fully hands-free** — committing every step on its own branch so you can review and merge.

| File | Command you'll type | What it does |
| :-- | :-- | :-- |
| `security-audit.md` | `/security-audit` | 100 prompts: auth, access control, injection, secrets, data exposure |
| `performance-audit.md` | `/performance-audit` | 100 prompts: rendering, bundles, network, caching, database |
| `ui-audit.md` | `/ui-audit` | 100 prompts: layout, typography, spacing, motion, accessibility |

Each file is **completely self-contained** — all 100 prompts are baked in. No other files, no config, no API keys.

---

## Step 1 — Download the three files

Download these three files (e.g. from wherever this is hosted — the green **Code → Download** button on GitHub, or "Download raw file" on each):

- `security-audit.md`
- `performance-audit.md`
- `ui-audit.md`

Keep their names exactly as-is — **the filename becomes the command** (`security-audit.md` → `/security-audit`).

---

## Step 2 — Put them in your Claude Code commands folder

This is the easiest method: drop the three files into your personal commands folder and they're available in **every** project.

**macOS / Linux**

```bash
mkdir -p ~/.claude/commands
mv ~/Downloads/security-audit.md ~/Downloads/performance-audit.md ~/Downloads/ui-audit.md ~/.claude/commands/
```

**Windows (PowerShell)**

```powershell
New-Item -ItemType Directory -Force "$HOME\.claude\commands"
Move-Item "$HOME\Downloads\security-audit.md","$HOME\Downloads\performance-audit.md","$HOME\Downloads\ui-audit.md" "$HOME\.claude\commands\"
```

> Prefer just one project to have them? Put the files in `.claude/commands/` **inside that project** instead of your home folder, and commit them so your team gets them too.

---

## Step 3 — Restart Claude Code

Start a new Claude Code session (or restart it). Type `/` and you should see `/security-audit`, `/performance-audit`, and `/ui-audit` in the list. You can also ask **"what skills are available?"** to confirm.

---

## Step 4 — Run one

Open Claude Code **in the project you want to improve**, then run one of:

```text
/security-audit
/performance-audit
/ui-audit
```

Then walk away. The skill will:

1. **Create a dedicated branch** (e.g. `vibecoder/security`) so your `main` is never touched.
2. Work through all 100 prompts **in order, one at a time** — investigating, applying the fix, and lightly verifying each.
3. **Commit after every prompt** (`security 1: …`, `security 2: …`) so each step is a separate, revertible checkpoint.
4. Track progress in `.claude/vibecoder/<category>-progress.md` so it **resumes automatically** if the session is interrupted or its context fills up.
5. Print a summary when all 100 are done and hand the branch back to you to review and merge.

It **won't stop to ask questions** mid-run. Prompts that don't apply to your stack are marked N/A and skipped, and it keeps going until the list is finished.

---

## Resuming an interrupted run

If you stop it or close your laptop, just run the **same command again in the same project**. It reads its progress file and git history and picks up at the next unchecked prompt.

## Reviewing the work

When it finishes (or any time), review what it did:

```bash
git log --oneline vibecoder/security        # see every step
git diff main..vibecoder/security           # see the whole change
```

Merge it when you're happy: `git checkout main && git merge vibecoder/security`. Or cherry-pick individual commits. Or throw the branch away — your `main` was never touched.

---

## Alternative install (skill folder instead of command file)

If you'd rather install them as formal "skills," put each file in its own folder named after the command, renamed to `SKILL.md`:

```text
~/.claude/skills/security-audit/SKILL.md
~/.claude/skills/performance-audit/SKILL.md
~/.claude/skills/ui-audit/SKILL.md
```

Both methods produce the exact same `/command` and behave identically — the command folder is just fewer steps.

## Uninstall

Delete the files you added (`~/.claude/commands/security-audit.md`, etc.) and restart Claude Code.

---

## Safety notes

- Runs on a **dedicated branch**; your working branch and `main` are left alone.
- **One commit per prompt**, so anything can be reverted individually.
- Never force-pushes, never deletes unrelated code — you review and merge.
- Recommended for a first run: try it on a branch of a project you can afford to experiment with. 100 automated changes is powerful; the per-commit checkpoints make it safe to review, but you stay in control of what merges.
