package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	githubUser         = "rlespinasse"
	defaultReadmeFile  = "README.md"
	highlightedCount   = 5
	actionSlots        = 3
	starSlots          = highlightedCount - actionSlots
	cutoffMonths       = 12
	actionSuffix       = "-action"
	highlightedStart   = "<!-- HIGHLIGHTED_PROJECTS:START -->"
	highlightedEnd     = "<!-- HIGHLIGHTED_PROJECTS:END -->"
	yearProjectsStart  = "<!-- YEAR_PROJECTS:START -->"
	yearProjectsEnd    = "<!-- YEAR_PROJECTS:END -->"
)

type repo struct {
	Name       string    `json:"name"`
	Fork       bool      `json:"fork"`
	Private    bool      `json:"private"`
	Stars      int       `json:"stargazers_count"`
	CreatedAt  time.Time `json:"created_at"`
	Description string   `json:"description"`
}

func main() {
	log.SetFlags(0)

	readmeFile := defaultReadmeFile
	if len(os.Args) > 1 {
		readmeFile = os.Args[1]
	}

	repos, err := fetchRepos()
	if err != nil {
		log.Fatalf("Failed to fetch repos: %v", err)
	}

	// Filter public non-fork repos
	var publicRepos []repo
	for _, r := range repos {
		if !r.Fork && !r.Private {
			publicRepos = append(publicRepos, r)
		}
	}

	// Identify action repos and fetch dependents
	var actionNames []string
	for _, r := range publicRepos {
		if isAction(r.Name) {
			actionNames = append(actionNames, r.Name)
		}
	}
	dependentsMap := fetchDependents(actionNames)

	// Generate sections
	highlightedContent := generateHighlighted(publicRepos, dependentsMap)
	recentContent := generateRecent(publicRepos, dependentsMap)

	// Update README
	readme, err := os.ReadFile(readmeFile)
	if err != nil {
		log.Fatalf("Failed to read %s: %v", readmeFile, err)
	}

	content := string(readme)
	content = replaceSection(content, highlightedStart, highlightedEnd, highlightedContent)
	content = replaceSection(content, yearProjectsStart, yearProjectsEnd, recentContent)

	if err := os.WriteFile(readmeFile, []byte(content), 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", readmeFile, err)
	}

	fmt.Println("README updated.")
}

func fetchRepos() ([]repo, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("users/%s/repos", githubUser),
		"--paginate",
		"--jq", ".",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w", err)
	}

	// gh api --paginate outputs multiple JSON arrays, one per page.
	// We need to concatenate them.
	var allRepos []repo
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	for decoder.More() {
		var page []repo
		if err := decoder.Decode(&page); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		allRepos = append(allRepos, page...)
	}

	return allRepos, nil
}

func fetchDependents(repoNames []string) map[string]int {
	result := make(map[string]int)
	if len(repoNames) == 0 {
		return result
	}

	args := make([]string, 0, len(repoNames)+1)
	args = append(args, "dependents")
	for _, name := range repoNames {
		args = append(args, fmt.Sprintf("%s/%s", githubUser, name))
	}

	cmd := exec.Command("ghat", args...)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("Warning: ghat dependents failed: %v", err)
		return result
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		fullRepo := strings.TrimSpace(parts[0])
		countStr := strings.TrimSpace(parts[2])

		// Extract repo name from "owner/repo"
		repoName := fullRepo
		if idx := strings.Index(fullRepo, "/"); idx >= 0 {
			repoName = fullRepo[idx+1:]
		}

		var count int
		if _, err := fmt.Sscanf(countStr, "%d", &count); err == nil {
			result[repoName] = count
		}
	}

	return result
}

func isAction(name string) bool {
	return strings.HasSuffix(name, actionSuffix)
}

func generateHighlighted(repos []repo, dependentsMap map[string]int) string {
	// Tier 1: Top action repos by dependents count
	var actionRepos []repo
	for _, r := range repos {
		if isAction(r.Name) {
			actionRepos = append(actionRepos, r)
		}
	}
	sort.Slice(actionRepos, func(i, j int) bool {
		return dependentsMap[actionRepos[i].Name] > dependentsMap[actionRepos[j].Name]
	})
	if len(actionRepos) > actionSlots {
		actionRepos = actionRepos[:actionSlots]
	}

	// Track which repos are already selected
	selected := make(map[string]bool)
	for _, r := range actionRepos {
		selected[r.Name] = true
	}

	// Tier 2: Top remaining repos by stars
	var remaining []repo
	for _, r := range repos {
		if !selected[r.Name] {
			remaining = append(remaining, r)
		}
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].Stars > remaining[j].Stars
	})
	if len(remaining) > starSlots {
		remaining = remaining[:starSlots]
	}

	// Combine: actions first, then by stars
	highlighted := append(actionRepos, remaining...)

	return buildTable(highlighted, dependentsMap)
}

func generateRecent(repos []repo, dependentsMap map[string]int) string {
	cutoff := time.Now().UTC().AddDate(0, -cutoffMonths, 0)

	var recent []repo
	for _, r := range repos {
		if r.CreatedAt.After(cutoff) {
			recent = append(recent, r)
		}
	}

	// Sort by creation date descending
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].CreatedAt.After(recent[j].CreatedAt)
	})

	return buildTable(recent, dependentsMap)
}

func buildTable(repos []repo, dependentsMap map[string]int) string {
	var sb strings.Builder
	sb.WriteString("| Project | Description | Created |\n")
	sb.WriteString("|---------|-------------|---------|")

	for _, r := range repos {
		sb.WriteString("\n")
		sb.WriteString(formatRow(r, dependentsMap[r.Name]))
	}

	return sb.String()
}

func formatRow(r repo, dependents int) string {
	description := strings.ReplaceAll(r.Description, "|", `\|`)
	created := r.CreatedAt.Format("January 2006")

	badges := fmt.Sprintf("![Stars](https://img.shields.io/github/stars/%s/%s?style=flat-square&color=58a6ff)",
		githubUser, r.Name)

	if dependents > 0 {
		badges += fmt.Sprintf(" [![Dependents](https://img.shields.io/badge/used%%20by-%d-58a6ff?style=flat-square)](https://github.com/%s/%s/network/dependents)",
			dependents, githubUser, r.Name)
	}

	return fmt.Sprintf("| [**%s**](https://github.com/%s/%s) | %s %s | %s |",
		r.Name, githubUser, r.Name, badges, description, created)
}

func replaceSection(content, startMarker, endMarker, newContent string) string {
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)

	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		log.Printf("Warning: markers not found for %s", startMarker)
		return content
	}

	return content[:startIdx+len(startMarker)] + "\n" + newContent + "\n" + content[endIdx:]
}
