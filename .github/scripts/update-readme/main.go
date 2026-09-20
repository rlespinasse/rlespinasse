package main

import (
	"bufio"
	"bytes"
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
	githubUser        = "rlespinasse"
	defaultReadmeFile = "README.md"
	cutoffMonths      = 6
	highlightedStart  = "<!-- HIGHLIGHTED_PROJECTS:START -->"
	highlightedEnd    = "<!-- HIGHLIGHTED_PROJECTS:END -->"
	yearProjectsStart = "<!-- YEAR_PROJECTS:START -->"
	yearProjectsEnd   = "<!-- YEAR_PROJECTS:END -->"
)

type repo struct {
	Name        string    `json:"name"`
	Fork        bool      `json:"fork"`
	Private     bool      `json:"private"`
	Archived    bool      `json:"archived"`
	Stars       int       `json:"stargazers_count"`
	CreatedAt   time.Time `json:"created_at"`
	Description string    `json:"description"`
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

	// Identify action repos dynamically by checking for action.yml / action.yaml
	actionNames, err := detectActionRepos(publicRepos)
	if err != nil {
		log.Printf("Warning: failed to detect action repos via GraphQL: %v", err)
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

// detectActionRepos queries GitHub GraphQL API to check if action.yml or action.yaml exists at the repository root
func detectActionRepos(repos []repo) ([]string, error) {
	if len(repos) == 0 {
		return nil, nil
	}

	var queryBuilder strings.Builder
	queryBuilder.WriteString("query {")
	for i, r := range repos {
		alias := fmt.Sprintf("repo_%d", i)
		queryBuilder.WriteString(fmt.Sprintf(`
			%s: repository(owner: "%s", name: "%s") {
				name
				actionYml: object(expression: "HEAD:action.yml") { id }
				actionYaml: object(expression: "HEAD:action.yaml") { id }
			}`, alias, githubUser, r.Name))
	}
	queryBuilder.WriteString("}")

	payload := map[string]string{"query": queryBuilder.String()}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("gh", "api", "graphql", "--input", "-")
	cmd.Stdin = bytes.NewReader(jsonBody)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
	}

	var gqlResp struct {
		Data map[string]struct {
			Name       string    `json:"name"`
			ActionYml  *struct{} `json:"actionYml"`
			ActionYaml *struct{} `json:"actionYaml"`
		} `json:"data"`
	}

	if err := json.Unmarshal(out, &gqlResp); err != nil {
		return nil, fmt.Errorf("decode graphql response: %w", err)
	}

	var actionRepos []string
	for _, repoData := range gqlResp.Data {
		if repoData.ActionYml != nil || repoData.ActionYaml != nil {
			actionRepos = append(actionRepos, repoData.Name)
		}
	}

	return actionRepos, nil
}

func fetchDependents(repoNames []string) map[string]int {
	result := make(map[string]int)
	if len(repoNames) == 0 {
		return result
	}

	args := make([]string, 0, len(repoNames)+2)
	args = append(args, "dependents", "--detailed")
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

func generateHighlighted(repos []repo, dependentsMap map[string]int) string {
	var highlighted []repo

	// Include non-archived repos with >= 1 star OR > 0 dependents
	for _, r := range repos {
		if !r.Archived && (r.Stars >= 1 || dependentsMap[r.Name] > 0) {
			highlighted = append(highlighted, r)
		}
	}

	// Sort by stars descending
	sort.Slice(highlighted, func(i, j int) bool {
		return highlighted[i].Stars > highlighted[j].Stars
	})

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
