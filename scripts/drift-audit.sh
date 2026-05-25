#!/usr/bin/env bash
# Dockyard drift-audit — mechanical design-coherence checks.
#   - AGENTS.md == CLAUDE.md (the verbatim mirror invariant)
#   - required hygiene files exist
#   - every docs/plans/phase-NN-*.md has a matching scripts/smoke/phase-NN.sh
#   - every `RFC §N.M` reference in a phase plan resolves to a real RFC heading
#   - every `brief NN` reference resolves to a docs/research/NN-*.md file
#   - AGENTS.md §19 hygiene: every `dockyard` CLI verb is referenced from a
#     shipped Agent Skill (skills/) or the docs site (docs/site/); every
#     shipped template has a docs/site/ walkthrough; every SKILL.md parses
#     against the agentskills.io spec via internal/skillcheck (D-138).
# Exits non-zero on any failure.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { echo "DRIFT: $*"; fail=1; }

# 1. Mirror invariant.
if ! diff -q AGENTS.md CLAUDE.md >/dev/null 2>&1; then
  note "AGENTS.md and CLAUDE.md differ"
fi

# 2. Required files.
for f in RFC-001-Dockyard.md README.md LICENSE Makefile \
         docs/decisions.md docs/glossary.md docs/research/INDEX.md \
         docs/plans/_template.md; do
  [ -f "$f" ] || note "required file missing: $f"
done

# 3. Phase plan <-> smoke script pairing.
# The phase id allows digits, a lowercase-letter suffix (10a) and a dotted
# suffix (21.5) — a lettered/dotted suffix inserts work without renumbering.
if compgen -G "docs/plans/phase-*.md" >/dev/null; then
  for p in docs/plans/phase-*.md; do
    nn=$(basename "$p" | sed -E 's/^phase-([0-9a-z.]+)-.*/\1/')
    [ -f "scripts/smoke/phase-${nn}.sh" ] || \
      note "phase plan $p has no scripts/smoke/phase-${nn}.sh"
  done
fi

# 4. RFC section references in phase plans resolve.
if compgen -G "docs/plans/phase-*.md" >/dev/null; then
  refs=$(grep -hoE 'RFC §[0-9]+(\.[0-9]+)*' docs/plans/phase-*.md 2>/dev/null \
         | sed -E 's/RFC §//' | sort -u)
  for r in $refs; do
    grep -qE "^#+ ${r}(\.| |\b)" RFC-001-Dockyard.md \
      || note "phase plan cites RFC §${r} — no matching RFC heading"
  done
fi

# 5. Brief references resolve.
if compgen -G "docs/plans/phase-*.md" >/dev/null; then
  briefs=$(grep -hoE 'brief [0-9]+' docs/plans/phase-*.md 2>/dev/null \
           | sed -E 's/brief 0*//' | sort -u)
  for b in $briefs; do
    bb=$(printf '%02d' "$b")
    compgen -G "docs/research/${bb}-*.md" >/dev/null \
      || note "phase plan cites brief ${b} — no docs/research/${bb}-*.md"
  done
fi

# 6. AGENTS.md §19 hygiene — the skills + docs surfaces stay in sync with
#    the shipped CLI + templates (Phase 29; D-138). The check is skipped
#    until skills/ and docs/site/ land, mirroring how earlier hooks
#    no-op against unbuilt surfaces.
if [ -d skills ] && [ -d docs/site ]; then
  # 6a. Every SKILL.md parses against the agentskills.io spec.
  if command -v go >/dev/null 2>&1; then
    if ! go run ./internal/skillcheck/cmd/skillcheck skills >/dev/null 2>&1; then
      go run ./internal/skillcheck/cmd/skillcheck skills 2>&1 | sed 's/^/  /'
      note "skills/: one or more SKILL.md files are malformed"
    fi
  else
    echo "  note: 'go' not on PATH — skipping skillcheck validation"
  fi

  # 6b. Every dockyard CLI verb has a referencing skill or docs page.
  # Verbs are the constructor functions registered onto root in
  # internal/cli/root.go (newXxxCmd). The grep finds them with a
  # tight regex so a new verb shows up automatically.
  if [ -f internal/cli/root.go ]; then
    verbs=$(grep -hoE 'root\.AddCommand\(new[A-Z][A-Za-z]+Cmd' internal/cli/root.go \
            | sed -E 's/root\.AddCommand\(new([A-Z][A-Za-z]+)Cmd/\1/' \
            | tr '[:upper:]' '[:lower:]' | sort -u)
    for v in $verbs; do
      # Each verb must appear in either a SKILL.md or a docs/site/ page.
      # The `dockyard <verb>` token is what the skills and docs use; the
      # check is intentionally exact so a typo fails fast.
      if ! grep -rqsE "dockyard ${v}\b" skills/ docs/site/; then
        note "AGENTS.md §19: 'dockyard ${v}' has no skill or docs reference"
      fi
    done
  fi

  # 6c. Every shipped template has a docs/site/ walkthrough.
  if [ -d templates ]; then
    for t in templates/*/; do
      name=$(basename "$t")
      [ "$name" = "_template" ] && continue
      # builtin.go is the canonical "this template is shipped" marker:
      # internal/scaffold/builtin.go imports the template's builtin.go
      # to register it via init(). A directory without one is in-flight
      # work, not a shipped template.
      [ -f "${t}builtin.go" ] || continue
      if [ ! -f "docs/site/getting-started/${name}.md" ]; then
        note "AGENTS.md §19: template '${name}' has no docs/site/getting-started/${name}.md walkthrough"
      fi
    done
  fi

  # 6d. Every shipped example has a README the docs site links to (Phase
  #     28, D-153). An example without a README is in-flight work or
  #     cruft; an example whose README is not linked from the docs site
  #     is unreachable from a developer's first read of the published
  #     docs. The check enforces both.
  #
  #     "Shipped" here is "the example has a cmd/server entrypoint" —
  #     mirrors the templates' builtin.go marker. examples/customer-
  #     health is the manifest reference fixture (consumed by
  #     test/integration/wave2_test.go) and is intentionally not a
  #     buildable example, so it is exempt: no cmd/server, no check.
  if [ -d examples ]; then
    for e in examples/*/; do
      name=$(basename "$e")
      [ -d "${e}cmd/server" ] || continue
      if [ ! -f "${e}README.md" ]; then
        note "AGENTS.md §19: example '${name}' has no examples/${name}/README.md"
      fi
      # The examples index page is the docs-site link target — a single
      # canonical reference for the example set. It must mention the
      # example by slug so a new example without a docs link fails fast.
      if ! grep -qsE "examples/${name}\b" docs/site/getting-started/examples.md 2>/dev/null; then
        note "AGENTS.md §19: example '${name}' is not referenced from docs/site/getting-started/examples.md"
      fi
    done
  fi

  # 6e. User-facing surfaces must not carry Dockyard's internal phase
  #     vocabulary. "Phase N" prose ("Phase 24 shipped X", "added in
  #     Phase 28") refers to Dockyard's internal build methodology and
  #     tells a user nothing about what the framework does. The check
  #     scans every user-facing surface for the prose pattern and fails
  #     on a hit. Contributor-facing paths (RFC, plans, decisions,
  #     glossary, design-spec, CONVENTIONS, AGENTS.md/CLAUDE.md,
  #     Makefile, workflows, internal/) are intentionally excluded —
  #     phase wording is the right vocabulary there.
  #
  #     The regex matches capital-P prose only ('\bPhase [0-9]+\b').
  #     The lowercase `phase-NN` form is path-shaped (screenshot
  #     directories, smoke-script filenames) and is filesystem
  #     metadata, not user-facing prose — those are intentionally not
  #     flagged.
  user_facing_md=(README.md CHANGELOG.md)
  for d in docs/site examples templates; do
    [ -d "$d" ] || continue
  done
  # Build the user-facing file set explicitly; do not glob the build
  # artefact tree (docs/site/.vitepress/dist/) or config files.
  user_facing_files=()
  [ -f README.md ] && user_facing_files+=(README.md)
  [ -f CHANGELOG.md ] && user_facing_files+=(CHANGELOG.md)
  if [ -d docs/site ]; then
    while IFS= read -r f; do
      user_facing_files+=("$f")
    done < <(find docs/site -type f -name '*.md' \
      -not -path 'docs/site/.vitepress/dist/*' \
      -not -path 'docs/site/node_modules/*' \
      | sort)
  fi
  if [ -d examples ]; then
    while IFS= read -r f; do
      user_facing_files+=("$f")
    done < <(find examples -mindepth 2 -maxdepth 2 -type f -name 'README.md' | sort)
  fi
  if [ -d templates ]; then
    while IFS= read -r f; do
      user_facing_files+=("$f")
    done < <(find templates -mindepth 2 -maxdepth 2 -type f -name 'README.md.tmpl' | sort)
  fi
  for f in "${user_facing_files[@]}"; do
    if grep -nE '\bPhase [0-9]+\b' "$f" >/dev/null 2>&1; then
      while IFS= read -r hit; do
        note "AGENTS.md §19 (user-facing vocabulary): ${f}: ${hit}"
      done < <(grep -nE '\bPhase [0-9]+\b' "$f")
    fi
  done

  # 6f. Template README.md.tmpl files scaffold into user projects.
  #     A scaffolded user's project README should be 100% about that
  #     project, not Dockyard's internal decision numbering. D-NNN
  #     citations are acceptable in docs/site/ and examples/ (they
  #     cross-link the public decisions reference page); they are not
  #     acceptable in templates/*/README.md.tmpl.
  if [ -d templates ]; then
    while IFS= read -r f; do
      if grep -nE 'D-[0-9]{3}' "$f" >/dev/null 2>&1; then
        while IFS= read -r hit; do
          note "AGENTS.md §19 (template-README D-NNN): ${f}: ${hit}"
        done < <(grep -nE 'D-[0-9]{3}' "$f")
      fi
    done < <(find templates -mindepth 2 -maxdepth 2 -type f -name 'README.md.tmpl' | sort)
  fi
elif [ -d skills ] && [ ! -d docs/site ]; then
  note "skills/ exists but docs/site/ does not — §19 requires both"
elif [ ! -d skills ] && [ -d docs/site ]; then
  note "docs/site/ exists but skills/ does not — §19 requires both"
fi

if [ "$fail" -ne 0 ]; then
  echo "DRIFT-AUDIT FAILED"
  exit 1
fi
echo "DRIFT-AUDIT OK"
