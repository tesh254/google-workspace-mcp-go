package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	drive "google.golang.org/api/drive/v3"
	sheets "google.golang.org/api/sheets/v4"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// RegisterSheetsTools registers all Sheets tools with the MCP server.
func RegisterSheetsTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	// Read tools
	registerListSpreadsheets(s, getClient)
	registerGetSpreadsheetInfo(s, getClient)
	registerReadSheetValues(s, getClient)

	// Write tools
	registerModifySheetValues(s, getClient)
	registerFormatSheetRange(s, getClient)
	registerAddConditionalFormatting(s, getClient)
	registerUpdateConditionalFormatting(s, getClient)
	registerDeleteConditionalFormatting(s, getClient)
	registerCreateSpreadsheet(s, getClient)
	registerCreateSheet(s, getClient)

	// Comment tools for Sheets (US-006 / US-019).
	RegisterCommentTools(s, getClient, "spreadsheet", "spreadsheet_id")
}

// newSheetsService creates a sheets.Service for the given user email.
func newSheetsService(ctx context.Context, getClient httpClientFunc, email string) (*sheets.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := sheets.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Sheets service: %w", err)
	}
	return svc, nil
}

// --- A1 range parsing helpers ---

var (
	a1PartRegex      = regexp.MustCompile(`^([A-Za-z]*)(\d*)$`)
	sheetTitleSafeRE = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

// conditionTypes is the set of valid conditional formatting condition types.
var conditionTypes = map[string]bool{
	"NUMBER_GREATER":         true,
	"NUMBER_GREATER_THAN_EQ": true,
	"NUMBER_LESS":            true,
	"NUMBER_LESS_THAN_EQ":    true,
	"NUMBER_EQ":              true,
	"NUMBER_NOT_EQ":          true,
	"TEXT_CONTAINS":          true,
	"TEXT_NOT_CONTAINS":      true,
	"TEXT_STARTS_WITH":       true,
	"TEXT_ENDS_WITH":         true,
	"TEXT_EQ":                true,
	"DATE_BEFORE":            true,
	"DATE_ON_OR_BEFORE":      true,
	"DATE_AFTER":             true,
	"DATE_ON_OR_AFTER":       true,
	"DATE_EQ":                true,
	"DATE_NOT_EQ":            true,
	"DATE_BETWEEN":           true,
	"DATE_NOT_BETWEEN":       true,
	"NOT_BLANK":              true,
	"BLANK":                  true,
	"CUSTOM_FORMULA":         true,
	"ONE_OF_RANGE":           true,
}

// gradientPointTypes is the set of valid gradient point types.
var gradientPointTypes = map[string]bool{
	"MIN":        true,
	"MAX":        true,
	"NUMBER":     true,
	"PERCENT":    true,
	"PERCENTILE": true,
}

// allowedNumberFormats is the set of valid number format types.
var allowedNumberFormats = map[string]bool{
	"NUMBER":               true,
	"NUMBER_WITH_GROUPING": true,
	"CURRENCY":             true,
	"PERCENT":              true,
	"SCIENTIFIC":           true,
	"DATE":                 true,
	"TIME":                 true,
	"DATE_TIME":            true,
	"TEXT":                 true,
}

func columnToIndex(col string) int {
	if col == "" {
		return -1
	}
	result := 0
	for _, ch := range strings.ToUpper(col) {
		result = result*26 + int(ch-'A'+1)
	}
	return result - 1
}

func indexToColumn(idx int) string {
	if idx < 0 {
		return ""
	}
	var result []byte
	idx++ // convert to 1-based
	for idx > 0 {
		idx--
		result = append(result, byte('A'+idx%26))
		idx /= 26
	}
	// reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

func parseA1Part(part string) (col, row int) {
	clean := strings.ReplaceAll(part, "$", "")
	m := a1PartRegex.FindStringSubmatch(clean)
	if m == nil {
		return -1, -1
	}
	col = columnToIndex(m[1])
	if m[2] != "" {
		r := 0
		for _, c := range m[2] {
			r = r*10 + int(c-'0')
		}
		row = r - 1 // zero-based
	} else {
		row = -1
	}
	return col, row
}

func splitSheetAndRange(rangeName string) (sheetName, a1Range string) {
	if !strings.Contains(rangeName, "!") {
		return "", rangeName
	}
	if strings.HasPrefix(rangeName, "'") {
		closing := strings.Index(rangeName, "'!")
		if closing != -1 {
			sheetName = strings.ReplaceAll(rangeName[1:closing], "''", "'")
			a1Range = rangeName[closing+2:]
			return sheetName, a1Range
		}
	}
	parts := strings.SplitN(rangeName, "!", 2)
	sheetName = strings.Trim(strings.TrimSpace(parts[0]), "'")
	a1Range = parts[1]
	return sheetName, a1Range
}

// sheetsGridRange represents a Sheets API GridRange.
type sheetsGridRange struct {
	SheetID          int64
	StartRowIndex    int64
	EndRowIndex      int64
	StartColumnIndex int64
	EndColumnIndex   int64
	hasStartRow      bool
	hasEndRow        bool
	hasStartCol      bool
	hasEndCol        bool
}

func (g *sheetsGridRange) toMap() map[string]int64 {
	m := map[string]int64{"sheetId": g.SheetID}
	if g.hasStartRow {
		m["startRowIndex"] = g.StartRowIndex
	}
	if g.hasEndRow {
		m["endRowIndex"] = g.EndRowIndex
	}
	if g.hasStartCol {
		m["startColumnIndex"] = g.StartColumnIndex
	}
	if g.hasEndCol {
		m["endColumnIndex"] = g.EndColumnIndex
	}
	return m
}

func (g *sheetsGridRange) toSheetsGridRange() *sheets.GridRange {
	r := &sheets.GridRange{SheetId: g.SheetID}
	var force []string
	force = append(force, "SheetId")
	if g.hasStartRow {
		r.StartRowIndex = g.StartRowIndex
		force = append(force, "StartRowIndex")
	}
	if g.hasEndRow {
		r.EndRowIndex = g.EndRowIndex
		force = append(force, "EndRowIndex")
	}
	if g.hasStartCol {
		r.StartColumnIndex = g.StartColumnIndex
		force = append(force, "StartColumnIndex")
	}
	if g.hasEndCol {
		r.EndColumnIndex = g.EndColumnIndex
		force = append(force, "EndColumnIndex")
	}
	r.ForceSendFields = force
	return r
}

type sheetInfo struct {
	SheetID          int64
	Title            string
	Rows             int64
	Cols             int64
	ConditionalRules []*sheets.ConditionalFormatRule
}

func parseA1Range(rangeName string, sheetInfos []sheetInfo) (*sheetsGridRange, error) {
	sheetName, a1Range := splitSheetAndRange(rangeName)

	if len(sheetInfos) == 0 {
		return nil, fmt.Errorf("spreadsheet has no sheets")
	}

	var target *sheetInfo
	if sheetName != "" {
		for i := range sheetInfos {
			if sheetInfos[i].Title == sheetName {
				target = &sheetInfos[i]
				break
			}
		}
		if target == nil {
			var titles []string
			for _, s := range sheetInfos {
				titles = append(titles, s.Title)
			}
			return nil, fmt.Errorf("sheet '%s' not found. Available sheets: %s", sheetName, strings.Join(titles, ", "))
		}
	} else {
		target = &sheetInfos[0]
	}

	if a1Range == "" {
		return nil, fmt.Errorf("A1-style range must not be empty")
	}

	var startPart, endPart string
	if strings.Contains(a1Range, ":") {
		parts := strings.SplitN(a1Range, ":", 2)
		startPart = parts[0]
		endPart = parts[1]
	} else {
		startPart = a1Range
		endPart = a1Range
	}

	startCol, startRow := parseA1Part(startPart)
	endCol, endRow := parseA1Part(endPart)

	gr := &sheetsGridRange{SheetID: target.SheetID}
	if startRow >= 0 {
		gr.StartRowIndex = int64(startRow)
		gr.hasStartRow = true
	}
	if startCol >= 0 {
		gr.StartColumnIndex = int64(startCol)
		gr.hasStartCol = true
	}
	if endRow >= 0 {
		gr.EndRowIndex = int64(endRow + 1) // exclusive
		gr.hasEndRow = true
	}
	if endCol >= 0 {
		gr.EndColumnIndex = int64(endCol + 1) // exclusive
		gr.hasEndCol = true
	}

	return gr, nil
}

// parseHexColorSheets converts a hex color like '#RRGGBB' to Sheets API color floats.
func parseHexColorSheets(color string) (red, green, blue float64, ok bool) {
	if color == "" {
		return 0, 0, 0, false
	}
	trimmed := strings.TrimSpace(color)
	trimmed = strings.TrimPrefix(trimmed, "#")
	if len(trimmed) != 6 {
		return 0, 0, 0, false
	}
	var r, g, b int
	_, err := fmt.Sscanf(trimmed, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64(r) / 255.0, float64(g) / 255.0, float64(b) / 255.0, true
}

func colorToHex(c *sheets.Color) string {
	if c == nil {
		return ""
	}
	r := int(math.Round(c.Red * 255))
	g := int(math.Round(c.Green * 255))
	b := int(math.Round(c.Blue * 255))
	if r < 0 {
		r = 0
	}
	if r > 255 {
		r = 255
	}
	if g < 0 {
		g = 0
	}
	if g > 255 {
		g = 255
	}
	if b < 0 {
		b = 0
	}
	if b > 255 {
		b = 255
	}
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// fetchSheetsMetadata returns sheet infos and a sheetID->title map.
func fetchSheetsMetadata(svc *sheets.Service, spreadsheetID string) ([]sheetInfo, map[int64]string, error) {
	resp, err := svc.Spreadsheets.Get(spreadsheetID).
		Fields("sheets(properties(sheetId,title),conditionalFormats)").
		Do()
	if err != nil {
		return nil, nil, fmt.Errorf("getting spreadsheet metadata: %w", err)
	}

	var infos []sheetInfo
	titles := make(map[int64]string)
	for _, sh := range resp.Sheets {
		props := sh.Properties
		info := sheetInfo{
			SheetID:          props.SheetId,
			Title:            props.Title,
			ConditionalRules: sh.ConditionalFormats,
		}
		infos = append(infos, info)
		titles[props.SheetId] = props.Title
	}
	return infos, titles, nil
}

// gridRangeToA1 converts a GridRange to an A1-like string using known sheet titles.
func gridRangeToA1(gr *sheets.GridRange, titles map[int64]string) string {
	title, ok := titles[gr.SheetId]
	if !ok {
		title = fmt.Sprintf("Sheet %d", gr.SheetId)
	}

	startRow := gr.StartRowIndex
	endRow := gr.EndRowIndex
	startCol := gr.StartColumnIndex
	endCol := gr.EndColumnIndex

	hasStart := startRow > 0 || startCol > 0
	hasEnd := endRow > 0 || endCol > 0

	if !hasStart && !hasEnd {
		return title
	}

	rowLabel := func(idx int64) string {
		if idx <= 0 {
			return ""
		}
		return fmt.Sprintf("%d", idx)
	}
	colLabel := func(idx int64) string {
		if idx <= 0 {
			return ""
		}
		return indexToColumn(int(idx) - 1)
	}

	// For start: use start indices directly (zero-based → display as 1-based row)
	startL := fmt.Sprintf("%s%s", indexToColumn(int(startCol)), fmt.Sprintf("%d", startRow+1))
	// For end: indices are exclusive, subtract 1 for display
	endL := ""
	if hasEnd {
		endColDisp := ""
		if endCol > 0 {
			endColDisp = colLabel(endCol)
		}
		endRowDisp := ""
		if endRow > 0 {
			endRowDisp = rowLabel(endRow)
		}
		endL = fmt.Sprintf("%s%s", endColDisp, endRowDisp)
	}

	rangeRef := startL
	if endL != "" && endL != startL {
		rangeRef = startL + ":" + endL
	}
	if rangeRef != "" {
		return title + "!" + rangeRef
	}
	return title
}

// summarizeConditionalRule produces a concise human-readable summary.
func summarizeConditionalRule(rule *sheets.ConditionalFormatRule, index int, titles map[int64]string) string {
	var rangeLabels []string
	for _, r := range rule.Ranges {
		rangeLabels = append(rangeLabels, gridRangeToA1(r, titles))
	}
	if len(rangeLabels) == 0 {
		rangeLabels = []string{"(no range)"}
	}

	if rule.BooleanRule != nil {
		br := rule.BooleanRule
		condType := "UNKNOWN"
		var condValues []string
		if br.Condition != nil {
			condType = br.Condition.Type
			for _, v := range br.Condition.Values {
				condValues = append(condValues, v.UserEnteredValue)
			}
		}
		valueDesc := ""
		if len(condValues) > 0 {
			valueDesc = fmt.Sprintf(" values=%v", condValues)
		}

		var fmtParts []string
		if br.Format != nil {
			if bgHex := colorToHex(br.Format.BackgroundColor); bgHex != "" && bgHex != "#000000" {
				fmtParts = append(fmtParts, "bg "+bgHex)
			} else if br.Format.BackgroundColor != nil {
				fmtParts = append(fmtParts, "bg "+colorToHex(br.Format.BackgroundColor))
			}
			if br.Format.TextFormat != nil {
				if fgHex := colorToHex(br.Format.TextFormat.ForegroundColor); fgHex != "" {
					fmtParts = append(fmtParts, "text "+fgHex)
				}
			}
		}
		fmtDesc := "no format"
		if len(fmtParts) > 0 {
			fmtDesc = strings.Join(fmtParts, ", ")
		}

		return fmt.Sprintf("[%d] %s%s -> %s on %s", index, condType, valueDesc, fmtDesc, strings.Join(rangeLabels, ", "))
	}

	if rule.GradientRule != nil {
		gr := rule.GradientRule
		var points []string
		for _, pt := range []struct {
			name  string
			point *sheets.InterpolationPoint
		}{
			{"minpoint", gr.Minpoint},
			{"midpoint", gr.Midpoint},
			{"maxpoint", gr.Maxpoint},
		} {
			if pt.point == nil {
				continue
			}
			desc := pt.point.Type
			if pt.point.Value != "" {
				desc += ":" + pt.point.Value
			}
			if hex := colorToHex(pt.point.Color); hex != "" {
				desc += " " + hex
			}
			points = append(points, desc)
		}
		gradientDesc := "gradient"
		if len(points) > 0 {
			gradientDesc = strings.Join(points, " | ")
		}
		return fmt.Sprintf("[%d] gradient -> %s on %s", index, gradientDesc, strings.Join(rangeLabels, ", "))
	}

	return fmt.Sprintf("[%d] (unknown rule) on %s", index, strings.Join(rangeLabels, ", "))
}

// formatConditionalRulesSection builds a multi-line string describing conditional formatting rules.
func formatConditionalRulesSection(sheetTitle string, rules []*sheets.ConditionalFormatRule, titles map[int64]string, indent string) string {
	if len(rules) == 0 {
		return fmt.Sprintf("%sConditional formats for \"%s\": none.", indent, sheetTitle)
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("%sConditional formats for \"%s\" (%d):", indent, sheetTitle, len(rules)))
	for i, rule := range rules {
		lines = append(lines, fmt.Sprintf("%s  %s", indent, summarizeConditionalRule(rule, i, titles)))
	}
	return strings.Join(lines, "\n")
}

// selectSheet finds a sheet by name, or returns the first if name is empty.
func selectSheet(infos []sheetInfo, sheetName string) (*sheetInfo, error) {
	if len(infos) == 0 {
		return nil, fmt.Errorf("spreadsheet has no sheets")
	}
	if sheetName == "" {
		return &infos[0], nil
	}
	for i := range infos {
		if infos[i].Title == sheetName {
			return &infos[i], nil
		}
	}
	var titles []string
	for _, s := range infos {
		titles = append(titles, s.Title)
	}
	return nil, fmt.Errorf("sheet '%s' not found. Available sheets: %s", sheetName, strings.Join(titles, ", "))
}

// parseConditionValues normalizes condition values from any/string/array into []string.
func parseConditionValues(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case string:
		var parsed []any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("condition_values must be a list or a JSON-encoded list")
		}
		var result []string
		for _, item := range parsed {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result, nil
	case []any:
		var result []string
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result, nil
	default:
		return []string{fmt.Sprintf("%v", v)}, nil
	}
}

// parseGradientPoints normalizes gradient points from any/string/array into structured points.
type gradientPoint struct {
	Type  string
	Value string
	Color *sheets.Color
}

func parseGradientPoints(raw any) ([]gradientPoint, error) {
	if raw == nil {
		return nil, nil
	}
	var items []any
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &items); err != nil {
			return nil, fmt.Errorf("gradient_points must be a list or JSON-encoded list of points")
		}
	case []any:
		items = v
	default:
		return nil, fmt.Errorf("gradient_points must be a list of point objects")
	}

	if len(items) < 2 || len(items) > 3 {
		return nil, fmt.Errorf("provide 2 or 3 gradient points (min/max or min/mid/max)")
	}

	var points []gradientPoint
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("gradient_points[%d] must be an object with type/color", i)
		}
		ptType, _ := m["type"].(string)
		if ptType == "" || !gradientPointTypes[strings.ToUpper(ptType)] {
			return nil, fmt.Errorf("gradient_points[%d].type must be one of MIN, MAX, NUMBER, PERCENT, PERCENTILE", i)
		}
		colorRaw, _ := m["color"].(string)
		r, g, b, ok := parseHexColorSheets(colorRaw)
		if !ok {
			return nil, fmt.Errorf("gradient_points[%d].color is required (hex format)", i)
		}
		pt := gradientPoint{
			Type: strings.ToUpper(ptType),
			Color: &sheets.Color{
				Red:             r,
				Green:           g,
				Blue:            b,
				ForceSendFields: []string{"Red", "Green", "Blue"},
			},
		}
		if val, exists := m["value"]; exists && val != nil {
			pt.Value = fmt.Sprintf("%v", val)
		}
		points = append(points, pt)
	}
	return points, nil
}

func gradientPointToInterpolation(pt gradientPoint) *sheets.InterpolationPoint {
	ip := &sheets.InterpolationPoint{
		Type:  pt.Type,
		Color: pt.Color,
	}
	if pt.Value != "" {
		ip.Value = pt.Value
	}
	return ip
}

// --- list_spreadsheets ---

func registerListSpreadsheets(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_spreadsheets",
		Description: "Lists spreadsheets from Google Drive that the user has access to.",
	}
	mcp.AddTool(s, tool, handleListSpreadsheets(getClient))
}

// listSpreadsheetsIn is the input schema for list_spreadsheets.
type listSpreadsheetsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	MaxResults      int    `json:"max_results,omitempty" jsonschema:"Maximum number of spreadsheets to return (default 25)"`
}

func handleListSpreadsheets(getClient httpClientFunc) mcp.ToolHandlerFor[listSpreadsheetsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listSpreadsheetsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		maxResults := in.MaxResults
		if maxResults == 0 {
			maxResults = 25
		}

		httpClient, err := getClient(ctx, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}
		drvSvc, err := drive.New(httpClient)
		if err != nil {
			return nil, nil, toolErr("creating Drive service: %v", err)
		}

		resp, err := drvSvc.Files.List().
			Q("mimeType='application/vnd.google-apps.spreadsheet'").
			PageSize(int64(maxResults)).
			Fields("files(id,name,modifiedTime,webViewLink)").
			OrderBy("modifiedTime desc").
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Do()
		if err != nil {
			return nil, nil, toolErr("listing spreadsheets: %v", err)
		}

		files := resp.Files
		if len(files) == 0 {
			return textResult(fmt.Sprintf("No spreadsheets found for %s.", email))
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully listed %d spreadsheets for %s:", len(files), email)
		for _, f := range files {
			modified := f.ModifiedTime
			if modified == "" {
				modified = "Unknown"
			}
			link := f.WebViewLink
			if link == "" {
				link = "No link"
			}
			fmt.Fprintf(&b, "\n- \"%s\" (ID: %s) | Modified: %s | Link: %s", f.Name, f.Id, modified, link)
		}
		return textResult(b.String())
	}
}

// --- get_spreadsheet_info ---

func registerGetSpreadsheetInfo(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_spreadsheet_info",
		Description: "Gets information about a specific spreadsheet including its sheets.",
	}
	mcp.AddTool(s, tool, handleGetSpreadsheetInfo(getClient))
}

// getSpreadsheetInfoIn is the input schema for get_spreadsheet_info.
type getSpreadsheetInfoIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID   string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
}

func handleGetSpreadsheetInfo(getClient httpClientFunc) mcp.ToolHandlerFor[getSpreadsheetInfoIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getSpreadsheetInfoIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		spreadsheet, err := svc.Spreadsheets.Get(spreadsheetID).
			Fields("spreadsheetId,properties(title,locale),sheets(properties(title,sheetId,gridProperties(rowCount,columnCount)),conditionalFormats)").
			Do()
		if err != nil {
			return nil, nil, toolErr("getting spreadsheet info: %v", err)
		}

		title := spreadsheet.Properties.Title
		locale := spreadsheet.Properties.Locale
		if locale == "" {
			locale = "Unknown"
		}

		// Build sheetID -> title map for conditional rule display
		sheetTitles := make(map[int64]string)
		for _, sh := range spreadsheet.Sheets {
			sheetTitles[sh.Properties.SheetId] = sh.Properties.Title
		}

		var sheetsInfo []string
		for _, sh := range spreadsheet.Sheets {
			props := sh.Properties
			sheetName := props.Title
			sheetID := props.SheetId
			rows := int64(0)
			cols := int64(0)
			if props.GridProperties != nil {
				rows = props.GridProperties.RowCount
				cols = props.GridProperties.ColumnCount
			}
			rules := sh.ConditionalFormats

			sheetsInfo = append(sheetsInfo,
				fmt.Sprintf("  - \"%s\" (ID: %d) | Size: %dx%d | Conditional formats: %d",
					sheetName, sheetID, rows, cols, len(rules)))

			if len(rules) > 0 {
				sheetsInfo = append(sheetsInfo,
					formatConditionalRulesSection(sheetName, rules, sheetTitles, "    "))
			}
		}

		sheetsSection := "  No sheets found"
		if len(sheetsInfo) > 0 {
			sheetsSection = strings.Join(sheetsInfo, "\n")
		}

		result := fmt.Sprintf("Spreadsheet: \"%s\" (ID: %s) | Locale: %s\nSheets (%d):\n%s",
			title, spreadsheetID, locale, len(spreadsheet.Sheets), sheetsSection)

		return textResult(result)
	}
}

// --- read_sheet_values ---

func registerReadSheetValues(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "read_sheet_values",
		Description: "Reads values from a specific range in a Google Sheet.",
	}
	mcp.AddTool(s, tool, handleReadSheetValues(getClient))
}

// readSheetValuesIn is the input schema for read_sheet_values.
type readSheetValuesIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID   string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	RangeName       string `json:"range_name,omitempty" jsonschema:"The range to read (default A1:Z1000)"`
}

func handleReadSheetValues(getClient httpClientFunc) mcp.ToolHandlerFor[readSheetValuesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in readSheetValuesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		rangeName := in.RangeName
		if rangeName == "" {
			rangeName = "A1:Z1000"
		}

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		resp, err := svc.Spreadsheets.Values.Get(spreadsheetID, rangeName).Do()
		if err != nil {
			return nil, nil, toolErr("reading sheet values: %v", err)
		}

		values := resp.Values
		if len(values) == 0 {
			return textResult(fmt.Sprintf("No data found in range '%s' for %s.", rangeName, email))
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully read %d rows from range '%s' in spreadsheet %s for %s:",
			len(values), rangeName, spreadsheetID, email)

		maxRows := 50
		if len(values) < maxRows {
			maxRows = len(values)
		}
		// Determine first row width for padding
		firstRowLen := 0
		if len(values) > 0 {
			firstRowLen = len(values[0])
		}
		for i := 0; i < maxRows; i++ {
			row := values[i]
			// Pad row with empty strings
			padded := make([]any, firstRowLen)
			copy(padded, row)
			fmt.Fprintf(&b, "\nRow %2d: %v", i+1, padded)
		}
		if len(values) > 50 {
			fmt.Fprintf(&b, "\n... and %d more rows", len(values)-50)
		}

		return textResult(b.String())
	}
}

// --- modify_sheet_values ---

func registerModifySheetValues(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "modify_sheet_values",
		Description: "Modifies values in a specific range of a Google Sheet - can write, update, or clear values.",
	}
	mcp.AddTool(s, tool, handleModifySheetValues(getClient))
}

// modifySheetValuesIn is the input schema for modify_sheet_values.
type modifySheetValuesIn struct {
	UserGoogleEmail  string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID    string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	RangeName        string `json:"range_name" jsonschema:"The range to modify"`
	Values           any    `json:"values,omitempty" jsonschema:"2D array of values as JSON string"`
	ValueInputOption string `json:"value_input_option,omitempty" jsonschema:"RAW or USER_ENTERED (default USER_ENTERED)"`
	ClearValues      bool   `json:"clear_values,omitempty" jsonschema:"If true, clears the range instead of writing"`
}

func handleModifySheetValues(getClient httpClientFunc) mcp.ToolHandlerFor[modifySheetValuesIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in modifySheetValuesIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		rangeName := in.RangeName
		valueInputOption := in.ValueInputOption
		if valueInputOption == "" {
			valueInputOption = "USER_ENTERED"
		}
		clearValues := in.ClearValues

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		if clearValues {
			resp, err := svc.Spreadsheets.Values.Clear(spreadsheetID, rangeName, &sheets.ClearValuesRequest{}).Do()
			if err != nil {
				return nil, nil, toolErr("clearing range: %v", err)
			}
			clearedRange := rangeName
			if resp.ClearedRange != "" {
				clearedRange = resp.ClearedRange
			}
			return textResult(
				fmt.Sprintf("Successfully cleared range '%s' in spreadsheet %s for %s.",
					clearedRange, spreadsheetID, email))
		}

		// Parse values
		rawValues := in.Values
		if rawValues == nil {
			return nil, nil, toolErr("either 'values' must be provided or 'clear_values' must be true")
		}

		var values [][]any
		switch v := rawValues.(type) {
		case string:
			if err := json.Unmarshal([]byte(v), &values); err != nil {
				return nil, nil, toolErr("invalid JSON format for values: %v", err)
			}
		case []any:
			for i, row := range v {
				rowSlice, ok := row.([]any)
				if !ok {
					return nil, nil, toolErr("row %d must be a list", i)
				}
				values = append(values, rowSlice)
			}
		default:
			return nil, nil, toolErr("values must be a 2D array or JSON string")
		}

		if len(values) == 0 {
			return nil, nil, toolErr("either 'values' must be provided or 'clear_values' must be true")
		}

		// Convert [][]any to [][]interface{} for the API
		var apiValues [][]interface{}
		for _, row := range values {
			apiRow := make([]interface{}, len(row))
			for j, cell := range row {
				apiRow[j] = cell
			}
			apiValues = append(apiValues, apiRow)
		}

		resp, err := svc.Spreadsheets.Values.Update(spreadsheetID, rangeName, &sheets.ValueRange{
			Values: apiValues,
		}).
			ValueInputOption(valueInputOption).
			IncludeValuesInResponse(true).
			ResponseValueRenderOption("FORMATTED_VALUE").
			Do()
		if err != nil {
			return nil, nil, toolErr("updating sheet values: %v", err)
		}

		result := fmt.Sprintf("Successfully updated range '%s' in spreadsheet %s for %s. Updated: %d cells, %d rows, %d columns.",
			rangeName, spreadsheetID, email, resp.UpdatedCells, resp.UpdatedRows, resp.UpdatedColumns)

		return textResult(result)
	}
}

// --- format_sheet_range ---

func registerFormatSheetRange(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name: "format_sheet_range",
		Description: "Applies formatting to a range: background/text color and number/date formats.\n\n" +
			"Colors accept hex strings (#RRGGBB). Number formats follow Sheets types (e.g., NUMBER, CURRENCY, DATE, TIME, DATE_TIME, PERCENT, TEXT, SCIENTIFIC).",
	}
	mcp.AddTool(s, tool, handleFormatSheetRange(getClient))
}

// formatSheetRangeIn is the input schema for format_sheet_range.
type formatSheetRangeIn struct {
	UserGoogleEmail     string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID       string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	RangeName           string `json:"range_name" jsonschema:"A1-style range"`
	BackgroundColor     string `json:"background_color,omitempty" jsonschema:"Hex background color (e.g., #FFEECC)"`
	TextColor           string `json:"text_color,omitempty" jsonschema:"Hex text color (e.g., #000000)"`
	NumberFormatType    string `json:"number_format_type,omitempty" jsonschema:"Sheets number format type (e.g., DATE)"`
	NumberFormatPattern string `json:"number_format_pattern,omitempty" jsonschema:"Custom pattern for the number format"`
}

func handleFormatSheetRange(getClient httpClientFunc) mcp.ToolHandlerFor[formatSheetRangeIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in formatSheetRangeIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		rangeName := in.RangeName
		bgColor := in.BackgroundColor
		txtColor := in.TextColor
		numFmtType := in.NumberFormatType
		numFmtPattern := in.NumberFormatPattern

		if bgColor == "" && txtColor == "" && numFmtType == "" {
			return nil, nil, toolErr("provide at least one of background_color, text_color, or number_format_type")
		}

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		// Get sheet metadata for A1 parsing
		meta, err := svc.Spreadsheets.Get(spreadsheetID).
			Fields("sheets(properties(sheetId,title))").
			Do()
		if err != nil {
			return nil, nil, toolErr("getting spreadsheet metadata: %v", err)
		}
		var sheetInfos []sheetInfo
		for _, sh := range meta.Sheets {
			sheetInfos = append(sheetInfos, sheetInfo{
				SheetID: sh.Properties.SheetId,
				Title:   sh.Properties.Title,
			})
		}

		gridRange, err := parseA1Range(rangeName, sheetInfos)
		if err != nil {
			return nil, nil, err
		}

		cellFormat := &sheets.CellFormat{}
		var fields []string

		if bgColor != "" {
			r, g, b, ok := parseHexColorSheets(bgColor)
			if !ok {
				return nil, nil, toolErr("invalid background_color format: %s", bgColor)
			}
			cellFormat.BackgroundColor = &sheets.Color{
				Red: r, Green: g, Blue: b,
				ForceSendFields: []string{"Red", "Green", "Blue"},
			}
			fields = append(fields, "userEnteredFormat.backgroundColor")
		}

		if txtColor != "" {
			r, g, b, ok := parseHexColorSheets(txtColor)
			if !ok {
				return nil, nil, toolErr("invalid text_color format: %s", txtColor)
			}
			cellFormat.TextFormat = &sheets.TextFormat{
				ForegroundColor: &sheets.Color{
					Red: r, Green: g, Blue: b,
					ForceSendFields: []string{"Red", "Green", "Blue"},
				},
			}
			fields = append(fields, "userEnteredFormat.textFormat.foregroundColor")
		}

		if numFmtType != "" {
			normalized := strings.ToUpper(numFmtType)
			if !allowedNumberFormats[normalized] {
				return nil, nil, toolErr("number_format_type must be one of: %v", sortedKeys(allowedNumberFormats))
			}
			nf := &sheets.NumberFormat{Type: normalized}
			if numFmtPattern != "" {
				nf.Pattern = numFmtPattern
			}
			cellFormat.NumberFormat = nf
			fields = append(fields, "userEnteredFormat.numberFormat")
		}

		_, err = svc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					RepeatCell: &sheets.RepeatCellRequest{
						Range:  gridRange.toSheetsGridRange(),
						Cell:   &sheets.CellData{UserEnteredFormat: cellFormat},
						Fields: strings.Join(fields, ","),
					},
				},
			},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("applying formatting: %v", err)
		}

		var appliedParts []string
		if bgColor != "" {
			appliedParts = append(appliedParts, "background "+bgColor)
		}
		if txtColor != "" {
			appliedParts = append(appliedParts, "text "+txtColor)
		}
		if numFmtType != "" {
			nfDesc := strings.ToUpper(numFmtType)
			if numFmtPattern != "" {
				nfDesc += fmt.Sprintf(" (pattern: %s)", numFmtPattern)
			}
			appliedParts = append(appliedParts, "format "+nfDesc)
		}

		return textResult(
			fmt.Sprintf("Applied formatting to range '%s' in spreadsheet %s for %s: %s.",
				rangeName, spreadsheetID, email, strings.Join(appliedParts, ", ")))
	}
}

// sortedKeys returns sorted keys from a map for display.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// --- add_conditional_formatting ---

func registerAddConditionalFormatting(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "add_conditional_formatting",
		Description: "Adds a conditional formatting rule to a range.",
	}
	mcp.AddTool(s, tool, handleAddConditionalFormatting(getClient))
}

// addConditionalFormattingIn is the input schema for add_conditional_formatting.
type addConditionalFormattingIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID   string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	RangeName       string `json:"range_name" jsonschema:"A1-style range"`
	ConditionType   string `json:"condition_type" jsonschema:"Sheets condition type"`
	ConditionValues any    `json:"condition_values,omitempty" jsonschema:"Values for the condition (JSON array or string)"`
	BackgroundColor string `json:"background_color,omitempty" jsonschema:"Hex background color"`
	TextColor       string `json:"text_color,omitempty" jsonschema:"Hex text color"`
	RuleIndex       *int   `json:"rule_index,omitempty" jsonschema:"Position to insert the rule (0-based)"`
	GradientPoints  any    `json:"gradient_points,omitempty" jsonschema:"Gradient points as JSON array"`
}

func handleAddConditionalFormatting(getClient httpClientFunc) mcp.ToolHandlerFor[addConditionalFormattingIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in addConditionalFormattingIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		rangeName := in.RangeName
		condType := in.ConditionType

		condValuesRaw := in.ConditionValues
		bgColor := in.BackgroundColor
		txtColor := in.TextColor
		ruleIndex := -1
		if in.RuleIndex != nil {
			ruleIndex = *in.RuleIndex
		}
		gradientPointsRaw := in.GradientPoints

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		// Fetch sheets with rules
		infos, titles, err := fetchSheetsMetadata(svc, spreadsheetID)
		if err != nil {
			return nil, nil, err
		}

		gridRange, err := parseA1Range(rangeName, infos)
		if err != nil {
			return nil, nil, err
		}

		// Find target sheet
		var targetInfo *sheetInfo
		for i := range infos {
			if infos[i].SheetID == gridRange.SheetID {
				targetInfo = &infos[i]
				break
			}
		}
		if targetInfo == nil {
			return nil, nil, toolErr("target sheet not found")
		}

		currentRules := targetInfo.ConditionalRules
		insertAt := len(currentRules)
		if ruleIndex >= 0 {
			insertAt = ruleIndex
			if insertAt > len(currentRules) {
				return nil, nil, toolErr(
					"rule_index %d is out of range for sheet '%s' (current count: %d)",
					insertAt, targetInfo.Title, len(currentRules))
			}
		}

		// Parse gradient points
		gPoints, err := parseGradientPoints(gradientPointsRaw)
		if err != nil {
			return nil, nil, err
		}

		var newRule *sheets.ConditionalFormatRule
		var ruleDesc string
		var valuesDesc string
		var appliedParts []string

		sheetsRange := gridRange.toSheetsGridRange()

		if len(gPoints) > 0 {
			// Gradient rule
			gradientRule := &sheets.GradientRule{}
			if len(gPoints) == 2 {
				gradientRule.Minpoint = gradientPointToInterpolation(gPoints[0])
				gradientRule.Maxpoint = gradientPointToInterpolation(gPoints[1])
			} else {
				gradientRule.Minpoint = gradientPointToInterpolation(gPoints[0])
				gradientRule.Midpoint = gradientPointToInterpolation(gPoints[1])
				gradientRule.Maxpoint = gradientPointToInterpolation(gPoints[2])
			}
			newRule = &sheets.ConditionalFormatRule{
				Ranges:       []*sheets.GridRange{sheetsRange},
				GradientRule: gradientRule,
			}
			ruleDesc = "gradient"
			appliedParts = append(appliedParts, fmt.Sprintf("gradient points %d", len(gPoints)))
		} else {
			// Boolean rule
			if bgColor == "" && txtColor == "" {
				return nil, nil, toolErr("provide at least one of background_color or text_color for the rule format")
			}

			condTypeNorm := strings.ToUpper(condType)
			if !conditionTypes[condTypeNorm] {
				return nil, nil, toolErr("condition_type must be one of: %v", sortedKeys(conditionTypes))
			}

			condValues, err := parseConditionValues(condValuesRaw)
			if err != nil {
				return nil, nil, err
			}

			condition := &sheets.BooleanCondition{Type: condTypeNorm}
			if len(condValues) > 0 {
				for _, v := range condValues {
					condition.Values = append(condition.Values, &sheets.ConditionValue{
						UserEnteredValue: v,
					})
				}
				valuesDesc = fmt.Sprintf(" with values %v", condValues)
			}

			format := &sheets.CellFormat{}
			if bgColor != "" {
				r, g, b, ok := parseHexColorSheets(bgColor)
				if !ok {
					return nil, nil, toolErr("invalid background_color: %s", bgColor)
				}
				format.BackgroundColor = &sheets.Color{
					Red: r, Green: g, Blue: b,
					ForceSendFields: []string{"Red", "Green", "Blue"},
				}
				appliedParts = append(appliedParts, "background "+bgColor)
			}
			if txtColor != "" {
				r, g, b, ok := parseHexColorSheets(txtColor)
				if !ok {
					return nil, nil, toolErr("invalid text_color: %s", txtColor)
				}
				format.TextFormat = &sheets.TextFormat{
					ForegroundColor: &sheets.Color{
						Red: r, Green: g, Blue: b,
						ForceSendFields: []string{"Red", "Green", "Blue"},
					},
				}
				appliedParts = append(appliedParts, "text "+txtColor)
			}

			newRule = &sheets.ConditionalFormatRule{
				Ranges: []*sheets.GridRange{sheetsRange},
				BooleanRule: &sheets.BooleanRule{
					Condition: condition,
					Format:    format,
				},
			}
			ruleDesc = condTypeNorm
		}

		addReq := &sheets.AddConditionalFormatRuleRequest{Rule: newRule}
		if ruleIndex >= 0 {
			addReq.Index = int64(ruleIndex)
			addReq.ForceSendFields = []string{"Index"}
		}

		_, err = svc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{AddConditionalFormatRule: addReq},
			},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("adding conditional format: %v", err)
		}

		formatDesc := "format applied"
		if len(appliedParts) > 0 {
			formatDesc = strings.Join(appliedParts, ", ")
		}

		// Build simulated new rules state for display
		newRulesState := make([]*sheets.ConditionalFormatRule, 0, len(currentRules)+1)
		newRulesState = append(newRulesState, currentRules[:insertAt]...)
		newRulesState = append(newRulesState, newRule)
		if insertAt < len(currentRules) {
			newRulesState = append(newRulesState, currentRules[insertAt:]...)
		}

		stateText := formatConditionalRulesSection(targetInfo.Title, newRulesState, titles, "")

		return textResult(
			fmt.Sprintf("Added conditional format on '%s' in spreadsheet %s for %s: %s%s; format: %s.\n%s",
				rangeName, spreadsheetID, email, ruleDesc, valuesDesc, formatDesc, stateText))
	}
}

// --- update_conditional_formatting ---

func registerUpdateConditionalFormatting(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "update_conditional_formatting",
		Description: "Updates an existing conditional formatting rule by index on a sheet.",
	}
	mcp.AddTool(s, tool, handleUpdateConditionalFormatting(getClient))
}

// updateConditionalFormattingIn is the input schema for update_conditional_formatting.
type updateConditionalFormattingIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID   string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	RuleIndex       int    `json:"rule_index" jsonschema:"Index of the rule to update (0-based)"`
	RangeName       string `json:"range_name,omitempty" jsonschema:"A1-style range"`
	ConditionType   string `json:"condition_type,omitempty" jsonschema:"Sheets condition type"`
	ConditionValues any    `json:"condition_values,omitempty" jsonschema:"Values for the condition"`
	BackgroundColor string `json:"background_color,omitempty" jsonschema:"Hex background color"`
	TextColor       string `json:"text_color,omitempty" jsonschema:"Hex text color"`
	SheetName       string `json:"sheet_name,omitempty" jsonschema:"Sheet name when range_name is omitted"`
	GradientPoints  any    `json:"gradient_points,omitempty" jsonschema:"Gradient points as JSON array"`
}

func handleUpdateConditionalFormatting(getClient httpClientFunc) mcp.ToolHandlerFor[updateConditionalFormattingIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateConditionalFormattingIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		ruleIdx := in.RuleIndex
		if ruleIdx < 0 {
			return nil, nil, toolErr("rule_index must be a non-negative integer")
		}

		rangeName := in.RangeName
		condType := in.ConditionType
		condValuesRaw := in.ConditionValues
		bgColor := in.BackgroundColor
		txtColor := in.TextColor
		sheetName := in.SheetName
		gradientPointsRaw := in.GradientPoints

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		infos, titles, err := fetchSheetsMetadata(svc, spreadsheetID)
		if err != nil {
			return nil, nil, err
		}

		var targetInfo *sheetInfo
		var gridRange *sheetsGridRange
		if rangeName != "" {
			gridRange, err = parseA1Range(rangeName, infos)
			if err != nil {
				return nil, nil, err
			}
			for i := range infos {
				if infos[i].SheetID == gridRange.SheetID {
					targetInfo = &infos[i]
					break
				}
			}
		} else {
			targetInfo, err = selectSheet(infos, sheetName)
			if err != nil {
				return nil, nil, err
			}
		}

		if targetInfo == nil {
			return nil, nil, toolErr("target sheet not found")
		}

		rules := targetInfo.ConditionalRules
		if ruleIdx >= len(rules) {
			return nil, nil, toolErr(
				"rule_index %d is out of range for sheet '%s' (current count: %d)",
				ruleIdx, targetInfo.Title, len(rules))
		}

		existingRule := rules[ruleIdx]

		// Determine ranges to use
		rangesToUse := existingRule.Ranges
		if rangeName != "" {
			rangesToUse = []*sheets.GridRange{gridRange.toSheetsGridRange()}
		}
		if len(rangesToUse) == 0 {
			rangesToUse = []*sheets.GridRange{{SheetId: targetInfo.SheetID}}
		}

		gPoints, err := parseGradientPoints(gradientPointsRaw)
		if err != nil {
			return nil, nil, err
		}

		var newRule *sheets.ConditionalFormatRule
		var ruleDesc, valuesDesc, formatDesc string

		if len(gPoints) > 0 {
			// Update to gradient
			gradientRule := &sheets.GradientRule{}
			if len(gPoints) == 2 {
				gradientRule.Minpoint = gradientPointToInterpolation(gPoints[0])
				gradientRule.Maxpoint = gradientPointToInterpolation(gPoints[1])
			} else {
				gradientRule.Minpoint = gradientPointToInterpolation(gPoints[0])
				gradientRule.Midpoint = gradientPointToInterpolation(gPoints[1])
				gradientRule.Maxpoint = gradientPointToInterpolation(gPoints[2])
			}
			newRule = &sheets.ConditionalFormatRule{
				Ranges:       rangesToUse,
				GradientRule: gradientRule,
			}
			ruleDesc = "gradient"
			formatDesc = fmt.Sprintf("gradient points %d", len(gPoints))
		} else if existingRule.GradientRule != nil {
			// Existing gradient rule - keep it if no gradient_points provided
			if bgColor != "" || txtColor != "" || condType != "" || condValuesRaw != nil {
				return nil, nil, toolErr("existing rule is a gradient rule. Provide gradient_points to update it, or omit formatting/condition parameters to keep it unchanged")
			}
			newRule = &sheets.ConditionalFormatRule{
				Ranges:       rangesToUse,
				GradientRule: existingRule.GradientRule,
			}
			ruleDesc = "gradient"
			formatDesc = "gradient (unchanged)"
		} else {
			// Boolean rule
			existingBoolean := existingRule.BooleanRule
			if existingBoolean == nil {
				existingBoolean = &sheets.BooleanRule{}
			}
			existingCondition := existingBoolean.Condition
			if existingCondition == nil {
				existingCondition = &sheets.BooleanCondition{}
			}
			existingFormat := existingBoolean.Format
			if existingFormat == nil {
				existingFormat = &sheets.CellFormat{}
			}

			ct := condType
			if ct == "" {
				ct = existingCondition.Type
			}
			ct = strings.ToUpper(ct)
			if ct == "" {
				return nil, nil, toolErr("condition_type is required for boolean rules")
			}
			if !conditionTypes[ct] {
				return nil, nil, toolErr("condition_type must be one of: %v", sortedKeys(conditionTypes))
			}

			condValues, err := parseConditionValues(condValuesRaw)
			if err != nil {
				return nil, nil, err
			}

			condition := &sheets.BooleanCondition{Type: ct}
			if condValues != nil {
				for _, v := range condValues {
					condition.Values = append(condition.Values, &sheets.ConditionValue{
						UserEnteredValue: v,
					})
				}
				valuesDesc = fmt.Sprintf(" with values %v", condValues)
			} else if existingCondition.Values != nil {
				condition.Values = existingCondition.Values
			}

			// Build format
			newFormat := &sheets.CellFormat{}
			var fmtParts []string

			// Preserve or update background color
			if bgColor != "" {
				r, g, b, ok := parseHexColorSheets(bgColor)
				if ok {
					newFormat.BackgroundColor = &sheets.Color{
						Red: r, Green: g, Blue: b,
						ForceSendFields: []string{"Red", "Green", "Blue"},
					}
					fmtParts = append(fmtParts, "background updated")
				}
			} else if existingFormat.BackgroundColor != nil {
				newFormat.BackgroundColor = existingFormat.BackgroundColor
				fmtParts = append(fmtParts, "background preserved")
			}

			if txtColor != "" {
				r, g, b, ok := parseHexColorSheets(txtColor)
				if ok {
					newFormat.TextFormat = &sheets.TextFormat{
						ForegroundColor: &sheets.Color{
							Red: r, Green: g, Blue: b,
							ForceSendFields: []string{"Red", "Green", "Blue"},
						},
					}
					fmtParts = append(fmtParts, "text color updated")
				}
			} else if existingFormat.TextFormat != nil && existingFormat.TextFormat.ForegroundColor != nil {
				newFormat.TextFormat = &sheets.TextFormat{
					ForegroundColor: existingFormat.TextFormat.ForegroundColor,
				}
			}

			if newFormat.BackgroundColor == nil && newFormat.TextFormat == nil {
				return nil, nil, toolErr("at least one format option must remain on the rule")
			}

			formatDesc = strings.Join(fmtParts, ", ")
			if formatDesc == "" {
				formatDesc = "format preserved"
			}

			newRule = &sheets.ConditionalFormatRule{
				Ranges: rangesToUse,
				BooleanRule: &sheets.BooleanRule{
					Condition: condition,
					Format:    newFormat,
				},
			}
			ruleDesc = ct
		}

		_, err = svc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					UpdateConditionalFormatRule: &sheets.UpdateConditionalFormatRuleRequest{
						Index:           int64(ruleIdx),
						SheetId:         targetInfo.SheetID,
						Rule:            newRule,
						ForceSendFields: []string{"Index"},
					},
				},
			},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("updating conditional format: %v", err)
		}

		// Build new state
		newRulesState := make([]*sheets.ConditionalFormatRule, len(rules))
		copy(newRulesState, rules)
		newRulesState[ruleIdx] = newRule

		stateText := formatConditionalRulesSection(targetInfo.Title, newRulesState, titles, "")

		return textResult(
			fmt.Sprintf("Updated conditional format at index %d on sheet '%s' in spreadsheet %s for %s: %s%s; format: %s.\n%s",
				ruleIdx, targetInfo.Title, spreadsheetID, email, ruleDesc, valuesDesc, formatDesc, stateText))
	}
}

// --- delete_conditional_formatting ---

func registerDeleteConditionalFormatting(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_conditional_formatting",
		Description: "Deletes an existing conditional formatting rule by index on a sheet.",
	}
	mcp.AddTool(s, tool, handleDeleteConditionalFormatting(getClient))
}

// deleteConditionalFormattingIn is the input schema for delete_conditional_formatting.
type deleteConditionalFormattingIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID   string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	RuleIndex       int    `json:"rule_index" jsonschema:"Index of the rule to delete (0-based)"`
	SheetName       string `json:"sheet_name,omitempty" jsonschema:"Name of the sheet"`
}

func handleDeleteConditionalFormatting(getClient httpClientFunc) mcp.ToolHandlerFor[deleteConditionalFormattingIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteConditionalFormattingIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		ruleIdx := in.RuleIndex
		if ruleIdx < 0 {
			return nil, nil, toolErr("rule_index must be a non-negative integer")
		}
		sheetName := in.SheetName

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		infos, titles, err := fetchSheetsMetadata(svc, spreadsheetID)
		if err != nil {
			return nil, nil, err
		}

		targetInfo, err := selectSheet(infos, sheetName)
		if err != nil {
			return nil, nil, err
		}

		rules := targetInfo.ConditionalRules
		if ruleIdx >= len(rules) {
			return nil, nil, toolErr(
				"rule_index %d is out of range for sheet '%s' (current count: %d)",
				ruleIdx, targetInfo.Title, len(rules))
		}

		_, err = svc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					DeleteConditionalFormatRule: &sheets.DeleteConditionalFormatRuleRequest{
						Index:           int64(ruleIdx),
						SheetId:         targetInfo.SheetID,
						ForceSendFields: []string{"Index"},
					},
				},
			},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("deleting conditional format: %v", err)
		}

		// Build new state (remove the deleted rule)
		newRulesState := make([]*sheets.ConditionalFormatRule, 0, len(rules)-1)
		for i, r := range rules {
			if i != ruleIdx {
				newRulesState = append(newRulesState, r)
			}
		}

		stateText := formatConditionalRulesSection(targetInfo.Title, newRulesState, titles, "")

		return textResult(
			fmt.Sprintf("Deleted conditional format at index %d on sheet '%s' in spreadsheet %s for %s.\n%s",
				ruleIdx, targetInfo.Title, spreadsheetID, email, stateText))
	}
}

// --- create_spreadsheet ---

func registerCreateSpreadsheet(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_spreadsheet",
		Description: "Creates a new Google Spreadsheet.",
	}
	mcp.AddTool(s, tool, handleCreateSpreadsheet(getClient))
}

// createSpreadsheetIn is the input schema for create_spreadsheet.
type createSpreadsheetIn struct {
	UserGoogleEmail string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	Title           string   `json:"title" jsonschema:"Title of the new spreadsheet"`
	SheetNames      []string `json:"sheet_names,omitempty" jsonschema:"List of sheet names to create"`
}

func handleCreateSpreadsheet(getClient httpClientFunc) mcp.ToolHandlerFor[createSpreadsheetIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createSpreadsheetIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		title := in.Title

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		body := &sheets.Spreadsheet{
			Properties: &sheets.SpreadsheetProperties{Title: title},
		}

		// Parse sheet_names
		for _, name := range in.SheetNames {
			if name != "" {
				body.Sheets = append(body.Sheets, &sheets.Sheet{
					Properties: &sheets.SheetProperties{Title: name},
				})
			}
		}

		spreadsheet, err := svc.Spreadsheets.Create(body).
			Fields("spreadsheetId,spreadsheetUrl,properties(title,locale)").
			Do()
		if err != nil {
			return nil, nil, toolErr("creating spreadsheet: %v", err)
		}

		locale := spreadsheet.Properties.Locale
		if locale == "" {
			locale = "Unknown"
		}

		return textResult(
			fmt.Sprintf("Successfully created spreadsheet '%s' for %s. ID: %s | URL: %s | Locale: %s",
				title, email, spreadsheet.SpreadsheetId, spreadsheet.SpreadsheetUrl, locale))
	}
}

// --- create_sheet ---

func registerCreateSheet(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "create_sheet",
		Description: "Creates a new sheet within an existing spreadsheet.",
	}
	mcp.AddTool(s, tool, handleCreateSheet(getClient))
}

// createSheetIn is the input schema for create_sheet.
type createSheetIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address"`
	SpreadsheetID   string `json:"spreadsheet_id" jsonschema:"The ID of the spreadsheet"`
	SheetName       string `json:"sheet_name" jsonschema:"Name of the new sheet"`
}

func handleCreateSheet(getClient httpClientFunc) mcp.ToolHandlerFor[createSheetIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createSheetIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		spreadsheetID := in.SpreadsheetID
		sheetName := in.SheetName

		svc, err := newSheetsService(ctx, getClient, email)
		if err != nil {
			return nil, nil, toolErr("authentication failed: %v", err)
		}

		resp, err := svc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{
				{
					AddSheet: &sheets.AddSheetRequest{
						Properties: &sheets.SheetProperties{Title: sheetName},
					},
				},
			},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("creating sheet: %v", err)
		}

		sheetID := int64(0)
		if len(resp.Replies) > 0 && resp.Replies[0].AddSheet != nil {
			sheetID = resp.Replies[0].AddSheet.Properties.SheetId
		}

		return textResult(
			fmt.Sprintf("Successfully created sheet '%s' (ID: %d) in spreadsheet %s for %s.",
				sheetName, sheetID, spreadsheetID, email))
	}
}
