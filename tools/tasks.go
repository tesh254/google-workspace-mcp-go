package tools

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	tasks "google.golang.org/api/tasks/v1"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

const (
	listTasksMaxResultsDefault = 20
	listTasksMaxResultsMax     = 10000
)

// RegisterTasksTools registers all Tasks tools with the MCP server.
func RegisterTasksTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	// Read tools
	registerListTaskLists(s, getClient)
	registerGetTaskList(s, getClient)
	registerListTasks(s, getClient)
	registerGetTask(s, getClient)

	// Write tools
	registerCreateTaskList(s, getClient)
	registerUpdateTaskList(s, getClient)
	registerDeleteTaskList(s, getClient)
	registerCreateTask(s, getClient)
	registerUpdateTask(s, getClient)
	registerDeleteTask(s, getClient)
	registerMoveTask(s, getClient)
	registerClearCompletedTasks(s, getClient)
}

// newTasksService creates a tasks.Service for the given user email.
func newTasksService(ctx context.Context, getClient httpClientFunc, email string) (*tasks.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := tasks.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Tasks service: %w", err)
	}
	return svc, nil
}

// --- structuredTask for hierarchical display ---

type structuredTask struct {
	id                  string
	title               string
	status              string
	due                 string
	notes               string
	updated             string
	completed           string
	isPlaceholderParent bool
	subtasks            []*structuredTask
}

// getStructuredTasks converts a flat list of tasks into hierarchical structuredTask objects.
func getStructuredTasks(items []*tasks.Task) []*structuredTask {
	tasksById := make(map[string]*structuredTask, len(items))
	positionsById := make(map[string]int64, len(items))

	for _, t := range items {
		completed := ""
		if t.Completed != nil {
			completed = *t.Completed
		}
		tasksById[t.Id] = &structuredTask{
			id:        t.Id,
			title:     t.Title,
			status:    t.Status,
			due:       t.Due,
			notes:     t.Notes,
			updated:   t.Updated,
			completed: completed,
		}
		if t.Position != "" {
			// Position is a string of digits; parse as int for sorting.
			var pos int64
			fmt.Sscanf(t.Position, "%d", &pos)
			positionsById[t.Id] = pos
		}
	}

	root := &structuredTask{id: "root", title: "Root"}

	for _, t := range items {
		st := tasksById[t.Id]
		parentID := t.Parent

		var parent *structuredTask
		if parentID == "" {
			parent = root
		} else if p, ok := tasksById[parentID]; ok {
			parent = p
		} else {
			// Orphaned subtask: create placeholder parent.
			parent = &structuredTask{id: parentID, isPlaceholderParent: true}
			tasksById[parentID] = parent
			root.subtasks = append(root.subtasks, parent)
		}
		parent.subtasks = append(parent.subtasks, st)
	}

	sortStructuredTasks(root, positionsById)
	return root.subtasks
}

func sortStructuredTasks(root *structuredTask, positionsById map[string]int64) {
	sort.SliceStable(root.subtasks, func(i, j int) bool {
		pi, oki := positionsById[root.subtasks[i].id]
		pj, okj := positionsById[root.subtasks[j].id]
		if !oki {
			pi = math.MaxInt64
		}
		if !okj {
			pj = math.MaxInt64
		}
		return pi < pj
	})
	for _, st := range root.subtasks {
		sortStructuredTasks(st, positionsById)
	}
}

func serializeTasks(structured []*structuredTask, level int) string {
	var sb strings.Builder
	placeholderCount := 0
	for _, t := range structured {
		indent := strings.Repeat("  ", level)
		bullet := "-"
		if level > 0 {
			bullet = "*"
		}
		title := t.title
		if title == "" {
			if t.isPlaceholderParent {
				title = "Unknown parent"
				placeholderCount++
			} else {
				title = "Untitled"
			}
		}
		fmt.Fprintf(&sb, "%s%s %s (ID: %s)\n", indent, bullet, title, t.id)
		fmt.Fprintf(&sb, "%s  Status: %s\n", indent, orNA(t.status))
		if t.due != "" {
			fmt.Fprintf(&sb, "%s  Due: %s\n", indent, t.due)
		}
		if t.notes != "" {
			n := t.notes
			if len(n) > 100 {
				n = n[:100] + "..."
			}
			fmt.Fprintf(&sb, "%s  Notes: %s\n", indent, n)
		}
		if t.completed != "" {
			fmt.Fprintf(&sb, "%s  Completed: %s\n", indent, t.completed)
		}
		fmt.Fprintf(&sb, "%s  Updated: %s\n", indent, orNA(t.updated))
		sb.WriteString("\n")

		sb.WriteString(serializeTasks(t.subtasks, level+1))
	}

	if placeholderCount > 0 && level == 0 {
		fmt.Fprintf(&sb, "\n%d tasks with title Unknown parent are included as placeholders.\n", placeholderCount)
		sb.WriteString("These placeholders contain subtasks whose parents were not present in the task list.\n")
		sb.WriteString("This can occur due to pagination. Callers can often avoid this problem if max_results is large enough to contain all tasks (subtasks and their parents) without paging.\n")
		sb.WriteString("This can also occur due to filtering that excludes parent tasks while including their subtasks or due to deleted or hidden parent tasks.\n")
	}

	return sb.String()
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

// adjustDueMaxForTasksAPI compensates for the Google Tasks API treating dueMax
// as an exclusive bound by adding one day.
func adjustDueMaxForTasksAPI(dueMax string) string {
	// Try RFC3339
	t, err := time.Parse(time.RFC3339, dueMax)
	if err != nil {
		// Try without timezone
		t, err = time.Parse("2006-01-02T15:04:05", dueMax)
		if err != nil {
			return dueMax
		}
		t = t.UTC()
	}
	adjusted := t.Add(24 * time.Hour)
	return adjusted.Format(time.RFC3339)
}

// --- list_task_lists ---

func registerListTaskLists(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_task_lists",
		Description: "List all task lists for the user.",
	}
	mcp.AddTool(s, tool, handleListTaskLists(getClient))
}

// listTaskListsIn is the input schema for list_task_lists.
type listTaskListsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	MaxResults      int    `json:"max_results,omitempty" jsonschema:"Maximum number of task lists to return (default: 1000, max: 1000)."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for pagination."`
}

func handleListTaskLists(getClient httpClientFunc) mcp.ToolHandlerFor[listTaskListsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listTaskListsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		maxResults := in.MaxResults
		if maxResults == 0 {
			maxResults = 1000
		}
		pageToken := in.PageToken

		call := svc.Tasklists.List().MaxResults(int64(maxResults))
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("listing task lists: %v", err)
		}

		if len(result.Items) == 0 {
			return textResult(fmt.Sprintf("No task lists found for %s.", email))
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Task Lists for %s:\n", email)
		for _, tl := range result.Items {
			fmt.Fprintf(&sb, "- %s (ID: %s)\n", tl.Title, tl.Id)
			fmt.Fprintf(&sb, "  Updated: %s\n", orNA(tl.Updated))
		}

		if result.NextPageToken != "" {
			fmt.Fprintf(&sb, "\nNext page token: %s", result.NextPageToken)
		}

		return textResult(sb.String())
	}
}

// --- get_task_list ---

func registerGetTaskList(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_task_list",
		Description: "Get details of a specific task list.",
	}
	mcp.AddTool(s, tool, handleGetTaskList(getClient))
}

// getTaskListIn is the input schema for get_task_list.
type getTaskListIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list to retrieve."`
}

func handleGetTaskList(getClient httpClientFunc) mcp.ToolHandlerFor[getTaskListIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getTaskListIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		tl, err := svc.Tasklists.Get(taskListID).Do()
		if err != nil {
			return nil, nil, toolErr("getting task list: %v", err)
		}

		response := fmt.Sprintf(`Task List Details for %s:
- Title: %s
- ID: %s
- Updated: %s
- Self Link: %s`, email, tl.Title, tl.Id, orNA(tl.Updated), orNA(tl.SelfLink))

		return textResult(response)
	}
}

// --- create_task_list ---

func registerCreateTaskList(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_task_list",
		Description: "Create a new task list.",
	}
	mcp.AddTool(s, tool, handleCreateTaskList(getClient))
}

// createTaskListIn is the input schema for create_task_list.
type createTaskListIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	Title           string `json:"title" jsonschema:"The title of the new task list."`
}

func handleCreateTaskList(getClient httpClientFunc) mcp.ToolHandlerFor[createTaskListIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createTaskListIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		title := in.Title

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		result, err := svc.Tasklists.Insert(&tasks.TaskList{Title: title}).Do()
		if err != nil {
			return nil, nil, toolErr("creating task list: %v", err)
		}

		response := fmt.Sprintf(`Task List Created for %s:
- Title: %s
- ID: %s
- Created: %s
- Self Link: %s`, email, result.Title, result.Id, orNA(result.Updated), orNA(result.SelfLink))

		return textResult(response)
	}
}

// --- update_task_list ---

func registerUpdateTaskList(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_task_list",
		Description: "Update an existing task list.",
	}
	mcp.AddTool(s, tool, handleUpdateTaskList(getClient))
}

// updateTaskListIn is the input schema for update_task_list.
type updateTaskListIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list to update."`
	Title           string `json:"title" jsonschema:"The new title for the task list."`
}

func handleUpdateTaskList(getClient httpClientFunc) mcp.ToolHandlerFor[updateTaskListIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateTaskListIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID
		title := in.Title

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		body := &tasks.TaskList{Id: taskListID, Title: title}
		result, err := svc.Tasklists.Update(taskListID, body).Do()
		if err != nil {
			return nil, nil, toolErr("updating task list: %v", err)
		}

		response := fmt.Sprintf(`Task List Updated for %s:
- Title: %s
- ID: %s
- Updated: %s`, email, result.Title, result.Id, orNA(result.Updated))

		return textResult(response)
	}
}

// --- delete_task_list ---

func registerDeleteTaskList(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_task_list",
		Description: "Delete a task list. Note: This will also delete all tasks in the list.",
	}
	mcp.AddTool(s, tool, handleDeleteTaskList(getClient))
}

// deleteTaskListIn is the input schema for delete_task_list.
type deleteTaskListIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list to delete."`
}

func handleDeleteTaskList(getClient httpClientFunc) mcp.ToolHandlerFor[deleteTaskListIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTaskListIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		err = svc.Tasklists.Delete(taskListID).Do()
		if err != nil {
			return nil, nil, toolErr("deleting task list: %v", err)
		}

		return textResult(fmt.Sprintf(
			"Task list %s has been deleted for %s. All tasks in this list have also been deleted.",
			taskListID, email,
		))
	}
}

// --- list_tasks ---

func registerListTasks(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_tasks",
		Description: "List all tasks in a specific task list.",
	}
	mcp.AddTool(s, tool, handleListTasks(getClient))
}

// listTasksIn is the input schema for list_tasks.
type listTasksIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list to retrieve tasks from."`
	MaxResults      int    `json:"max_results,omitempty" jsonschema:"Maximum number of tasks to return (default: 20, max: 10000)."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for pagination."`
	ShowCompleted   *bool  `json:"show_completed,omitempty" jsonschema:"Whether to include completed tasks (default: true). Note that show_hidden must also be true to show tasks completed in first party clients, such as the web UI and Google's mobile apps."`
	ShowDeleted     bool   `json:"show_deleted,omitempty" jsonschema:"Whether to include deleted tasks (default: false)."`
	ShowHidden      bool   `json:"show_hidden,omitempty" jsonschema:"Whether to include hidden tasks (default: false)."`
	ShowAssigned    bool   `json:"show_assigned,omitempty" jsonschema:"Whether to include assigned tasks (default: false)."`
	CompletedMax    string `json:"completed_max,omitempty" jsonschema:"Upper bound for completion date (RFC 3339 timestamp)."`
	CompletedMin    string `json:"completed_min,omitempty" jsonschema:"Lower bound for completion date (RFC 3339 timestamp)."`
	DueMax          string `json:"due_max,omitempty" jsonschema:"Upper bound for due date (RFC 3339 timestamp)."`
	DueMin          string `json:"due_min,omitempty" jsonschema:"Lower bound for due date (RFC 3339 timestamp)."`
	UpdatedMin      string `json:"updated_min,omitempty" jsonschema:"Lower bound for last modification time (RFC 3339 timestamp)."`
}

func handleListTasks(getClient httpClientFunc) mcp.ToolHandlerFor[listTasksIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listTasksIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		maxResults := in.MaxResults
		if maxResults == 0 {
			maxResults = listTasksMaxResultsDefault
		}
		pageToken := in.PageToken

		showCompleted := true
		if in.ShowCompleted != nil {
			showCompleted = *in.ShowCompleted
		}
		showDeleted := in.ShowDeleted
		showHidden := in.ShowHidden
		showAssigned := in.ShowAssigned

		completedMax := in.CompletedMax
		completedMin := in.CompletedMin
		dueMax := in.DueMax
		dueMin := in.DueMin
		updatedMin := in.UpdatedMin

		call := svc.Tasks.List(taskListID).MaxResults(int64(maxResults))
		call = call.ShowCompleted(showCompleted)
		call = call.ShowDeleted(showDeleted)
		call = call.ShowHidden(showHidden)
		call = call.ShowAssigned(showAssigned)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		if completedMax != "" {
			call = call.CompletedMax(completedMax)
		}
		if completedMin != "" {
			call = call.CompletedMin(completedMin)
		}
		if dueMax != "" {
			call = call.DueMax(adjustDueMaxForTasksAPI(dueMax))
		}
		if dueMin != "" {
			call = call.DueMin(dueMin)
		}
		if updatedMin != "" {
			call = call.UpdatedMin(updatedMin)
		}

		result, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("listing tasks: %v", err)
		}

		allTasks := result.Items
		nextPageToken := result.NextPageToken

		// Multi-page retrieval to support sorting and structured display.
		remaining := min(maxResults, listTasksMaxResultsMax) - len(allTasks)
		for remaining > 0 && nextPageToken != "" {
			call2 := svc.Tasks.List(taskListID).MaxResults(int64(remaining)).PageToken(nextPageToken)
			call2 = call2.ShowCompleted(showCompleted).ShowDeleted(showDeleted).ShowHidden(showHidden).ShowAssigned(showAssigned)
			if completedMax != "" {
				call2 = call2.CompletedMax(completedMax)
			}
			if completedMin != "" {
				call2 = call2.CompletedMin(completedMin)
			}
			if dueMax != "" {
				call2 = call2.DueMax(adjustDueMaxForTasksAPI(dueMax))
			}
			if dueMin != "" {
				call2 = call2.DueMin(dueMin)
			}
			if updatedMin != "" {
				call2 = call2.UpdatedMin(updatedMin)
			}

			more, err := call2.Do()
			if err != nil {
				break
			}
			if len(more.Items) == 0 {
				break
			}
			allTasks = append(allTasks, more.Items...)
			nextPageToken = more.NextPageToken
			remaining -= len(more.Items)
		}

		if len(allTasks) == 0 {
			return textResult(fmt.Sprintf("No tasks found in task list %s for %s.", taskListID, email))
		}

		structured := getStructuredTasks(allTasks)

		var sb strings.Builder
		fmt.Fprintf(&sb, "Tasks in list %s for %s:\n", taskListID, email)
		sb.WriteString(serializeTasks(structured, 0))

		if nextPageToken != "" {
			fmt.Fprintf(&sb, "Next page token: %s\n", nextPageToken)
		}

		return textResult(sb.String())
	}
}

// --- get_task ---

func registerGetTask(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_task",
		Description: "Get details of a specific task.",
	}
	mcp.AddTool(s, tool, handleGetTask(getClient))
}

// getTaskIn is the input schema for get_task.
type getTaskIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list containing the task."`
	TaskID          string `json:"task_id" jsonschema:"The ID of the task to retrieve."`
}

func handleGetTask(getClient httpClientFunc) mcp.ToolHandlerFor[getTaskIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getTaskIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID
		taskID := in.TaskID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		task, err := svc.Tasks.Get(taskListID, taskID).Do()
		if err != nil {
			return nil, nil, toolErr("getting task: %v", err)
		}

		title := task.Title
		if title == "" {
			title = "Untitled"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Task Details for %s:\n", email)
		fmt.Fprintf(&sb, "- Title: %s\n", title)
		fmt.Fprintf(&sb, "- ID: %s\n", task.Id)
		fmt.Fprintf(&sb, "- Status: %s\n", orNA(task.Status))
		fmt.Fprintf(&sb, "- Updated: %s", orNA(task.Updated))

		if task.Due != "" {
			fmt.Fprintf(&sb, "\n- Due Date: %s", task.Due)
		}
		if task.Completed != nil && *task.Completed != "" {
			fmt.Fprintf(&sb, "\n- Completed: %s", *task.Completed)
		}
		if task.Notes != "" {
			fmt.Fprintf(&sb, "\n- Notes: %s", task.Notes)
		}
		if task.Parent != "" {
			fmt.Fprintf(&sb, "\n- Parent Task ID: %s", task.Parent)
		}
		if task.Position != "" {
			fmt.Fprintf(&sb, "\n- Position: %s", task.Position)
		}
		if task.SelfLink != "" {
			fmt.Fprintf(&sb, "\n- Self Link: %s", task.SelfLink)
		}
		if task.WebViewLink != "" {
			fmt.Fprintf(&sb, "\n- Web View Link: %s", task.WebViewLink)
		}

		return textResult(sb.String())
	}
}

// --- create_task ---

func registerCreateTask(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task in a task list.",
	}
	mcp.AddTool(s, tool, handleCreateTask(getClient))
}

// createTaskIn is the input schema for create_task.
type createTaskIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list to create the task in."`
	Title           string `json:"title" jsonschema:"The title of the task."`
	Notes           string `json:"notes,omitempty" jsonschema:"Notes/description for the task."`
	Due             string `json:"due,omitempty" jsonschema:"Due date in RFC 3339 format (e.g., \"2024-12-31T23:59:59Z\")."`
	Parent          string `json:"parent,omitempty" jsonschema:"Parent task ID (for subtasks)."`
	Previous        string `json:"previous,omitempty" jsonschema:"Previous sibling task ID (for positioning)."`
}

func handleCreateTask(getClient httpClientFunc) mcp.ToolHandlerFor[createTaskIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createTaskIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID
		title := in.Title

		notes := in.Notes
		due := in.Due
		parent := in.Parent
		previous := in.Previous

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		body := &tasks.Task{Title: title}
		if notes != "" {
			body.Notes = notes
		}
		if due != "" {
			body.Due = due
		}

		call := svc.Tasks.Insert(taskListID, body)
		if parent != "" {
			call = call.Parent(parent)
		}
		if previous != "" {
			call = call.Previous(previous)
		}

		result, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("creating task: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Task Created for %s:\n", email)
		fmt.Fprintf(&sb, "- Title: %s\n", result.Title)
		fmt.Fprintf(&sb, "- ID: %s\n", result.Id)
		fmt.Fprintf(&sb, "- Status: %s\n", orNA(result.Status))
		fmt.Fprintf(&sb, "- Updated: %s", orNA(result.Updated))

		if result.Due != "" {
			fmt.Fprintf(&sb, "\n- Due Date: %s", result.Due)
		}
		if result.Notes != "" {
			fmt.Fprintf(&sb, "\n- Notes: %s", result.Notes)
		}
		if result.WebViewLink != "" {
			fmt.Fprintf(&sb, "\n- Web View Link: %s", result.WebViewLink)
		}

		return textResult(sb.String())
	}
}

// --- update_task ---

func registerUpdateTask(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_task",
		Description: "Update an existing task.",
	}
	mcp.AddTool(s, tool, handleUpdateTask(getClient))
}

// updateTaskIn is the input schema for update_task.
type updateTaskIn struct {
	UserGoogleEmail string  `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string  `json:"task_list_id" jsonschema:"The ID of the task list containing the task."`
	TaskID          string  `json:"task_id" jsonschema:"The ID of the task to update."`
	Title           *string `json:"title,omitempty" jsonschema:"New title for the task."`
	Notes           *string `json:"notes,omitempty" jsonschema:"New notes/description for the task."`
	Status          *string `json:"status,omitempty" jsonschema:"New status (\"needsAction\" or \"completed\")."`
	Due             *string `json:"due,omitempty" jsonschema:"New due date in RFC 3339 format."`
}

func handleUpdateTask(getClient httpClientFunc) mcp.ToolHandlerFor[updateTaskIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateTaskIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID
		taskID := in.TaskID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Fetch current task to preserve existing fields.
		current, err := svc.Tasks.Get(taskListID, taskID).Do()
		if err != nil {
			return nil, nil, toolErr("getting current task: %v", err)
		}

		body := &tasks.Task{
			Id:     taskID,
			Title:  current.Title,
			Status: current.Status,
		}

		if in.Title != nil {
			body.Title = *in.Title
		}
		if in.Status != nil {
			body.Status = *in.Status
			if body.Status == "" {
				body.Status = "needsAction"
			}
		}
		if in.Notes != nil {
			body.Notes = *in.Notes
		} else if current.Notes != "" {
			body.Notes = current.Notes
		}
		if in.Due != nil {
			body.Due = *in.Due
		} else if current.Due != "" {
			body.Due = current.Due
		}

		result, err := svc.Tasks.Update(taskListID, taskID, body).Do()
		if err != nil {
			return nil, nil, toolErr("updating task: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Task Updated for %s:\n", email)
		fmt.Fprintf(&sb, "- Title: %s\n", result.Title)
		fmt.Fprintf(&sb, "- ID: %s\n", result.Id)
		fmt.Fprintf(&sb, "- Status: %s\n", orNA(result.Status))
		fmt.Fprintf(&sb, "- Updated: %s", orNA(result.Updated))

		if result.Due != "" {
			fmt.Fprintf(&sb, "\n- Due Date: %s", result.Due)
		}
		if result.Notes != "" {
			fmt.Fprintf(&sb, "\n- Notes: %s", result.Notes)
		}
		if result.Completed != nil && *result.Completed != "" {
			fmt.Fprintf(&sb, "\n- Completed: %s", *result.Completed)
		}

		return textResult(sb.String())
	}
}

// --- delete_task ---

func registerDeleteTask(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete a task from a task list.",
	}
	mcp.AddTool(s, tool, handleDeleteTask(getClient))
}

// deleteTaskIn is the input schema for delete_task.
type deleteTaskIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list containing the task."`
	TaskID          string `json:"task_id" jsonschema:"The ID of the task to delete."`
}

func handleDeleteTask(getClient httpClientFunc) mcp.ToolHandlerFor[deleteTaskIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteTaskIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID
		taskID := in.TaskID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		err = svc.Tasks.Delete(taskListID, taskID).Do()
		if err != nil {
			return nil, nil, toolErr("deleting task: %v", err)
		}

		return textResult(fmt.Sprintf(
			"Task %s has been deleted from task list %s for %s.",
			taskID, taskListID, email,
		))
	}
}

// --- move_task ---

func registerMoveTask(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "move_task",
		Description: "Move a task to a different position or parent within the same list, or to a different list.",
	}
	mcp.AddTool(s, tool, handleMoveTask(getClient))
}

// moveTaskIn is the input schema for move_task.
type moveTaskIn struct {
	UserGoogleEmail     string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID          string `json:"task_list_id" jsonschema:"The ID of the current task list containing the task."`
	TaskID              string `json:"task_id" jsonschema:"The ID of the task to move."`
	Parent              string `json:"parent,omitempty" jsonschema:"New parent task ID (for making it a subtask)."`
	Previous            string `json:"previous,omitempty" jsonschema:"Previous sibling task ID (for positioning)."`
	DestinationTaskList string `json:"destination_task_list,omitempty" jsonschema:"Destination task list ID (for moving between lists)."`
}

func handleMoveTask(getClient httpClientFunc) mcp.ToolHandlerFor[moveTaskIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in moveTaskIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID
		taskID := in.TaskID

		parent := in.Parent
		previous := in.Previous
		destinationTaskList := in.DestinationTaskList

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		call := svc.Tasks.Move(taskListID, taskID)
		if parent != "" {
			call = call.Parent(parent)
		}
		if previous != "" {
			call = call.Previous(previous)
		}
		if destinationTaskList != "" {
			call = call.DestinationTasklist(destinationTaskList)
		}

		result, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("moving task: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Task Moved for %s:\n", email)
		fmt.Fprintf(&sb, "- Title: %s\n", result.Title)
		fmt.Fprintf(&sb, "- ID: %s\n", result.Id)
		fmt.Fprintf(&sb, "- Status: %s\n", orNA(result.Status))
		fmt.Fprintf(&sb, "- Updated: %s", orNA(result.Updated))

		if result.Parent != "" {
			fmt.Fprintf(&sb, "\n- Parent Task ID: %s", result.Parent)
		}
		if result.Position != "" {
			fmt.Fprintf(&sb, "\n- Position: %s", result.Position)
		}

		var moveDetails []string
		if destinationTaskList != "" {
			moveDetails = append(moveDetails, fmt.Sprintf("moved to task list %s", destinationTaskList))
		}
		if parent != "" {
			moveDetails = append(moveDetails, fmt.Sprintf("made a subtask of %s", parent))
		}
		if previous != "" {
			moveDetails = append(moveDetails, fmt.Sprintf("positioned after %s", previous))
		}
		if len(moveDetails) > 0 {
			fmt.Fprintf(&sb, "\n- Move Details: %s", strings.Join(moveDetails, ", "))
		}

		return textResult(sb.String())
	}
}

// --- clear_completed_tasks ---

func registerClearCompletedTasks(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "clear_completed_tasks",
		Description: "Clear all completed tasks from a task list. The tasks will be marked as hidden.",
	}
	mcp.AddTool(s, tool, handleClearCompletedTasks(getClient))
}

// clearCompletedTasksIn is the input schema for clear_completed_tasks.
type clearCompletedTasksIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TaskListID      string `json:"task_list_id" jsonschema:"The ID of the task list to clear completed tasks from."`
}

func handleClearCompletedTasks(getClient httpClientFunc) mcp.ToolHandlerFor[clearCompletedTasksIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in clearCompletedTasksIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		taskListID := in.TaskListID

		svc, err := newTasksService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		err = svc.Tasks.Clear(taskListID).Do()
		if err != nil {
			return nil, nil, toolErr("clearing completed tasks: %v", err)
		}

		return textResult(fmt.Sprintf(
			"All completed tasks have been cleared from task list %s for %s. The tasks are now hidden and won't appear in default task list views.",
			taskListID, email,
		))
	}
}
