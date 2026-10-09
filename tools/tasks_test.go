package tools

import (
	"strings"
	"testing"

	tasks "google.golang.org/api/tasks/v1"
)

// --- orNA ---

func TestTasksOrNA(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string returns N/A", "", "N/A"},
		{"non-empty returns as-is", "hello", "hello"},
		{"whitespace returns as-is", "  ", "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orNA(tt.input)
			if got != tt.want {
				t.Errorf("orNA(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- adjustDueMaxForTasksAPI ---

func TestTasksAdjustDueMaxForTasksAPI(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantContains string
		wantExact    string
	}{
		{
			"RFC3339 date adds one day",
			"2024-12-31T00:00:00Z",
			"",
			"2025-01-01T00:00:00Z",
		},
		{
			"RFC3339 with timezone offset",
			"2024-06-15T12:00:00+05:00",
			"2024-06-16T",
			"",
		},
		{
			"without timezone adds one day",
			"2024-01-01T00:00:00",
			"",
			"2024-01-02T00:00:00Z",
		},
		{
			"invalid format returns as-is",
			"not-a-date",
			"",
			"not-a-date",
		},
		{
			"empty string returns as-is",
			"",
			"",
			"",
		},
		{
			"date-only format returns as-is (no T)",
			"2024-12-31",
			"",
			"2024-12-31",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adjustDueMaxForTasksAPI(tt.input)
			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("adjustDueMaxForTasksAPI(%q) = %q, want %q", tt.input, got, tt.wantExact)
				}
			}
			if tt.wantContains != "" {
				if !strings.Contains(got, tt.wantContains) {
					t.Errorf("adjustDueMaxForTasksAPI(%q) = %q, want contains %q", tt.input, got, tt.wantContains)
				}
			}
		})
	}
}

// --- getStructuredTasks ---

func TestTasksGetStructuredTasks(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := getStructuredTasks(nil)
		if len(result) != 0 {
			t.Errorf("expected empty result, got %d items", len(result))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result := getStructuredTasks([]*tasks.Task{})
		if len(result) != 0 {
			t.Errorf("expected empty result, got %d items", len(result))
		}
	})

	t.Run("single task no parent", func(t *testing.T) {
		items := []*tasks.Task{
			{Id: "t1", Title: "Task 1", Status: "needsAction", Position: "00000000000000000000"},
		}
		result := getStructuredTasks(items)
		if len(result) != 1 {
			t.Fatalf("expected 1 top-level task, got %d", len(result))
		}
		if result[0].title != "Task 1" {
			t.Errorf("expected title 'Task 1', got %q", result[0].title)
		}
		if result[0].status != "needsAction" {
			t.Errorf("expected status 'needsAction', got %q", result[0].status)
		}
	})

	t.Run("parent-child relationship", func(t *testing.T) {
		items := []*tasks.Task{
			{Id: "p1", Title: "Parent", Position: "00000000000000000000"},
			{Id: "c1", Title: "Child", Parent: "p1", Position: "00000000000000000000"},
		}
		result := getStructuredTasks(items)
		if len(result) != 1 {
			t.Fatalf("expected 1 top-level task, got %d", len(result))
		}
		if result[0].title != "Parent" {
			t.Errorf("expected 'Parent', got %q", result[0].title)
		}
		if len(result[0].subtasks) != 1 {
			t.Fatalf("expected 1 subtask, got %d", len(result[0].subtasks))
		}
		if result[0].subtasks[0].title != "Child" {
			t.Errorf("expected subtask 'Child', got %q", result[0].subtasks[0].title)
		}
	})

	t.Run("orphaned subtask creates placeholder parent", func(t *testing.T) {
		items := []*tasks.Task{
			{Id: "c1", Title: "Orphan", Parent: "missing_parent", Position: "00000000000000000000"},
		}
		result := getStructuredTasks(items)
		// Should have a placeholder parent at root level
		if len(result) != 1 {
			t.Fatalf("expected 1 top-level item (placeholder), got %d", len(result))
		}
		if !result[0].isPlaceholderParent {
			t.Error("expected placeholder parent")
		}
		if len(result[0].subtasks) != 1 {
			t.Fatalf("expected 1 subtask under placeholder, got %d", len(result[0].subtasks))
		}
		if result[0].subtasks[0].title != "Orphan" {
			t.Errorf("expected 'Orphan', got %q", result[0].subtasks[0].title)
		}
	})

	t.Run("completed task preserves completed field", func(t *testing.T) {
		completedTime := "2024-01-15T10:00:00Z"
		items := []*tasks.Task{
			{Id: "t1", Title: "Done", Status: "completed", Completed: &completedTime, Position: "00000000000000000000"},
		}
		result := getStructuredTasks(items)
		if len(result) != 1 {
			t.Fatalf("expected 1 task, got %d", len(result))
		}
		if result[0].completed != completedTime {
			t.Errorf("expected completed %q, got %q", completedTime, result[0].completed)
		}
	})

	t.Run("tasks sorted by position", func(t *testing.T) {
		items := []*tasks.Task{
			{Id: "t2", Title: "Second", Position: "00000000000000000001"},
			{Id: "t1", Title: "First", Position: "00000000000000000000"},
		}
		result := getStructuredTasks(items)
		if len(result) != 2 {
			t.Fatalf("expected 2 tasks, got %d", len(result))
		}
		if result[0].title != "First" {
			t.Errorf("expected 'First' first, got %q", result[0].title)
		}
		if result[1].title != "Second" {
			t.Errorf("expected 'Second' second, got %q", result[1].title)
		}
	})

	t.Run("nil Completed pointer", func(t *testing.T) {
		items := []*tasks.Task{
			{Id: "t1", Title: "Pending", Status: "needsAction", Completed: nil},
		}
		result := getStructuredTasks(items)
		if result[0].completed != "" {
			t.Errorf("expected empty completed, got %q", result[0].completed)
		}
	})
}

// --- serializeTasks ---

func TestTasksSerializeTasks(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		result := serializeTasks(nil, 0)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("single task at root level", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "t1", title: "My Task", status: "needsAction", updated: "2024-01-01"},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "- My Task (ID: t1)") {
			t.Errorf("expected root bullet, got %q", result)
		}
		if !strings.Contains(result, "Status: needsAction") {
			t.Errorf("expected status line, got %q", result)
		}
		if !strings.Contains(result, "Updated: 2024-01-01") {
			t.Errorf("expected updated line, got %q", result)
		}
	})

	t.Run("subtask uses * bullet", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "c1", title: "Child", status: "needsAction", updated: "2024-01-01"},
		}
		result := serializeTasks(structured, 1)
		if !strings.Contains(result, "* Child") {
			t.Errorf("expected * bullet for subtask, got %q", result)
		}
	})

	t.Run("task with due date", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "t1", title: "Task", status: "needsAction", due: "2024-12-31", updated: "2024-01-01"},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "Due: 2024-12-31") {
			t.Errorf("expected due date, got %q", result)
		}
	})

	t.Run("task with notes truncated at 100 chars", func(t *testing.T) {
		longNotes := strings.Repeat("a", 150)
		structured := []*structuredTask{
			{id: "t1", title: "Task", status: "needsAction", notes: longNotes, updated: "2024-01-01"},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "Notes: "+strings.Repeat("a", 100)+"...") {
			t.Error("expected notes truncated at 100 chars with ellipsis")
		}
	})

	t.Run("task with completed field", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "t1", title: "Done", status: "completed", completed: "2024-01-15", updated: "2024-01-15"},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "Completed: 2024-01-15") {
			t.Errorf("expected completed line, got %q", result)
		}
	})

	t.Run("empty title shows Untitled", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "t1", title: "", status: "needsAction", updated: "2024-01-01"},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "Untitled") {
			t.Errorf("expected 'Untitled' for empty title, got %q", result)
		}
	})

	t.Run("placeholder parent shows Unknown parent", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "p1", title: "", isPlaceholderParent: true, updated: "2024-01-01",
				subtasks: []*structuredTask{
					{id: "c1", title: "Child", status: "needsAction", updated: "2024-01-01"},
				},
			},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "Unknown parent") {
			t.Errorf("expected 'Unknown parent' for placeholder, got %q", result)
		}
		if !strings.Contains(result, "1 tasks with title Unknown parent") {
			t.Errorf("expected placeholder explanation, got %q", result)
		}
	})

	t.Run("empty status shows N/A", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "t1", title: "Task", status: "", updated: ""},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "Status: N/A") {
			t.Errorf("expected 'Status: N/A', got %q", result)
		}
	})

	t.Run("nested subtasks", func(t *testing.T) {
		structured := []*structuredTask{
			{id: "p1", title: "Parent", status: "needsAction", updated: "2024-01-01",
				subtasks: []*structuredTask{
					{id: "c1", title: "Child", status: "needsAction", updated: "2024-01-01",
						subtasks: []*structuredTask{
							{id: "gc1", title: "Grandchild", status: "needsAction", updated: "2024-01-01"},
						},
					},
				},
			},
		}
		result := serializeTasks(structured, 0)
		if !strings.Contains(result, "- Parent") {
			t.Error("expected root level Parent")
		}
		if !strings.Contains(result, "  * Child") {
			t.Error("expected level-1 Child with indent")
		}
		if !strings.Contains(result, "    * Grandchild") {
			t.Error("expected level-2 Grandchild with double indent")
		}
	})
}

// --- sortStructuredTasks ---

func TestTasksSortStructuredTasks(t *testing.T) {
	t.Run("sorts by position ascending", func(t *testing.T) {
		root := &structuredTask{
			subtasks: []*structuredTask{
				{id: "b", title: "B"},
				{id: "a", title: "A"},
				{id: "c", title: "C"},
			},
		}
		positions := map[string]int64{"a": 0, "b": 1, "c": 2}
		sortStructuredTasks(root, positions)
		if root.subtasks[0].id != "a" || root.subtasks[1].id != "b" || root.subtasks[2].id != "c" {
			t.Errorf("expected a,b,c order, got %s,%s,%s",
				root.subtasks[0].id, root.subtasks[1].id, root.subtasks[2].id)
		}
	})

	t.Run("missing position goes to end", func(t *testing.T) {
		root := &structuredTask{
			subtasks: []*structuredTask{
				{id: "no-pos", title: "No Position"},
				{id: "a", title: "A"},
			},
		}
		positions := map[string]int64{"a": 0}
		sortStructuredTasks(root, positions)
		if root.subtasks[0].id != "a" {
			t.Errorf("expected 'a' first, got %q", root.subtasks[0].id)
		}
	})

	t.Run("sorts subtasks recursively", func(t *testing.T) {
		root := &structuredTask{
			subtasks: []*structuredTask{
				{id: "p", title: "Parent", subtasks: []*structuredTask{
					{id: "c2", title: "C2"},
					{id: "c1", title: "C1"},
				}},
			},
		}
		positions := map[string]int64{"p": 0, "c1": 0, "c2": 1}
		sortStructuredTasks(root, positions)
		if root.subtasks[0].subtasks[0].id != "c1" {
			t.Errorf("expected 'c1' first among children, got %q", root.subtasks[0].subtasks[0].id)
		}
	})
}
