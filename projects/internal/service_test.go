package projects

import (
	"context"
	"errors"
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
