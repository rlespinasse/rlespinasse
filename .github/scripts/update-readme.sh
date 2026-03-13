#!/usr/bin/env bash
set -euo pipefail

GITHUB_USER="rlespinasse"
README_FILE="README.md"
HIGHLIGHTED_COUNT=5

# Cutoff date: 12 months ago (ISO 8601)
CUTOFF_DATE=$(date -u -d '12 months ago' '+%Y-%m-%dT%H:%M:%SZ')

generate_table_row() {
  local repo="$1"
  local description="$2"
  # Sanitize pipe characters in descriptions to avoid breaking markdown tables
  description="${description//|/\\|}"
  echo "| [**${repo}**](https://github.com/${GITHUB_USER}/${repo}) | ![Stars](https://img.shields.io/github/stars/${GITHUB_USER}/${repo}?style=flat-square&color=58a6ff) ${description} |"
}

generate_table_row_with_month() {
  local repo="$1"
  local description="$2"
  local created_at="$3"
  description="${description//|/\\|}"
  # Extract month and year from created_at (e.g. 2026-03-12T... -> March 2026)
  local created
  created=$(date -u -d "$created_at" '+%B %Y')
  echo "| [**${repo}**](https://github.com/${GITHUB_USER}/${repo}) | ![Stars](https://img.shields.io/github/stars/${GITHUB_USER}/${repo}?style=flat-square&color=58a6ff) ${description} | ${created} |"
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

update_recent_projects() {
  local tmpfile
  tmpfile=$(mktemp)

  # Fetch public non-fork repos created in the last 12 months
  # Output: created_at<TAB>repo_name<TAB>description
  gh api "users/${GITHUB_USER}/repos" \
    --paginate \
    --jq ".[] | select(.fork == false and .private == false and (.created_at >= \"${CUTOFF_DATE}\")) | [.created_at, .name, (.description // \"\")] | @tsv" \
    > "$tmpfile"

  local table_content
  table_content="| Project | Description | Created |
|---------|-------------|---------|"

  # Sort by creation date descending (newest first)
  while IFS=$'\t' read -r created_at repo description; do
    table_content="${table_content}
$(generate_table_row_with_month "$repo" "$description" "$created_at")"
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

echo "Updating Recent Projects (created in the last 12 months, newest first)..."
year_content=$(update_recent_projects)
replace_section "<!-- YEAR_PROJECTS:START -->" "<!-- YEAR_PROJECTS:END -->" "$year_content" "$README_FILE"

echo "README updated."
