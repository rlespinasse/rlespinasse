#!/usr/bin/env bash
set -euo pipefail

GITHUB_USER="rlespinasse"
README_FILE="README.md"
TARGET_YEAR="2026"
HIGHLIGHTED_COUNT=5

generate_table_row() {
  local repo="$1"
  local description="$2"
  # Sanitize pipe characters in descriptions to avoid breaking markdown tables
  description="${description//|/\\|}"
  echo "| [**${repo}**](https://github.com/${GITHUB_USER}/${repo}) | ![Stars](https://img.shields.io/github/stars/${GITHUB_USER}/${repo}?style=flat-square&color=58a6ff) ${description} |"
}

update_highlighted_projects() {
  local tmpfile
  tmpfile=$(mktemp)

  # Fetch all public non-fork repos, output: stars<TAB>repo_name<TAB>description
  gh api "users/${GITHUB_USER}/repos" \
    --paginate \
    --jq '.[] | select(.fork == false and .private == false) | [(.stargazers_count | tostring), .name, (.description // "")] | @tsv' \
    > "$tmpfile"

  local table_content
  table_content="| Project | Description |
|---------|-------------|"

  # Sort by stars descending, take top N
  while IFS=$'\t' read -r stars repo description; do
    table_content="${table_content}
$(generate_table_row "$repo" "$description")"
  done < <(sort -t$'\t' -k1 -nr "$tmpfile" | head -n "$HIGHLIGHTED_COUNT")

  rm "$tmpfile"
  echo "$table_content"
}

update_year_projects() {
  local tmpfile
  tmpfile=$(mktemp)

  # Fetch public non-fork repos created in TARGET_YEAR
  # Output: created_at<TAB>repo_name<TAB>description
  gh api "users/${GITHUB_USER}/repos" \
    --paginate \
    --jq ".[] | select(.fork == false and .private == false and (.created_at | startswith(\"${TARGET_YEAR}\"))) | [.created_at, .name, (.description // \"\")] | @tsv" \
    > "$tmpfile"

  local table_content
  table_content="| Project | Description |
|---------|-------------|"

  # Sort by creation date descending (newest first)
  while IFS=$'\t' read -r created_at repo description; do
    table_content="${table_content}
$(generate_table_row "$repo" "$description")"
  done < <(sort -t$'\t' -k1 -r "$tmpfile")

  rm "$tmpfile"
  echo "$table_content"
}

replace_section() {
  local start_marker="$1"
  local end_marker="$2"
  local new_content="$3"
  local file="$4"

  awk -v start="$start_marker" -v end="$end_marker" -v content="$new_content" '
    $0 == start { print; printf "%s\n", content; skip=1; next }
    $0 == end   { print; skip=0; next }
    !skip       { print }
  ' "$file" > "${file}.tmp" && mv "${file}.tmp" "$file"
}

# --- Main ---
echo "Updating Highlighted Projects (top ${HIGHLIGHTED_COUNT} by stars)..."
highlighted_content=$(update_highlighted_projects)
replace_section "<!-- HIGHLIGHTED_PROJECTS:START -->" "<!-- HIGHLIGHTED_PROJECTS:END -->" "$highlighted_content" "$README_FILE"

echo "Updating ${TARGET_YEAR} Projects (sorted by creation date, newest first)..."
year_content=$(update_year_projects)
replace_section "<!-- YEAR_PROJECTS:START -->" "<!-- YEAR_PROJECTS:END -->" "$year_content" "$README_FILE"

echo "README updated."
