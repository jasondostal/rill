#!/usr/bin/env bash
#
# pii-guard — catch personal/internal references before they ship public.
#
# Why this exists: secret scanners (gitleaks, trufflehog, GitHub) hunt
# high-entropy strings — API keys, tokens. They are BLIND to the leak that
# actually hurts: your own life hardcoded as sample data. A sample memory like
# "<coworker> prefers people-first leadership" is a grammatical sentence,
# invisible to entropy. This guard greps tracked/staged files against a denylist
# of YOUR real entities (people, employers, internal hostnames, old projects).
#
# (Note: this script is deliberately written with ZERO real names — a PII guard
# that contains PII would trip itself. Keep it that way.)
#
# THE CARDINAL RULE (learned the hard way): the denylist is the single most
# sensitive file in the repo — it is a PII index. It must NEVER be tracked.
# This script (a) resolves the denylist from OUTSIDE the repo by default, and
# (b) refuses to run clean if a denylist file is staged or tracked. Discipline
# failed once; structure won't.
#
# Usage:
#   scripts/pii-guard.sh              # scan tracked files (working tree)
#   scripts/pii-guard.sh --staged     # scan the index (use in pre-commit)
#   scripts/pii-guard.sh --require-list   # fail if no denylist found (use in CI)
#   scripts/pii-guard.sh --quiet      # withhold patterns + matched content from
#                                     # output, print only file paths (use in CI:
#                                     # Actions logs are world-readable when public)
#
# Denylist resolution (all that exist are merged, so a global list grows
# across every repo while a repo adds its own specifics):
#   1. $PII_GUARD_DENYLIST                      (explicit path / CI secret file)
#   2. <repo>/.pii-denylist                     (gitignored — repo-specific terms)
#   3. ${XDG_CONFIG_HOME:-~/.config}/pii-guard/denylist   (your shared global list)
#
# Denylist format: one extended-regex (ERE) pattern per line, matched
# case-insensitively by default. Prefix a line with 'cs:' to match it
# case-SENSITIVELY — essential for ALLCAPS hostnames that collide with common
# lowercase words (an ALLCAPS hostname vs. a like-spelled common word). '#' comments and blank
# lines ignored. Examples:
#   Firstname
#   Lastname
#   yourdomain\.example
#   cs:\b(HOSTA|HOSTB)\b
#   192\.168\.
#
# Allowlist (intentional matches, e.g. your contact email in LICENSE): an
# OPTIONAL tracked <repo>/.pii-allowlist of ERE patterns matched against the
# "file:line:content" hit lines; any match is excluded. Keep it tiny.

set -euo pipefail

STAGED=0
REQUIRE_LIST=0
QUIET=0
for arg in "$@"; do
  case "$arg" in
    --staged)       STAGED=1 ;;
    --require-list) REQUIRE_LIST=1 ;;
    --quiet)        QUIET=1 ;;
    -h|--help)      sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "pii-guard: unknown arg: $arg" >&2; exit 2 ;;
  esac
done

cd "$(git rev-parse --show-toplevel)"
REPO_ROOT="$(pwd)"

red()  { printf '\033[31m%s\033[0m\n' "$*"; }
grn()  { printf '\033[32m%s\033[0m\n' "$*"; }
ylw()  { printf '\033[33m%s\033[0m\n' "$*"; }

# --- Self-protection: the denylist must never be tracked or staged ----------
# This is the whole point. A committed denylist IS the leak.
TRACKED_LISTS="$(git ls-files | grep -E '(^|/)\.pii-denylist$|(^|/)pii-denylist$' || true)"
STAGED_LISTS="$(git diff --cached --name-only | grep -E '(^|/)\.pii-denylist$|(^|/)pii-denylist$' || true)"
if [ -n "$TRACKED_LISTS$STAGED_LISTS" ]; then
  red "✗ FATAL: a denylist file is tracked or staged — that file IS the leak."
  [ -n "$TRACKED_LISTS" ] && printf '   tracked: %s\n' $TRACKED_LISTS
  [ -n "$STAGED_LISTS" ]  && printf '   staged:  %s\n' $STAGED_LISTS
  red "   Remove it from git: git rm --cached <file>  (and add to .gitignore)."
  red "   The denylist belongs OUTSIDE the repo. See script header."
  exit 1
fi

# --- Resolve + merge denylists ----------------------------------------------
GLOBAL_LIST="${XDG_CONFIG_HOME:-$HOME/.config}/pii-guard/denylist"
SOURCES=()
[ -n "${PII_GUARD_DENYLIST:-}" ] && [ -f "$PII_GUARD_DENYLIST" ] && SOURCES+=("$PII_GUARD_DENYLIST")
[ -f "$REPO_ROOT/.pii-denylist" ] && SOURCES+=("$REPO_ROOT/.pii-denylist")
[ -f "$GLOBAL_LIST" ] && SOURCES+=("$GLOBAL_LIST")

if [ "${#SOURCES[@]}" -eq 0 ]; then
  ylw "⚠ pii-guard: no denylist found. Nothing to scan against."
  echo "   Set one up (it must live OUTSIDE the repo):"
  echo "     mkdir -p \"$(dirname "$GLOBAL_LIST")\" && \$EDITOR \"$GLOBAL_LIST\""
  echo "   Or point at one: export PII_GUARD_DENYLIST=/path/to/denylist"
  echo "   Format example: see .pii-denylist.sample in this repo."
  if [ "$REQUIRE_LIST" -eq 1 ]; then
    red "✗ --require-list set and no denylist available — failing closed."
    exit 1
  fi
  exit 0
fi

# Collect patterns (strip comments/blanks), dedup.
PATTERNS="$(cat "${SOURCES[@]}" | sed -e 's/#.*$//' -e 's/[[:space:]]*$//' | grep -v '^[[:space:]]*$' | sort -u || true)"
if [ -z "$PATTERNS" ]; then
  ylw "⚠ pii-guard: denylist(s) found but empty after stripping comments."
  [ "$REQUIRE_LIST" -eq 1 ] && { red "✗ failing closed."; exit 1; }
  exit 0
fi

# --- Build allowlist filter (intentional matches) ---------------------------
ALLOW="$REPO_ROOT/.pii-allowlist"
allow_filter() {
  if [ -f "$ALLOW" ]; then
    local pats
    pats="$(sed -e 's/#.*$//' -e 's/[[:space:]]*$//' "$ALLOW" | grep -v '^[[:space:]]*$' || true)"
    if [ -n "$pats" ]; then grep -vEf <(printf '%s\n' "$pats"); return; fi
  fi
  cat
}

# --- Scan -------------------------------------------------------------------
# git grep only ever sees tracked (or --cached/staged) files, so node_modules
# and untracked junk are out of scope by construction. Skip lockfiles for noise.
BASE_ARGS=(-nIE)
SCOPE_DESC="tracked working-tree files"
if [ "$STAGED" -eq 1 ]; then BASE_ARGS=(--cached "${BASE_ARGS[@]}"); SCOPE_DESC="staged (index) files"; fi
EXCLUDES=(':(exclude)*-lock.json' ':(exclude)*.sum' ':(exclude)*.pii-denylist*' ':(exclude)*.pii-allowlist')

echo "pii-guard: scanning $SCOPE_DESC against $(printf '%s\n' "$PATTERNS" | wc -l | tr -d ' ') denylist pattern(s)"

HITS=0
TMP="$(mktemp)"; HITS_FILES="$(mktemp)"; trap 'rm -f "$TMP" "$HITS_FILES"' EXIT
while IFS= read -r pat; do
  [ -z "$pat" ] && continue
  # 'cs:' prefix → case-sensitive; otherwise case-insensitive (-i).
  GREP_ARGS=("${BASE_ARGS[@]}")
  if [ "${pat#cs:}" != "$pat" ]; then pat="${pat#cs:}"; else GREP_ARGS+=(-i); fi
  # -e "$pat" so leading dashes / regex are taken literally as the pattern.
  if git grep "${GREP_ARGS[@]}" -e "$pat" -- . "${EXCLUDES[@]}" 2>/dev/null \
       | allow_filter > "$TMP"; then
    if [ -s "$TMP" ]; then
      # --quiet (CI): never echo the pattern or the matched content — both reveal
      # the denylist, and Actions logs are world-readable on a public repo. Only
      # accumulate file paths for a safe summary. Verbose (local): show details.
      if [ "$QUIET" -eq 0 ]; then
        red "✗ denylist match: /$pat/"
        sed 's/\(.\{160\}\).*/\1 …/' "$TMP" | sed 's/^/    /'  # truncate long lines
      fi
      cut -d: -f1 "$TMP" >> "$HITS_FILES"
      HITS=$((HITS + $(wc -l < "$TMP")))
    fi
  fi
done <<< "$PATTERNS"

# --- Archive blind-spot warning --------------------------------------------
# The scan is text-only (git grep -I skips binaries), so an archive can smuggle
# denylisted content straight past it. Surface any tracked/staged archive so a
# human eyeballs it — this is exactly how a stray zip slips into a public repo.
if [ "$STAGED" -eq 1 ]; then ARCHIVES="$(git diff --cached --name-only)"; else ARCHIVES="$(git ls-files)"; fi
ARCHIVES="$(printf '%s\n' "$ARCHIVES" | grep -iE '\.(zip|tar|tgz|tar\.gz|gz|bz2|xz|7z|rar|jar|war)$' || true)"
if [ -n "$ARCHIVES" ]; then
  echo
  ylw "⚠ pii-guard: archive file(s) present — the text scan CANNOT see inside these:"
  printf '%s\n' "$ARCHIVES" | sed 's/^/    /'
  ylw "   Inspect or remove them before publishing (unzip + rescan, or git rm --cached)."
fi

echo
if [ "$HITS" -gt 0 ]; then
  if [ "$QUIET" -eq 1 ]; then
    red "✗ pii-guard: $HITS line(s) matched the denylist, in these file(s):"
    sort -u "$HITS_FILES" | sed 's/^/    /'
    echo "   Patterns and matched lines are withheld here (this log may be public)."
    echo "   Run 'scripts/pii-guard.sh' locally, with your denylist, to see details."
  else
    red "✗ pii-guard: $HITS line(s) matched the denylist. Scrub before publishing."
    echo "   False positive? Add a narrow pattern to .pii-allowlist (tracked, safe)."
  fi
  exit 1
fi
grn "✓ pii-guard: clean — no denylisted references in $SCOPE_DESC."
