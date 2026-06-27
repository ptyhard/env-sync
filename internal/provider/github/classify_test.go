package github

import (
	"errors"
	"testing"

	"github.com/ptyhard/env-sync/internal/provider"
)

// --- classifyGitHubTasksByExistence のユニットテスト（純粋関数） ---

func TestClassifyGitHubTasksByExistence_AllNew(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "FOO", Secret: true}},
		{envScope: "production", entry: provider.Entry{Key: "BAR", Secret: false}},
	}
	exists := func(t githubTask) (bool, error) { return false, nil }

	result, err := classifyGitHubTasksByExistence(tasks, exists)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	for _, c := range result {
		if !c.isNew {
			t.Errorf("%s: isNew = false, want true", c.task.entry.Key)
		}
	}
}

func TestClassifyGitHubTasksByExistence_AllUpdate(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "FOO", Secret: true}},
		{envScope: "production", entry: provider.Entry{Key: "BAR", Secret: false}},
	}
	exists := func(t githubTask) (bool, error) { return true, nil }

	result, err := classifyGitHubTasksByExistence(tasks, exists)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	for _, c := range result {
		if c.isNew {
			t.Errorf("%s: isNew = true, want false", c.task.entry.Key)
		}
	}
}

func TestClassifyGitHubTasksByExistence_Mixed(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "NEW_SECRET", Secret: true}},
		{envScope: "", entry: provider.Entry{Key: "EXISTING_VAR", Secret: false}},
	}
	existingKeys := map[string]bool{"EXISTING_VAR": true}
	exists := func(t githubTask) (bool, error) {
		return existingKeys[t.entry.Key], nil
	}

	result, err := classifyGitHubTasksByExistence(tasks, exists)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if !result[0].isNew {
		t.Errorf("NEW_SECRET: isNew = false, want true")
	}
	if result[1].isNew {
		t.Errorf("EXISTING_VAR: isNew = true, want false")
	}
}

func TestClassifyGitHubTasksByExistence_Error(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "FOO", Secret: true}},
	}
	existsErr := errors.New("API エラー")
	exists := func(t githubTask) (bool, error) { return false, existsErr }

	_, err := classifyGitHubTasksByExistence(tasks, exists)
	if err == nil {
		t.Fatal("エラーを期待したが nil")
	}
}

func TestClassifyGitHubTasksByExistence_Empty(t *testing.T) {
	result, err := classifyGitHubTasksByExistence([]githubTask{}, func(t githubTask) (bool, error) { return false, nil })
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("len = %d, want 0", len(result))
	}
}

// --- countGitHubClassified のユニットテスト ---

func TestCountGitHubClassified_Nil(t *testing.T) {
	newCount, updateCount := countGitHubClassified(nil, 3)
	if newCount != 3 {
		t.Errorf("newCount = %d, want 3", newCount)
	}
	if updateCount != 0 {
		t.Errorf("updateCount = %d, want 0", updateCount)
	}
}

func TestCountGitHubClassified_Mixed(t *testing.T) {
	classified := []githubClassifiedTask{
		{task: githubTask{entry: provider.Entry{Key: "A"}}, isNew: true},
		{task: githubTask{entry: provider.Entry{Key: "B"}}, isNew: false},
		{task: githubTask{entry: provider.Entry{Key: "C"}}, isNew: true},
	}
	newCount, updateCount := countGitHubClassified(classified, len(classified))
	if newCount != 2 {
		t.Errorf("newCount = %d, want 2", newCount)
	}
	if updateCount != 1 {
		t.Errorf("updateCount = %d, want 1", updateCount)
	}
}
