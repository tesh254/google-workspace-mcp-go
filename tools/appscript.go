package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	drive "google.golang.org/api/drive/v3"
	script "google.golang.org/api/script/v1"

	google "github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// RegisterAppScriptTools registers all Apps Script tools with the MCP server.
func RegisterAppScriptTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())
	registerListScriptProjects(s, getClient)
	registerGetScriptProject(s, getClient)
	registerGetScriptContent(s, getClient)
	registerListDeployments(s, getClient)
	registerListScriptProcesses(s, getClient)
	registerListVersions(s, getClient)
	registerGetVersion(s, getClient)
	registerGetScriptMetrics(s, getClient)
	registerCreateScriptProject(s, getClient)
	registerUpdateScriptContent(s, getClient)
	registerRunScriptFunction(s, getClient)
	registerCreateDeployment(s, getClient)
	registerUpdateDeployment(s, getClient)
	registerDeleteDeployment(s, getClient)
	registerDeleteScriptProject(s, getClient)
	registerCreateVersion(s, getClient)
	registerGenerateTriggerCode(s)
}

// newScriptService creates an authenticated script.Service for the given user.
func newScriptService(ctx context.Context, getClient httpClientFunc, email string) (*script.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for Apps Script: %w", err)
	}
	svc, err := script.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Apps Script service: %w", err)
	}
	return svc, nil
}

// newDriveServiceForScript creates an authenticated drive.Service for script-related operations.
func newDriveServiceForScript(ctx context.Context, getClient httpClientFunc, email string) (*drive.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for Drive (Apps Script): %w", err)
	}
	svc, err := drive.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Drive service for Apps Script: %w", err)
	}
	return svc, nil
}

// --- list_script_projects ---

// listScriptProjectsIn is the input schema for list_script_projects.
type listScriptProjectsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default: 50)."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for pagination."`
}

func registerListScriptProjects(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_script_projects",
		Description: "Lists Google Apps Script projects accessible to the user.",
	}
	mcp.AddTool(s, tool, handleListScriptProjects(getClient))
}

func handleListScriptProjects(getClient httpClientFunc) mcp.ToolHandlerFor[listScriptProjectsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listScriptProjectsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 50
		}
		pageToken := in.PageToken

		svc, err := newDriveServiceForScript(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		query := "mimeType='application/vnd.google-apps.script' and trashed=false"
		call := svc.Files.List().
			Q(query).
			PageSize(int64(pageSize)).
			Fields("nextPageToken, files(id, name, createdTime, modifiedTime)").
			OrderBy("modifiedTime desc").
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("listing script projects: %v", err)
		}

		if len(resp.Files) == 0 {
			return textResult("No Apps Script projects found.")
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Found %d Apps Script projects:\n", len(resp.Files))
		for _, f := range resp.Files {
			title := f.Name
			if title == "" {
				title = "Untitled"
			}
			scriptID := f.Id
			if scriptID == "" {
				scriptID = "Unknown ID"
			}
			createTime := f.CreatedTime
			if createTime == "" {
				createTime = "Unknown"
			}
			updateTime := f.ModifiedTime
			if updateTime == "" {
				updateTime = "Unknown"
			}
			fmt.Fprintf(&sb, "- %s (ID: %s) Created: %s Modified: %s\n", title, scriptID, createTime, updateTime)
		}

		if resp.NextPageToken != "" {
			fmt.Fprintf(&sb, "\nNext page token: %s", resp.NextPageToken)
		}

		return textResult(sb.String())
	}
}

// --- get_script_project ---

// getScriptProjectIn is the input schema for get_script_project.
type getScriptProjectIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerGetScriptProject(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_script_project",
		Description: "Retrieves complete project details including all source files.",
	}
	mcp.AddTool(s, tool, handleGetScriptProject(getClient))
}

func handleGetScriptProject(getClient httpClientFunc) mcp.ToolHandlerFor[getScriptProjectIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getScriptProjectIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		project, err := svc.Projects.Get(scriptID).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("getting script project: %v", err)
		}

		title := project.Title
		if title == "" {
			title = "Untitled"
		}
		projectScriptID := project.ScriptId
		if projectScriptID == "" {
			projectScriptID = "Unknown"
		}
		creator := "Unknown"
		if project.Creator != nil && project.Creator.Email != "" {
			creator = project.Creator.Email
		}
		createTime := project.CreateTime
		if createTime == "" {
			createTime = "Unknown"
		}
		updateTime := project.UpdateTime
		if updateTime == "" {
			updateTime = "Unknown"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Project: %s (ID: %s)\n", title, projectScriptID)
		fmt.Fprintf(&sb, "Creator: %s\n", creator)
		fmt.Fprintf(&sb, "Created: %s\n", createTime)
		fmt.Fprintf(&sb, "Modified: %s\n", updateTime)

		// Get project content (files) via GetContent
		content, err := svc.Projects.GetContent(scriptID).Context(ctx).Do()
		if err == nil && content != nil && len(content.Files) > 0 {
			sb.WriteString("\nFiles:\n")
			for i, f := range content.Files {
				fileName := f.Name
				if fileName == "" {
					fileName = "Untitled"
				}
				fileType := f.Type
				if fileType == "" {
					fileType = "Unknown"
				}
				fmt.Fprintf(&sb, "%d. %s (%s)\n", i+1, fileName, fileType)
				if f.Source != "" {
					source := f.Source
					if len(source) > 200 {
						source = source[:200] + "..."
					}
					fmt.Fprintf(&sb, "   %s\n\n", source)
				}
			}
		}

		return textResult(sb.String())
	}
}

// --- get_script_content ---

// getScriptContentIn is the input schema for get_script_content.
type getScriptContentIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	FileName        string `json:"file_name" jsonschema:"Name of the file to retrieve."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerGetScriptContent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_script_content",
		Description: "Retrieves content of a specific file within a project.",
	}
	mcp.AddTool(s, tool, handleGetScriptContent(getClient))
}

func handleGetScriptContent(getClient httpClientFunc) mcp.ToolHandlerFor[getScriptContentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getScriptContentIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		fileName := in.FileName

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		content, err := svc.Projects.GetContent(scriptID).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("getting script content: %v", err)
		}

		for _, f := range content.Files {
			if f.Name == fileName {
				fileType := f.Type
				if fileType == "" {
					fileType = "Unknown"
				}
				var sb strings.Builder
				fmt.Fprintf(&sb, "File: %s (%s)\n\n%s", fileName, fileType, f.Source)
				return textResult(sb.String())
			}
		}

		return textResult(fmt.Sprintf("File '%s' not found in project %s", fileName, scriptID))
	}
}

// --- list_deployments ---

// listDeploymentsIn is the input schema for list_deployments.
type listDeploymentsIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerListDeployments(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_deployments",
		Description: "Lists all deployments for a script project.",
	}
	mcp.AddTool(s, tool, handleListDeployments(getClient))
}

func handleListDeployments(getClient httpClientFunc) mcp.ToolHandlerFor[listDeploymentsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listDeploymentsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resp, err := svc.Projects.Deployments.List(scriptID).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("listing deployments: %v", err)
		}

		if len(resp.Deployments) == 0 {
			return textResult(fmt.Sprintf("No deployments found for script: %s", scriptID))
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Deployments for script: %s\n\n", scriptID)
		for i, d := range resp.Deployments {
			deploymentID := d.DeploymentId
			if deploymentID == "" {
				deploymentID = "Unknown"
			}
			description := "No description"
			if d.DeploymentConfig != nil && d.DeploymentConfig.Description != "" {
				description = d.DeploymentConfig.Description
			}
			updateTime := d.UpdateTime
			if updateTime == "" {
				updateTime = "Unknown"
			}
			fmt.Fprintf(&sb, "%d. %s (%s)\n", i+1, description, deploymentID)
			fmt.Fprintf(&sb, "   Updated: %s\n\n", updateTime)
		}

		return textResult(sb.String())
	}
}

// --- list_script_processes ---

// listScriptProcessesIn is the input schema for list_script_processes.
type listScriptProcessesIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Number of results (default: 50)."`
	ScriptID        string `json:"script_id,omitempty" jsonschema:"Optional filter by script ID."`
}

func registerListScriptProcesses(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_script_processes",
		Description: "Lists recent execution processes for user's scripts.",
	}
	mcp.AddTool(s, tool, handleListScriptProcesses(getClient))
}

func handleListScriptProcesses(getClient httpClientFunc) mcp.ToolHandlerFor[listScriptProcessesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listScriptProcessesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 50
		}
		scriptID := in.ScriptID

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		call := svc.Processes.List().PageSize(int64(pageSize))
		if scriptID != "" {
			call = call.UserProcessFilterScriptId(scriptID)
		}

		resp, err := call.Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("listing script processes: %v", err)
		}

		if len(resp.Processes) == 0 {
			return textResult("No recent script executions found.")
		}

		var sb strings.Builder
		sb.WriteString("Recent script executions:\n\n")
		for i, p := range resp.Processes {
			functionName := p.FunctionName
			if functionName == "" {
				functionName = "Unknown"
			}
			processStatus := p.ProcessStatus
			if processStatus == "" {
				processStatus = "Unknown"
			}
			startTime := p.StartTime
			if startTime == "" {
				startTime = "Unknown"
			}
			duration := p.Duration
			if duration == "" {
				duration = "Unknown"
			}
			fmt.Fprintf(&sb, "%d. %s\n", i+1, functionName)
			fmt.Fprintf(&sb, "   Status: %s\n", processStatus)
			fmt.Fprintf(&sb, "   Started: %s\n", startTime)
			fmt.Fprintf(&sb, "   Duration: %s\n\n", duration)
		}

		return textResult(sb.String())
	}
}

// --- list_versions ---

// listVersionsIn is the input schema for list_versions.
type listVersionsIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerListVersions(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_versions",
		Description: "Lists all versions of a script project.",
	}
	mcp.AddTool(s, tool, handleListVersions(getClient))
}

func handleListVersions(getClient httpClientFunc) mcp.ToolHandlerFor[listVersionsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listVersionsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resp, err := svc.Projects.Versions.List(scriptID).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("listing versions: %v", err)
		}

		if len(resp.Versions) == 0 {
			return textResult(fmt.Sprintf("No versions found for script: %s", scriptID))
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Versions for script: %s\n\n", scriptID)
		for _, v := range resp.Versions {
			description := v.Description
			if description == "" {
				description = "No description"
			}
			createTime := v.CreateTime
			if createTime == "" {
				createTime = "Unknown"
			}
			fmt.Fprintf(&sb, "Version %d: %s\n", v.VersionNumber, description)
			fmt.Fprintf(&sb, "   Created: %s\n\n", createTime)
		}

		return textResult(sb.String())
	}
}

// --- get_version ---

// getVersionIn is the input schema for get_version.
type getVersionIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	VersionNumber   int    `json:"version_number" jsonschema:"The version number to retrieve (1, 2, 3, etc.)."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerGetVersion(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_version",
		Description: "Gets details of a specific version.",
	}
	mcp.AddTool(s, tool, handleGetVersion(getClient))
}

func handleGetVersion(getClient httpClientFunc) mcp.ToolHandlerFor[getVersionIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getVersionIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		versionNumber := in.VersionNumber
		if versionNumber == 0 {
			return nil, nil, toolErr("version_number is required")
		}

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		version, err := svc.Projects.Versions.Get(scriptID, int64(versionNumber)).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("getting version: %v", err)
		}

		description := version.Description
		if description == "" {
			description = "No description"
		}
		createTime := version.CreateTime
		if createTime == "" {
			createTime = "Unknown"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Version %d of script: %s\n", version.VersionNumber, scriptID)
		fmt.Fprintf(&sb, "Description: %s\n", description)
		fmt.Fprintf(&sb, "Created: %s", createTime)

		return textResult(sb.String())
	}
}

// --- get_script_metrics ---

// getScriptMetricsIn is the input schema for get_script_metrics.
type getScriptMetricsIn struct {
	ScriptID           string `json:"script_id" jsonschema:"The script project ID."`
	UserGoogleEmail    string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	MetricsGranularity string `json:"metrics_granularity,omitempty" jsonschema:"Granularity of metrics — \"DAILY\" or \"WEEKLY\". Defaults to \"DAILY\"."`
}

func registerGetScriptMetrics(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[getScriptMetricsIn](&mcp.Tool{
		Name:        "get_script_metrics",
		Description: "Gets execution metrics for a script project.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "metrics_granularity", "DAILY", "WEEKLY")
	})
	mcp.AddTool(s, tool, handleGetScriptMetrics(getClient))
}

func handleGetScriptMetrics(getClient httpClientFunc) mcp.ToolHandlerFor[getScriptMetricsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getScriptMetricsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		granularity := in.MetricsGranularity
		if granularity == "" {
			granularity = "DAILY"
		}

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		metrics, err := svc.Projects.GetMetrics(scriptID).
			MetricsGranularity(granularity).
			Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("getting script metrics: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Metrics for script: %s\n", scriptID)
		fmt.Fprintf(&sb, "Granularity: %s\n\n", granularity)

		hasData := false

		if len(metrics.ActiveUsers) > 0 {
			hasData = true
			sb.WriteString("Active Users:\n")
			for _, m := range metrics.ActiveUsers {
				startTime := m.StartTime
				if startTime == "" {
					startTime = "Unknown"
				}
				endTime := m.EndTime
				if endTime == "" {
					endTime = "Unknown"
				}
				fmt.Fprintf(&sb, "  %s to %s: %d users\n", startTime, endTime, m.Value)
			}
			sb.WriteString("\n")
		}

		if len(metrics.TotalExecutions) > 0 {
			hasData = true
			sb.WriteString("Total Executions:\n")
			for _, m := range metrics.TotalExecutions {
				startTime := m.StartTime
				if startTime == "" {
					startTime = "Unknown"
				}
				endTime := m.EndTime
				if endTime == "" {
					endTime = "Unknown"
				}
				fmt.Fprintf(&sb, "  %s to %s: %d executions\n", startTime, endTime, m.Value)
			}
			sb.WriteString("\n")
		}

		if len(metrics.FailedExecutions) > 0 {
			hasData = true
			sb.WriteString("Failed Executions:\n")
			for _, m := range metrics.FailedExecutions {
				startTime := m.StartTime
				if startTime == "" {
					startTime = "Unknown"
				}
				endTime := m.EndTime
				if endTime == "" {
					endTime = "Unknown"
				}
				fmt.Fprintf(&sb, "  %s to %s: %d failures\n", startTime, endTime, m.Value)
			}
			sb.WriteString("\n")
		}

		if !hasData {
			sb.WriteString("No metrics data available for this script.")
		}

		return textResult(sb.String())
	}
}

// --- create_script_project ---

// createScriptProjectIn is the input schema for create_script_project.
type createScriptProjectIn struct {
	Title           string `json:"title" jsonschema:"Project title."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	ParentID        string `json:"parent_id,omitempty" jsonschema:"Optional Drive folder ID or bound container ID."`
}

func registerCreateScriptProject(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_script_project",
		Description: "Creates a new Apps Script project.",
	}
	mcp.AddTool(s, tool, handleCreateScriptProject(getClient))
}

func handleCreateScriptProject(getClient httpClientFunc) mcp.ToolHandlerFor[createScriptProjectIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createScriptProjectIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		title := in.Title
		parentID := in.ParentID

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		body := &script.CreateProjectRequest{
			Title: title,
		}
		if parentID != "" {
			body.ParentId = parentID
		}

		project, err := svc.Projects.Create(body).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("creating script project: %v", err)
		}

		scriptID := project.ScriptId
		if scriptID == "" {
			scriptID = "Unknown"
		}
		editURL := fmt.Sprintf("https://script.google.com/d/%s/edit", scriptID)

		var sb strings.Builder
		fmt.Fprintf(&sb, "Created Apps Script project: %s\n", title)
		fmt.Fprintf(&sb, "Script ID: %s\n", scriptID)
		fmt.Fprintf(&sb, "Edit URL: %s", editURL)

		return textResult(sb.String())
	}
}

// --- update_script_content ---

// updateScriptContentIn is the input schema for update_script_content.
type updateScriptContentIn struct {
	ScriptID        string           `json:"script_id" jsonschema:"The script project ID."`
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Files           []map[string]any `json:"files" jsonschema:"List of file objects with name, type, and source."`
}

func registerUpdateScriptContent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_script_content",
		Description: "Updates or creates files in a script project.",
	}
	mcp.AddTool(s, tool, handleUpdateScriptContent(getClient))
}

func handleUpdateScriptContent(getClient httpClientFunc) mcp.ToolHandlerFor[updateScriptContentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateScriptContentIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID

		if in.Files == nil {
			return nil, nil, toolErr("files is required")
		}

		var files []*script.File
		for _, m := range in.Files {
			f := &script.File{}
			if v, ok := m["name"].(string); ok {
				f.Name = v
			}
			if v, ok := m["type"].(string); ok {
				f.Type = v
			}
			if v, ok := m["source"].(string); ok {
				f.Source = v
			}
			files = append(files, f)
		}

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		content := &script.Content{
			Files: files,
		}

		updated, err := svc.Projects.UpdateContent(scriptID, content).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("updating script content: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Updated script project: %s\n\nModified files:\n", scriptID)
		for _, f := range updated.Files {
			fileName := f.Name
			if fileName == "" {
				fileName = "Untitled"
			}
			fileType := f.Type
			if fileType == "" {
				fileType = "Unknown"
			}
			fmt.Fprintf(&sb, "- %s (%s)\n", fileName, fileType)
		}

		return textResult(sb.String())
	}
}

// --- run_script_function ---

// runScriptFunctionIn is the input schema for run_script_function.
type runScriptFunctionIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	FunctionName    string `json:"function_name" jsonschema:"Name of function to execute."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Parameters      []any  `json:"parameters,omitempty" jsonschema:"Optional list of parameters to pass."`
	DevMode         bool   `json:"dev_mode,omitempty" jsonschema:"Whether to run latest code vs deployed version."`
}

func registerRunScriptFunction(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "run_script_function",
		Description: "Executes a function in a deployed script.",
	}
	mcp.AddTool(s, tool, handleRunScriptFunction(getClient))
}

func handleRunScriptFunction(getClient httpClientFunc) mcp.ToolHandlerFor[runScriptFunctionIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in runScriptFunctionIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		functionName := in.FunctionName
		devMode := in.DevMode

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		execReq := &script.ExecutionRequest{
			Function: functionName,
			DevMode:  devMode,
		}

		// Extract optional parameters
		if params := in.Parameters; len(params) > 0 {
			execReq.Parameters = params
		}

		if devMode {
			execReq.ForceSendFields = append(execReq.ForceSendFields, "DevMode")
		}

		operation, err := svc.Scripts.Run(scriptID, execReq).Context(ctx).Do()
		if err != nil {
			return textResult(fmt.Sprintf("Execution failed\nFunction: %s\nError: %v", functionName, err))
		}

		if operation.Error != nil {
			errorMsg := "Unknown error"
			if operation.Error.Message != "" {
				errorMsg = operation.Error.Message
			}
			return textResult(fmt.Sprintf("Execution failed\nFunction: %s\nError: %s", functionName, errorMsg))
		}

		// Parse the result from the response
		var result any
		if operation.Response != nil {
			// Response is a googleapi.RawMessage — unmarshal to get the result field
			var respMap map[string]any
			if err := json.Unmarshal(operation.Response, &respMap); err == nil {
				result = respMap["result"]
			}
		}

		var sb strings.Builder
		sb.WriteString("Execution successful\n")
		fmt.Fprintf(&sb, "Function: %s\n", functionName)
		fmt.Fprintf(&sb, "Result: %v", result)

		return textResult(sb.String())
	}
}

// --- create_deployment ---

// createDeploymentIn is the input schema for create_deployment.
type createDeploymentIn struct {
	ScriptID           string `json:"script_id" jsonschema:"The script project ID."`
	Description        string `json:"description" jsonschema:"Deployment description."`
	UserGoogleEmail    string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	VersionDescription string `json:"version_description,omitempty" jsonschema:"Optional version description."`
}

func registerCreateDeployment(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_deployment",
		Description: "Creates a new deployment of the script.",
	}
	mcp.AddTool(s, tool, handleCreateDeployment(getClient))
}

func handleCreateDeployment(getClient httpClientFunc) mcp.ToolHandlerFor[createDeploymentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createDeploymentIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		description := in.Description
		versionDescription := in.VersionDescription
		if versionDescription == "" {
			versionDescription = description
		}

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// First, create a new version
		versionBody := &script.Version{
			Description: versionDescription,
		}
		version, err := svc.Projects.Versions.Create(scriptID, versionBody).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("creating version for deployment: %v", err)
		}

		// Now create the deployment with the version number
		deploymentConfig := &script.DeploymentConfig{
			VersionNumber: version.VersionNumber,
			Description:   description,
		}

		deployment, err := svc.Projects.Deployments.Create(scriptID, deploymentConfig).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("creating deployment: %v", err)
		}

		deploymentID := deployment.DeploymentId
		if deploymentID == "" {
			deploymentID = "Unknown"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Created deployment for script: %s\n", scriptID)
		fmt.Fprintf(&sb, "Deployment ID: %s\n", deploymentID)
		fmt.Fprintf(&sb, "Version: %d\n", version.VersionNumber)
		fmt.Fprintf(&sb, "Description: %s", description)

		return textResult(sb.String())
	}
}

// --- update_deployment ---

// updateDeploymentIn is the input schema for update_deployment.
type updateDeploymentIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	DeploymentID    string `json:"deployment_id" jsonschema:"The deployment ID to update."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Description     string `json:"description,omitempty" jsonschema:"Optional new description."`
}

func registerUpdateDeployment(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_deployment",
		Description: "Updates an existing deployment configuration.",
	}
	mcp.AddTool(s, tool, handleUpdateDeployment(getClient))
}

func handleUpdateDeployment(getClient httpClientFunc) mcp.ToolHandlerFor[updateDeploymentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateDeploymentIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		deploymentID := in.DeploymentID
		description := in.Description

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		updateReq := &script.UpdateDeploymentRequest{
			DeploymentConfig: &script.DeploymentConfig{},
		}
		if description != "" {
			updateReq.DeploymentConfig.Description = description
		}

		deployment, err := svc.Projects.Deployments.Update(scriptID, deploymentID, updateReq).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("updating deployment: %v", err)
		}

		respDescription := "No description"
		if deployment.DeploymentConfig != nil && deployment.DeploymentConfig.Description != "" {
			respDescription = deployment.DeploymentConfig.Description
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Updated deployment: %s\n", deploymentID)
		fmt.Fprintf(&sb, "Script: %s\n", scriptID)
		fmt.Fprintf(&sb, "Description: %s", respDescription)

		return textResult(sb.String())
	}
}

// --- delete_deployment ---

// deleteDeploymentIn is the input schema for delete_deployment.
type deleteDeploymentIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	DeploymentID    string `json:"deployment_id" jsonschema:"The deployment ID to delete."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerDeleteDeployment(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_deployment",
		Description: "Deletes a deployment.",
	}
	mcp.AddTool(s, tool, handleDeleteDeployment(getClient))
}

func handleDeleteDeployment(getClient httpClientFunc) mcp.ToolHandlerFor[deleteDeploymentIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteDeploymentIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		deploymentID := in.DeploymentID

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		_, err = svc.Projects.Deployments.Delete(scriptID, deploymentID).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("deleting deployment: %v", err)
		}

		return textResult(fmt.Sprintf("Deleted deployment: %s from script: %s", deploymentID, scriptID))
	}
}

// --- delete_script_project ---

// deleteScriptProjectIn is the input schema for delete_script_project.
type deleteScriptProjectIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID to delete."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerDeleteScriptProject(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_script_project",
		Description: "Deletes an Apps Script project. This permanently deletes the script project. The action cannot be undone.",
	}
	mcp.AddTool(s, tool, handleDeleteScriptProject(getClient))
}

func handleDeleteScriptProject(getClient httpClientFunc) mcp.ToolHandlerFor[deleteScriptProjectIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteScriptProjectIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID

		// Apps Script projects are stored as Drive files — use Drive API to delete
		svc, err := newDriveServiceForScript(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		err = svc.Files.Delete(scriptID).SupportsAllDrives(true).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("deleting script project: %v", err)
		}

		return textResult(fmt.Sprintf("Deleted Apps Script project: %s", scriptID))
	}
}

// --- create_version ---

// createVersionIn is the input schema for create_version.
type createVersionIn struct {
	ScriptID        string `json:"script_id" jsonschema:"The script project ID."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	Description     string `json:"description,omitempty" jsonschema:"Optional description for this version."`
}

func registerCreateVersion(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_version",
		Description: "Creates a new immutable version of a script project. Versions capture a snapshot of the current script code. Once created, versions cannot be modified.",
	}
	mcp.AddTool(s, tool, handleCreateVersion(getClient))
}

func handleCreateVersion(getClient httpClientFunc) mcp.ToolHandlerFor[createVersionIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createVersionIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		scriptID := in.ScriptID
		description := in.Description

		svc, err := newScriptService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		body := &script.Version{}
		if description != "" {
			body.Description = description
		}

		version, err := svc.Projects.Versions.Create(scriptID, body).Context(ctx).Do()
		if err != nil {
			return nil, nil, toolErr("creating version: %v", err)
		}

		createTime := version.CreateTime
		if createTime == "" {
			createTime = "Unknown"
		}
		descriptionText := description
		if descriptionText == "" {
			descriptionText = "No description"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Created version %d for script: %s\n", version.VersionNumber, scriptID)
		fmt.Fprintf(&sb, "Description: %s\n", descriptionText)
		fmt.Fprintf(&sb, "Created: %s", createTime)

		return textResult(sb.String())
	}
}

// --- generate_trigger_code ---

// generateTriggerCodeIn is the input schema for generate_trigger_code.
type generateTriggerCodeIn struct {
	TriggerType  string `json:"trigger_type" jsonschema:"Type of trigger: time_minutes, time_hours, time_daily, time_weekly, on_open, on_edit, on_form_submit, on_change."`
	FunctionName string `json:"function_name" jsonschema:"The function to run when trigger fires (e.g., \"sendDailyReport\")."`
	Schedule     string `json:"schedule,omitempty" jsonschema:"Schedule details (depends on trigger_type): For time_minutes: \"1\", \"5\", \"10\", \"15\", or \"30\". For time_hours: \"1\", \"2\", \"4\", \"6\", \"8\", or \"12\". For time_daily: hour as \"0\"-\"23\". For time_weekly: \"MONDAY\", \"TUESDAY\", etc. For simple triggers: not needed."`
}

func registerGenerateTriggerCode(s *mcp.Server) {
	tool := inputSchema[generateTriggerCodeIn](&mcp.Tool{
		Name:        "generate_trigger_code",
		Description: "Generates Apps Script code for creating triggers. The Apps Script API cannot create triggers directly - they must be created from within Apps Script itself. This tool generates the code you need.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "trigger_type", "time_minutes", "time_hours", "time_daily", "time_weekly", "on_open", "on_edit", "on_form_submit", "on_change")
	})
	mcp.AddTool(s, tool, generateTriggerCodeHandler)
}

func generateTriggerCodeHandler(_ context.Context, _ *mcp.CallToolRequest, in generateTriggerCodeIn) (*mcp.CallToolResult, any, error) {
	triggerType := in.TriggerType
	functionName := in.FunctionName
	schedule := in.Schedule

	var codeLines []string
	var instructions []string

	switch triggerType {
	case "on_open":
		codeLines = []string{
			"// Simple trigger - just rename your function to 'onOpen'",
			"// This runs automatically when the document is opened",
			"function onOpen(e) {",
			fmt.Sprintf("  %s();", functionName),
			"}",
		}
	case "on_edit":
		codeLines = []string{
			"// Simple trigger - just rename your function to 'onEdit'",
			"// This runs automatically when a user edits the spreadsheet",
			"function onEdit(e) {",
			fmt.Sprintf("  %s();", functionName),
			"}",
		}
	case "time_minutes":
		interval := schedule
		if interval == "" {
			interval = "5"
		}
		codeLines = []string{
			"// Run this function ONCE to install the trigger",
			fmt.Sprintf("function createTimeTrigger_%s() {", functionName),
			"  // Delete existing triggers for this function first",
			"  const triggers = ScriptApp.getProjectTriggers();",
			"  triggers.forEach(trigger => {",
			fmt.Sprintf("    if (trigger.getHandlerFunction() === '%s') {", functionName),
			"      ScriptApp.deleteTrigger(trigger);",
			"    }",
			"  });",
			"",
			fmt.Sprintf("  // Create new trigger - runs every %s minutes", interval),
			fmt.Sprintf("  ScriptApp.newTrigger('%s')", functionName),
			"    .timeBased()",
			fmt.Sprintf("    .everyMinutes(%s)", interval),
			"    .create();",
			"",
			fmt.Sprintf("  Logger.log('Trigger created: %s will run every %s minutes');", functionName, interval),
			"}",
		}
	case "time_hours":
		interval := schedule
		if interval == "" {
			interval = "1"
		}
		codeLines = []string{
			"// Run this function ONCE to install the trigger",
			fmt.Sprintf("function createTimeTrigger_%s() {", functionName),
			"  // Delete existing triggers for this function first",
			"  const triggers = ScriptApp.getProjectTriggers();",
			"  triggers.forEach(trigger => {",
			fmt.Sprintf("    if (trigger.getHandlerFunction() === '%s') {", functionName),
			"      ScriptApp.deleteTrigger(trigger);",
			"    }",
			"  });",
			"",
			fmt.Sprintf("  // Create new trigger - runs every %s hour(s)", interval),
			fmt.Sprintf("  ScriptApp.newTrigger('%s')", functionName),
			"    .timeBased()",
			fmt.Sprintf("    .everyHours(%s)", interval),
			"    .create();",
			"",
			fmt.Sprintf("  Logger.log('Trigger created: %s will run every %s hour(s)');", functionName, interval),
			"}",
		}
	case "time_daily":
		hour := schedule
		if hour == "" {
			hour = "9"
		}
		codeLines = []string{
			"// Run this function ONCE to install the trigger",
			fmt.Sprintf("function createDailyTrigger_%s() {", functionName),
			"  // Delete existing triggers for this function first",
			"  const triggers = ScriptApp.getProjectTriggers();",
			"  triggers.forEach(trigger => {",
			fmt.Sprintf("    if (trigger.getHandlerFunction() === '%s') {", functionName),
			"      ScriptApp.deleteTrigger(trigger);",
			"    }",
			"  });",
			"",
			fmt.Sprintf("  // Create new trigger - runs daily at %s:00", hour),
			fmt.Sprintf("  ScriptApp.newTrigger('%s')", functionName),
			"    .timeBased()",
			fmt.Sprintf("    .atHour(%s)", hour),
			"    .everyDays(1)",
			"    .create();",
			"",
			fmt.Sprintf("  Logger.log('Trigger created: %s will run daily at %s:00');", functionName, hour),
			"}",
		}
	case "time_weekly":
		day := strings.ToUpper(schedule)
		if day == "" {
			day = "MONDAY"
		}
		codeLines = []string{
			"// Run this function ONCE to install the trigger",
			fmt.Sprintf("function createWeeklyTrigger_%s() {", functionName),
			"  // Delete existing triggers for this function first",
			"  const triggers = ScriptApp.getProjectTriggers();",
			"  triggers.forEach(trigger => {",
			fmt.Sprintf("    if (trigger.getHandlerFunction() === '%s') {", functionName),
			"      ScriptApp.deleteTrigger(trigger);",
			"    }",
			"  });",
			"",
			fmt.Sprintf("  // Create new trigger - runs weekly on %s", day),
			fmt.Sprintf("  ScriptApp.newTrigger('%s')", functionName),
			"    .timeBased()",
			fmt.Sprintf("    .onWeekDay(ScriptApp.WeekDay.%s)", day),
			"    .atHour(9)",
			"    .create();",
			"",
			fmt.Sprintf("  Logger.log('Trigger created: %s will run every %s at 9:00');", functionName, day),
			"}",
		}
	case "on_form_submit":
		codeLines = []string{
			"// Run this function ONCE to install the trigger",
			"// This must be run from a script BOUND to the Google Form",
			fmt.Sprintf("function createFormSubmitTrigger_%s() {", functionName),
			"  // Delete existing triggers for this function first",
			"  const triggers = ScriptApp.getProjectTriggers();",
			"  triggers.forEach(trigger => {",
			fmt.Sprintf("    if (trigger.getHandlerFunction() === '%s') {", functionName),
			"      ScriptApp.deleteTrigger(trigger);",
			"    }",
			"  });",
			"",
			"  // Create new trigger - runs when form is submitted",
			fmt.Sprintf("  ScriptApp.newTrigger('%s')", functionName),
			"    .forForm(FormApp.getActiveForm())",
			"    .onFormSubmit()",
			"    .create();",
			"",
			fmt.Sprintf("  Logger.log('Trigger created: %s will run on form submit');", functionName),
			"}",
		}
	case "on_change":
		codeLines = []string{
			"// Run this function ONCE to install the trigger",
			"// This must be run from a script BOUND to a Google Sheet",
			fmt.Sprintf("function createChangeTrigger_%s() {", functionName),
			"  // Delete existing triggers for this function first",
			"  const triggers = ScriptApp.getProjectTriggers();",
			"  triggers.forEach(trigger => {",
			fmt.Sprintf("    if (trigger.getHandlerFunction() === '%s') {", functionName),
			"      ScriptApp.deleteTrigger(trigger);",
			"    }",
			"  });",
			"",
			"  // Create new trigger - runs when spreadsheet changes",
			fmt.Sprintf("  ScriptApp.newTrigger('%s')", functionName),
			"    .forSpreadsheet(SpreadsheetApp.getActive())",
			"    .onChange()",
			"    .create();",
			"",
			fmt.Sprintf("  Logger.log('Trigger created: %s will run on spreadsheet change');", functionName),
			"}",
		}
	default:
		return textResult(fmt.Sprintf("Unknown trigger type: %s\n\nValid types: time_minutes, time_hours, time_daily, time_weekly, on_open, on_edit, on_form_submit, on_change", triggerType))
	}

	code := strings.Join(codeLines, "\n")

	if strings.HasPrefix(triggerType, "on_") {
		if triggerType == "on_open" || triggerType == "on_edit" {
			instructions = []string{
				"SIMPLE TRIGGER",
				strings.Repeat("=", 50),
				"",
				"Add this code to your script. Simple triggers run automatically",
				"when the event occurs - no setup function needed.",
				"",
				"Note: Simple triggers have limitations:",
				"- Cannot access services that require authorization",
				"- Cannot run longer than 30 seconds",
				"- Cannot make external HTTP requests",
				"",
				"For more capabilities, use an installable trigger instead.",
				"",
				"CODE TO ADD:",
				strings.Repeat("-", 50),
			}
		} else {
			instructions = []string{
				"INSTALLABLE TRIGGER",
				strings.Repeat("=", 50),
				"",
				"1. Add this code to your script",
				fmt.Sprintf("2. Run the setup function once: createFormSubmitTrigger_%s() or similar", functionName),
				"3. The trigger will then run automatically",
				"",
				"CODE TO ADD:",
				strings.Repeat("-", 50),
			}
		}
	} else {
		instructions = []string{
			"INSTALLABLE TRIGGER",
			strings.Repeat("=", 50),
			"",
			"1. Add this code to your script using update_script_content",
			"2. Run the setup function ONCE (manually in Apps Script editor or via run_script_function)",
			"3. The trigger will then run automatically on schedule",
			"",
			"To check installed triggers: Apps Script editor > Triggers (clock icon)",
			"",
			"CODE TO ADD:",
			strings.Repeat("-", 50),
		}
	}

	return textResult(strings.Join(instructions, "\n") + "\n\n" + code)
}
