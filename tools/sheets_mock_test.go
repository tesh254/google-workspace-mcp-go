package tools

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// --- list_spreadsheets ---

func TestSheetsMockListSpreadsheets(t *testing.T) {
	t.Run("success_with_results", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/drive/v3/files": `{
				"files": [
					{"id":"ss001","name":"Budget 2026","modifiedTime":"2026-02-10T08:00:00Z","webViewLink":"https://docs.google.com/spreadsheets/d/ss001/edit"},
					{"id":"ss002","name":"Inventory","modifiedTime":"2026-02-05T14:00:00Z","webViewLink":"https://docs.google.com/spreadsheets/d/ss002/edit"}
				]
			}`,
		})
		handler := handleListSpreadsheets(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_spreadsheets", listSpreadsheetsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Successfully listed 2 spreadsheets") {
			t.Errorf("expected 2 spreadsheets listed, got:\n%s", text)
		}
		if !strings.Contains(text, "Budget 2026") {
			t.Errorf("expected 'Budget 2026' in output")
		}
		if !strings.Contains(text, "ss001") {
			t.Errorf("expected spreadsheet ID in output")
		}
	})

	t.Run("success_no_results", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/drive/v3/files": `{"files":[]}`,
		})
		handler := handleListSpreadsheets(testClientFunc(ts))
		text := callHandlerOK(t, handler, "list_spreadsheets", listSpreadsheetsIn{
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "No spreadsheets found") {
			t.Errorf("expected 'No spreadsheets found', got:\n%s", text)
		}
	})
}

// --- create_spreadsheet ---

func TestSheetsMockCreateSpreadsheet(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v4/spreadsheets": map[string]any{
				"spreadsheetId":  "newss001",
				"spreadsheetUrl": "https://docs.google.com/spreadsheets/d/newss001/edit",
				"properties": map[string]any{
					"title":  "New Budget",
					"locale": "en_US",
				},
			},
		})
		handler := handleCreateSpreadsheet(testClientFunc(ts))
		text := callHandlerOK(t, handler, "create_spreadsheet", createSpreadsheetIn{
			Title:           "New Budget",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Successfully created spreadsheet") {
			t.Errorf("expected success message, got:\n%s", text)
		}
		if !strings.Contains(text, "newss001") {
			t.Errorf("expected spreadsheet ID in output")
		}
		if !strings.Contains(text, "New Budget") {
			t.Errorf("expected title in output")
		}
	})
}

// --- get_spreadsheet_info ---

func TestSheetsMockGetSpreadsheetInfo(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v4/spreadsheets/ss001": map[string]any{
				"spreadsheetId": "ss001",
				"properties": map[string]any{
					"title":  "Sales Report",
					"locale": "en_US",
				},
				"sheets": []map[string]any{
					{
						"properties": map[string]any{
							"sheetId": 0,
							"title":   "Q1",
							"gridProperties": map[string]any{
								"rowCount":    100,
								"columnCount": 26,
							},
						},
					},
					{
						"properties": map[string]any{
							"sheetId": 1,
							"title":   "Q2",
							"gridProperties": map[string]any{
								"rowCount":    200,
								"columnCount": 10,
							},
						},
					},
				},
			},
		})
		handler := handleGetSpreadsheetInfo(testClientFunc(ts))
		text := callHandlerOK(t, handler, "get_spreadsheet_info", getSpreadsheetInfoIn{
			SpreadsheetID:   "ss001",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Sales Report") {
			t.Errorf("expected spreadsheet title in output, got:\n%s", text)
		}
		if !strings.Contains(text, "Q1") {
			t.Errorf("expected sheet name Q1 in output")
		}
		if !strings.Contains(text, "Q2") {
			t.Errorf("expected sheet name Q2 in output")
		}
		if !strings.Contains(text, "Sheets (2)") {
			t.Errorf("expected 'Sheets (2)' in output, got:\n%s", text)
		}
	})
}

// --- read_sheet_values ---

func TestSheetsMockReadSheetValues(t *testing.T) {
	t.Run("success_with_data", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v4/spreadsheets/ss001/values/": map[string]any{
				"range": "Sheet1!A1:C3",
				"values": [][]any{
					{"Name", "Age", "City"},
					{"Alice", 30, "NYC"},
					{"Bob", 25, "LA"},
				},
			},
		})
		handler := handleReadSheetValues(testClientFunc(ts))
		text := callHandlerOK(t, handler, "read_sheet_values", readSheetValuesIn{
			SpreadsheetID:   "ss001",
			RangeName:       "Sheet1!A1:C3",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Successfully read") {
			t.Errorf("expected success message, got:\n%s", text)
		}
		if !strings.Contains(text, "3 rows") {
			t.Errorf("expected '3 rows' in output, got:\n%s", text)
		}
	})

	t.Run("success_no_data", func(t *testing.T) {
		ts := fakeAPIServer(t, map[string]any{
			"/v4/spreadsheets/ss001/values/": map[string]any{
				"range": "Sheet1!A1:C3",
			},
		})
		handler := handleReadSheetValues(testClientFunc(ts))
		text := callHandlerOK(t, handler, "read_sheet_values", readSheetValuesIn{
			SpreadsheetID:   "ss001",
			RangeName:       "Sheet1!A1:C3",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "No data found") {
			t.Errorf("expected 'No data found', got:\n%s", text)
		}
	})
}

// --- modify_sheet_values ---

func TestSheetsMockModifySheetValues(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := driveFakeServer(t, map[string]any{
			"/v4/spreadsheets/ss001/values/": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"updatedRange":"Sheet1!A1:B2","updatedRows":2,"updatedColumns":2,"updatedCells":4}`)
			},
		})
		handler := handleModifySheetValues(testClientFunc(ts))
		text := callHandlerOK(t, handler, "modify_sheet_values", modifySheetValuesIn{
			SpreadsheetID:   "ss001",
			RangeName:       "Sheet1!A1:B2",
			Values:          []any{[]any{"a", "b"}, []any{"c", "d"}},
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "Updated") || !strings.Contains(text, "cells") {
			t.Errorf("expected update confirmation, got:\n%s", text)
		}
	})
}

// --- API error responses ---

func TestSheetsMockAPIError(t *testing.T) {
	t.Run("get_spreadsheet_info_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v4/spreadsheets/ss001": {code: 404, body: `{"error": {"code": 404, "message": "Not Found"}}`},
		})
		handler := handleGetSpreadsheetInfo(testClientFunc(ts))
		text := callHandlerErr(t, handler, "get_spreadsheet_info", getSpreadsheetInfoIn{
			SpreadsheetID:   "ss001",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "getting spreadsheet info") {
			t.Errorf("expected error about getting spreadsheet info, got:\n%s", text)
		}
	})

	t.Run("create_spreadsheet_error", func(t *testing.T) {
		ts := fakeAPIServerWithStatus(t, map[string]statusResponse{
			"/v4/spreadsheets": {code: 500, body: `{"error": {"code": 500, "message": "Internal Server Error"}}`},
		})
		handler := handleCreateSpreadsheet(testClientFunc(ts))
		text := callHandlerErr(t, handler, "create_spreadsheet", createSpreadsheetIn{
			Title:           "Bad Sheet",
			UserGoogleEmail: "test@example.com",
		})
		if !strings.Contains(text, "creating spreadsheet") {
			t.Errorf("expected error about creating spreadsheet, got:\n%s", text)
		}
	})
}
