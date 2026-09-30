package projects

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestServiceListFiltersAndAssertWritable(t *testing.T) {
	service := NewService(newMemoryStore(), fixedClock)
	ctx := context.Background()
	if _, err := service.CreateProject(ctx, CreateProjectRequest{ID: "project-a", Name: "Project A"}); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := service.CreateSpace(ctx, CreateSpaceRequest{ID: "hidden-assistant", Name: "Hidden", Kind: KindAssistant, Visibility: VisibilityHidden}); err != nil {
		t.Fatalf("CreateSpace(hidden) error = %v", err)
	}
	if _, err := service.CreateSpace(ctx, CreateSpaceRequest{ID: "archived-system", Name: "Archived", Kind: KindSystem, Visibility: VisibilityArchived}); err != nil {
		t.Fatalf("CreateSpace(archived) error = %v", err)
	}

	projects, err := service.ListProjects(ctx, false, false)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0].ID() != "project-a" {
		t.Fatalf("projects = %+v", projects)
	}
	visibleSpaces, err := service.ListSpaces(ctx, "", false, false)
	if err != nil {
		t.Fatalf("ListSpaces() error = %v", err)
	}
	if len(visibleSpaces) != 1 {
		t.Fatalf("visible spaces length = %d", len(visibleSpaces))
	}
	allSpaces, err := service.ListSpaces(ctx, "", true, true)
	if err != nil {
		t.Fatalf("ListSpaces(all) error = %v", err)
	}
	if len(allSpaces) != 3 {
		t.Fatalf("all spaces length = %d", len(allSpaces))
	}
	if _, err := service.AssertWritable(ctx, "archived-system", false); !errors.Is(err, ErrArchivedScopeWrite) {
		t.Fatalf("AssertWritable archived error = %v", err)
	}
	if _, err := service.AssertWritable(ctx, "archived-system", true); err != nil {
		t.Fatalf("AssertWritable override error = %v", err)
	}
}

func TestServicePatchRootPathClearAndNameValidation(t *testing.T) {
	service := NewService(newMemoryStore(), fixedClock)
	ctx := context.Background()
	if _, err := service.CreateProject(ctx, CreateProjectRequest{ID: "project-a", Name: "Project A", RootPath: "/tmp/project-a"}); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	empty := ""
	renamed := "Project Renamed"
	updated, err := service.UpdateProject(ctx, "project-a", UpdateProjectRequest{
		Name:     &renamed,
		RootPath: &empty,
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if updated.Name() != "Project Renamed" || updated.RootPath() != "" {
		t.Fatalf("updated = %+v", updated)
	}
	blank := " "
	if _, err := service.UpdateProject(ctx, "project-a", UpdateProjectRequest{Name: &blank}); !errors.Is(err, ErrMissingName) {
		t.Fatalf("blank name error = %v", err)
	}
}

func TestServiceRepositoryURLAcceptsGitRemoteFormsAndCanClear(t *testing.T) {
	ctx := context.Background()
	service := NewService(newMemoryStore(), fixedClock)
	for index, repositoryURL := range []string{
		"http://git.internal/example/project.git",
		"https://github.com/example/project.git",
		"ssh://git@github.com/example/project.git",
		"git://git.internal/example/project.git",
		"git@github.com:example/project.git",
		"github.com:example/project.git",
	} {
		id := fmt.Sprintf("project-%d", index)
		created, err := service.CreateProject(ctx, CreateProjectRequest{
			ID: id, Name: id, RepositoryURL: " " + repositoryURL + " ",
		})
		if err != nil {
			t.Fatalf("CreateProject(%q) error = %v", repositoryURL, err)
		}
		if created.RepositoryURL() != repositoryURL {
			t.Fatalf("repository_url = %q, want trimmed %q", created.RepositoryURL(), repositoryURL)
		}
	}

	for _, repositoryURL := range []string{
		"ftp://github.com/example/project.git",
		"https://github.com",
		"https://github.com/example/project?token=secret",
		"https://user:secret@github.com/example/project.git",
		"../project.git",
	} {
		if _, err := service.CreateProject(ctx, CreateProjectRequest{
			ID: "invalid-project", Name: "Invalid Project", RepositoryURL: repositoryURL,
		}); !errors.Is(err, ErrInvalidRepositoryURL) {
			t.Fatalf("CreateProject(%q) error = %v, want ErrInvalidRepositoryURL", repositoryURL, err)
		}
	}

	created, err := service.CreateProject(ctx, CreateProjectRequest{ID: "clear-project", Name: "Clear Project"})
	if err != nil {
		t.Fatalf("CreateProject(without repository_url) error = %v", err)
	}
	repositoryURL := "ssh://git@github.com/example/renamed.git"
	updated, err := service.UpdateProject(ctx, created.ID(), UpdateProjectRequest{RepositoryURL: &repositoryURL})
	if err != nil {
		t.Fatalf("UpdateProject(repository_url) error = %v", err)
	}
	if updated.RepositoryURL() != repositoryURL {
		t.Fatalf("updated repository_url = %q, want %q", updated.RepositoryURL(), repositoryURL)
	}
	empty := ""
	cleared, err := service.UpdateProject(ctx, created.ID(), UpdateProjectRequest{RepositoryURL: &empty})
	if err != nil {
		t.Fatalf("UpdateProject(clear repository_url) error = %v", err)
	}
	if cleared.RepositoryURL() != "" {
		t.Fatalf("cleared repository_url = %q, want empty", cleared.RepositoryURL())
	}
	invalid := "file:///tmp/project.git"
	if _, err := service.UpdateProject(ctx, created.ID(), UpdateProjectRequest{RepositoryURL: &invalid}); !errors.Is(err, ErrInvalidRepositoryURL) {
		t.Fatalf("UpdateProject(%q) error = %v, want ErrInvalidRepositoryURL", invalid, err)
	}
}
