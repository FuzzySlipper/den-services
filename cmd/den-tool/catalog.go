package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const defaultRepoRoot = "/home/dev"

//go:embed catalog.json
var embeddedCatalog []byte

type Risk string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
)

type Catalog struct {
	Version string `json:"version"`
	Tools   []Tool `json:"tools"`
}

type Tool struct {
	ID               string          `json:"id"`
	Description      string          `json:"description"`
	Tags             []string        `json:"tags"`
	Repo             string          `json:"repo"`
	Root             string          `json:"root"`
	WorkingDirectory string          `json:"working_directory"`
	Argv             []string        `json:"argv"`
	Risk             Risk            `json:"risk"`
	Source           string          `json:"source"`
	Requirements     []string        `json:"requirements"`
	Examples         []string        `json:"examples"`
	Backend          string          `json:"backend,omitempty"`
	Operation        string          `json:"operation,omitempty"`
	WorkflowTier     string          `json:"workflow_tier,omitempty"`
	InputSchema      json.RawMessage `json:"input_schema,omitempty"`
}

type Availability struct {
	Available bool
	Reason    string
}

func ParseCatalog(data []byte) (Catalog, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Catalog{}, fmt.Errorf("decode catalog: trailing JSON value")
		}
		return Catalog{}, fmt.Errorf("decode catalog: trailing data: %w", err)
	}

	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func LoadEmbeddedCatalog(repoRoot string) (Catalog, error) {
	catalog, err := ParseCatalog(embeddedCatalog)
	if err != nil {
		return Catalog{}, err
	}
	catalog, err = appendMCPTools(catalog)
	if err != nil {
		return Catalog{}, err
	}
	return ResolveCatalog(catalog, repoRoot)
}

func (c *Catalog) Validate() error {
	if c == nil {
		return fmt.Errorf("catalog is nil")
	}
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("catalog version is required")
	}
	if len(c.Tools) == 0 {
		return fmt.Errorf("catalog must contain at least one tool")
	}

	seen := make(map[string]struct{}, len(c.Tools))
	for index := range c.Tools {
		tool := &c.Tools[index]
		if !validToolID(tool.ID) {
			return fmt.Errorf("tool %d has invalid id %q", index, tool.ID)
		}
		if _, exists := seen[tool.ID]; exists {
			return fmt.Errorf("duplicate tool id %q", tool.ID)
		}
		seen[tool.ID] = struct{}{}
		if strings.TrimSpace(tool.Description) == "" {
			return fmt.Errorf("tool %q has empty description", tool.ID)
		}
		if err := validateStringList(tool.ID, "tags", tool.Tags, true); err != nil {
			return err
		}
		if strings.TrimSpace(tool.Repo) == "" {
			return fmt.Errorf("tool %q has empty repo", tool.ID)
		}
		if err := validateCatalogPath(tool.ID, "root", tool.Root); err != nil {
			return err
		}
		if err := validateCatalogPath(tool.ID, "working_directory", tool.WorkingDirectory); err != nil {
			return err
		}
		if !pathWithin(tool.Root, tool.WorkingDirectory) {
			return fmt.Errorf("tool %q working_directory %q is outside root %q", tool.ID, tool.WorkingDirectory, tool.Root)
		}
		if len(tool.Argv) == 0 {
			return fmt.Errorf("tool %q must declare a non-empty argv", tool.ID)
		}
		for argumentIndex, argument := range tool.Argv {
			if argument == "" {
				return fmt.Errorf("tool %q argv[%d] is empty", tool.ID, argumentIndex)
			}
			if strings.ContainsRune(argument, '\x00') {
				return fmt.Errorf("tool %q argv[%d] contains NUL", tool.ID, argumentIndex)
			}
		}
		if !validRisk(tool.Risk) {
			return fmt.Errorf("tool %q has invalid risk %q", tool.ID, tool.Risk)
		}
		if strings.TrimSpace(tool.Source) == "" {
			return fmt.Errorf("tool %q has empty source", tool.ID)
		}
		if err := validateStringList(tool.ID, "requirements", tool.Requirements, false); err != nil {
			return err
		}
		if err := validateStringList(tool.ID, "examples", tool.Examples, false); err != nil {
			return err
		}
		if strings.HasPrefix(tool.ID, "den.") {
			if strings.TrimSpace(tool.Backend) == "" || strings.TrimSpace(tool.Operation) == "" {
				return fmt.Errorf("tool %q must declare backend and operation", tool.ID)
			}
			if len(tool.InputSchema) == 0 || !json.Valid(tool.InputSchema) {
				return fmt.Errorf("tool %q must declare a valid input schema", tool.ID)
			}
		}
	}

	sort.SliceStable(c.Tools, func(left, right int) bool {
		return c.Tools[left].ID < c.Tools[right].ID
	})
	return nil
}

func ResolveCatalog(catalog Catalog, repoRoot string) (Catalog, error) {
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = defaultRepoRoot
	}
	if !filepath.IsAbs(repoRoot) {
		return Catalog{}, fmt.Errorf("repo root %q must be absolute", repoRoot)
	}

	resolved := Catalog{Version: catalog.Version, Tools: make([]Tool, len(catalog.Tools))}
	copy(resolved.Tools, catalog.Tools)
	for index := range resolved.Tools {
		rawTool := catalog.Tools[index]
		rootRelative, err := filepath.Rel(defaultRepoRoot, rawTool.Root)
		if err != nil || !relativePathWithin(rootRelative) {
			return Catalog{}, fmt.Errorf("tool %q root %q is not under default repo root %q", rawTool.ID, rawTool.Root, defaultRepoRoot)
		}
		workingRelative, err := filepath.Rel(rawTool.Root, rawTool.WorkingDirectory)
		if err != nil || !relativePathWithin(workingRelative) {
			return Catalog{}, fmt.Errorf("tool %q working_directory %q is not under root %q", rawTool.ID, rawTool.WorkingDirectory, rawTool.Root)
		}

		resolvedRoot := filepath.Clean(filepath.Join(repoRoot, rootRelative))
		resolved.Tools[index].Root = resolvedRoot
		resolved.Tools[index].WorkingDirectory = filepath.Clean(filepath.Join(resolvedRoot, workingRelative))
	}
	return resolved, nil
}

func (c Catalog) Find(id string) (Tool, bool) {
	for _, tool := range c.Tools {
		if tool.ID == id {
			return tool, true
		}
	}
	return Tool{}, false
}

func (c Catalog) Search(terms []string) Catalog {
	result := Catalog{Version: c.Version}
	for _, term := range terms {
		if strings.TrimSpace(term) == "" {
			return result
		}
	}

	for _, tool := range c.Tools {
		metadata := strings.ToLower(strings.Join([]string{
			tool.ID,
			tool.Description,
			strings.Join(tool.Tags, " "),
			tool.Repo,
			tool.Root,
			tool.WorkingDirectory,
			strings.Join(tool.Argv, " "),
			string(tool.Risk),
			tool.Source,
			strings.Join(tool.Requirements, " "),
			strings.Join(tool.Examples, " "),
		}, " "))

		matches := true
		for _, term := range terms {
			if !strings.Contains(metadata, strings.ToLower(term)) {
				matches = false
				break
			}
		}
		if matches {
			result.Tools = append(result.Tools, tool)
		}
	}

	sort.SliceStable(result.Tools, func(left, right int) bool {
		return result.Tools[left].ID < result.Tools[right].ID
	})
	return result
}

func CheckAvailability(tool Tool) Availability {
	info, err := os.Stat(tool.WorkingDirectory)
	if err != nil {
		return Availability{Reason: fmt.Sprintf("working directory %q is unavailable: %v", tool.WorkingDirectory, err)}
	}
	if !info.IsDir() {
		return Availability{Reason: fmt.Sprintf("working directory %q is not a directory", tool.WorkingDirectory)}
	}
	if _, err := exec.LookPath(tool.Argv[0]); err != nil {
		return Availability{Reason: fmt.Sprintf("executable %q is unavailable: %v", tool.Argv[0], err)}
	}
	return Availability{Available: true}
}

func RepositoryRootFromEnv() (string, error) {
	repoRoot := strings.TrimSpace(os.Getenv("DEN_REPO_ROOT"))
	if repoRoot == "" {
		return defaultRepoRoot, nil
	}
	if !filepath.IsAbs(repoRoot) {
		return "", fmt.Errorf("DEN_REPO_ROOT %q must be an absolute path", repoRoot)
	}
	return filepath.Clean(repoRoot), nil
}

func validateStringList(id, field string, values []string, rejectDuplicates bool) error {
	if values == nil {
		return fmt.Errorf("tool %q must declare %s as an array", id, field)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("tool %q %s[%d] is empty", id, field, index)
		}
		if rejectDuplicates {
			if _, exists := seen[value]; exists {
				return fmt.Errorf("tool %q has duplicate %s value %q", id, field, value)
			}
			seen[value] = struct{}{}
		}
	}
	return nil
}

func validateCatalogPath(id, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("tool %q has empty %s", id, field)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("tool %q %s %q must be absolute", id, field, value)
	}
	return nil
}

func validToolID(id string) bool {
	if id == "" {
		return false
	}
	for index, character := range id {
		if index == 0 {
			if !isLowerAlphaNumeric(character) {
				return false
			}
			continue
		}
		if !isLowerAlphaNumeric(character) && character != '.' && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func isLowerAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func validRisk(risk Risk) bool {
	return risk == RiskRead || risk == RiskWrite || risk == RiskDestructive
}

func pathWithin(base, target string) bool {
	relative, err := filepath.Rel(base, target)
	return err == nil && relativePathWithin(relative)
}

func relativePathWithin(relative string) bool {
	return relative != ".." && relative != "" && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
