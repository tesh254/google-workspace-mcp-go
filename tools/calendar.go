package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	calendar "google.golang.org/api/calendar/v3"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

// RegisterCalendarTools registers all Calendar tools with the MCP server.
func RegisterCalendarTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	// Read tools
	registerListCalendars(s, getClient)
	registerGetEvents(s, getClient)
	registerQueryFreebusy(s, getClient)

	// Write tools
	registerCreateEvent(s, getClient)
	registerModifyEvent(s, getClient)
	registerDeleteEvent(s, getClient)
}

// newCalendarService creates a calendar.Service for the given user email.
func newCalendarService(ctx context.Context, getClient httpClientFunc, email string) (*calendar.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := calendar.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating Calendar service: %w", err)
	}
	return svc, nil
}

// correctTimeFormatForAPI converts bare dates (YYYY-MM-DD) to RFC3339 and
// ensures timestamps end with a timezone designator.
func correctTimeFormatForAPI(t string) string {
	if t == "" {
		return ""
	}
	// Bare date: YYYY-MM-DD → append T00:00:00Z
	if len(t) == 10 && !strings.Contains(t, "T") {
		return t + "T00:00:00Z"
	}
	// Has time component but no timezone indicator
	if strings.Contains(t, "T") && !strings.Contains(t, "Z") && !strings.Contains(t, "+") && !strings.ContainsAny(t[len(t)-6:], "+-") {
		return t + "Z"
	}
	return t
}

// isAllDay returns true if the time string is a bare date (no "T" component).
func isAllDay(t string) bool {
	return !strings.Contains(t, "T")
}

// --- list_calendars ---

// listCalendarsIn is the input schema for list_calendars.
type listCalendarsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
}

func registerListCalendars(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "list_calendars",
		Description: "Retrieves a list of calendars accessible to the authenticated user.",
	}
	mcp.AddTool(s, tool, handleListCalendars(getClient))
}

func handleListCalendars(getClient httpClientFunc) mcp.ToolHandlerFor[listCalendarsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listCalendarsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newCalendarService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resp, err := svc.CalendarList.List().Do()
		if err != nil {
			return nil, nil, toolErr("listing calendars: %v", err)
		}

		items := resp.Items
		if len(items) == 0 {
			return textResult(fmt.Sprintf("No calendars found for %s.", email))
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Successfully listed %d calendars for %s:", len(items), email)
		for _, cal := range items {
			summary := cal.Summary
			if summary == "" {
				summary = "No Summary"
			}
			primary := ""
			if cal.Primary {
				primary = " (Primary)"
			}
			fmt.Fprintf(&b, "\n- \"%s\"%s (ID: %s)", summary, primary, cal.Id)
		}
		return textResult(b.String())
	}
}

// --- get_events ---

// getEventsIn is the input schema for get_events.
type getEventsIn struct {
	UserGoogleEmail    string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	CalendarID         string "json:\"calendar_id,omitempty\" jsonschema:\"The ID of the calendar to query. Use 'primary' for the user's primary calendar. Defaults to 'primary'. Calendar IDs can be obtained using `list_calendars`.\""
	EventID            string `json:"event_id,omitempty" jsonschema:"The ID of a specific event to retrieve. If provided, retrieves only this event and ignores time filtering parameters."`
	TimeMin            string `json:"time_min,omitempty" jsonschema:"The start of the time range (inclusive) in RFC3339 format (e.g., '2024-05-12T10:00:00Z' or '2024-05-12'). If omitted, defaults to the current time. Ignored if event_id is provided."`
	TimeMax            string "json:\"time_max,omitempty\" jsonschema:\"The end of the time range (exclusive) in RFC3339 format. If omitted, events starting from `time_min` onwards are considered (up to `max_results`). Ignored if event_id is provided.\""
	MaxResults         int    `json:"max_results,omitempty" jsonschema:"The maximum number of events to return. Defaults to 25. Ignored if event_id is provided."`
	Query              string `json:"query,omitempty" jsonschema:"A keyword to search for within event fields (summary, description, location). Ignored if event_id is provided."`
	Detailed           bool   `json:"detailed,omitempty" jsonschema:"Whether to return detailed event information including description, location, attendees, and attendee details (response status, organizer, optional flags). Defaults to False."`
	IncludeAttachments bool   `json:"include_attachments,omitempty" jsonschema:"Whether to include attachment information in detailed event output. When True, shows attachment details (fileId, fileUrl, mimeType, title) for events that have attachments. Only applies when detailed=True. Defaults to False."`
}

func registerGetEvents(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "get_events",
		Description: "Retrieves events from a specified Google Calendar. Can retrieve a single event by ID or multiple events within a time range. You can also search for events by keyword by supplying the optional \"query\" param.",
	}
	mcp.AddTool(s, tool, handleGetEvents(getClient))
}

func handleGetEvents(getClient httpClientFunc) mcp.ToolHandlerFor[getEventsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getEventsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		calendarID := in.CalendarID
		if calendarID == "" {
			calendarID = "primary"
		}
		eventID := in.EventID
		timeMin := in.TimeMin
		timeMax := in.TimeMax
		maxResults := in.MaxResults
		if maxResults == 0 {
			maxResults = 25
		}
		query := in.Query
		detailed := in.Detailed
		includeAttachments := in.IncludeAttachments

		svc, err := newCalendarService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		var items []*calendar.Event

		if eventID != "" {
			// Single event retrieval
			event, err := svc.Events.Get(calendarID, eventID).Do()
			if err != nil {
				return nil, nil, toolErr("getting event %s: %v", eventID, err)
			}
			items = []*calendar.Event{event}
		} else {
			// Multiple events retrieval with time filtering
			effectiveTimeMin := correctTimeFormatForAPI(timeMin)
			if effectiveTimeMin == "" {
				effectiveTimeMin = time.Now().UTC().Format(time.RFC3339)
			}
			effectiveTimeMax := correctTimeFormatForAPI(timeMax)

			call := svc.Events.List(calendarID).
				TimeMin(effectiveTimeMin).
				MaxResults(int64(maxResults)).
				SingleEvents(true).
				OrderBy("startTime")

			if effectiveTimeMax != "" {
				call = call.TimeMax(effectiveTimeMax)
			}
			if query != "" {
				call = call.Q(query)
			}

			resp, err := call.Do()
			if err != nil {
				return nil, nil, toolErr("listing events: %v", err)
			}
			items = resp.Items
		}

		if len(items) == 0 {
			if eventID != "" {
				return textResult(fmt.Sprintf("Event with ID '%s' not found in calendar '%s' for %s.", eventID, calendarID, email))
			}
			return textResult(fmt.Sprintf("No events found in calendar '%s' for %s for the specified time range.", calendarID, email))
		}

		// Single event with detailed output
		if eventID != "" && detailed {
			item := items[0]
			return textResult(formatDetailedSingleEvent(item, eventID, includeAttachments))
		}

		// Multiple events or single event with basic output
		var b strings.Builder
		if eventID != "" {
			fmt.Fprintf(&b, "Successfully retrieved event from calendar '%s' for %s:", calendarID, email)
		} else {
			fmt.Fprintf(&b, "Successfully retrieved %d events from calendar '%s' for %s:", len(items), calendarID, email)
		}

		for _, item := range items {
			summary := item.Summary
			if summary == "" {
				summary = "No Title"
			}
			startTime := eventTime(item.Start)
			endTime := eventTime(item.End)
			link := item.HtmlLink
			if link == "" {
				link = "No Link"
			}
			itemEventID := item.Id
			if itemEventID == "" {
				itemEventID = "No ID"
			}

			if detailed {
				description := item.Description
				if description == "" {
					description = "No Description"
				}
				location := item.Location
				if location == "" {
					location = "No Location"
				}
				attendeeEmails := formatAttendeeEmails(item.Attendees)
				attendeeDetails := formatAttendeeDetails(item.Attendees, "    ")

				fmt.Fprintf(&b, "\n- \"%s\" (Starts: %s, Ends: %s)\n  Description: %s\n  Location: %s\n  Attendees: %s\n  Attendee Details: %s",
					summary, startTime, endTime, description, location, attendeeEmails, attendeeDetails)

				if includeAttachments {
					attachmentDetails := formatEventAttachmentDetails(item.Attachments, "    ")
					fmt.Fprintf(&b, "\n  Attachments: %s", attachmentDetails)
				}

				fmt.Fprintf(&b, "\n  ID: %s | Link: %s", itemEventID, link)
			} else {
				fmt.Fprintf(&b, "\n- \"%s\" (Starts: %s, Ends: %s) ID: %s | Link: %s",
					summary, startTime, endTime, itemEventID, link)
			}
		}

		return textResult(b.String())
	}
}

// --- create_event ---

// createEventIn is the input schema for create_event.
type createEventIn struct {
	UserGoogleEmail         string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	Summary                 string   `json:"summary" jsonschema:"Event title."`
	StartTime               string   `json:"start_time" jsonschema:"Start time (RFC3339, e.g., \"2023-10-27T10:00:00-07:00\" or \"2023-10-27\" for all-day)."`
	EndTime                 string   `json:"end_time" jsonschema:"End time (RFC3339, e.g., \"2023-10-27T11:00:00-07:00\" or \"2023-10-28\" for all-day)."`
	CalendarID              string   `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default: 'primary')."`
	Description             string   `json:"description,omitempty" jsonschema:"Event description."`
	Location                string   `json:"location,omitempty" jsonschema:"Event location."`
	Attendees               []string `json:"attendees,omitempty" jsonschema:"Attendee email addresses."`
	Timezone                string   `json:"timezone,omitempty" jsonschema:"Timezone (e.g., \"America/New_York\")."`
	Attachments             []string `json:"attachments,omitempty" jsonschema:"List of Google Drive file URLs or IDs to attach to the event."`
	AddGoogleMeet           bool     `json:"add_google_meet,omitempty" jsonschema:"Whether to add a Google Meet video conference to the event. Defaults to False."`
	Reminders               string   `json:"reminders,omitempty" jsonschema:"JSON string of reminder objects. Each should have 'method' (\"popup\" or \"email\") and 'minutes' (0-40320). Max 5 reminders. Example: '[{\"method\": \"popup\", \"minutes\": 15}]'"`
	UseDefaultReminders     *bool    `json:"use_default_reminders,omitempty" jsonschema:"Whether to use calendar's default reminders. If False, uses custom reminders. Defaults to True."`
	Transparency            string   `json:"transparency,omitempty" jsonschema:"Event transparency for busy/free status. \"opaque\" shows as Busy (default), \"transparent\" shows as Available/Free."`
	Visibility              string   `json:"visibility,omitempty" jsonschema:"Event visibility. \"default\" uses calendar default, \"public\" is visible to all, \"private\" is visible only to attendees, \"confidential\" is same as private (legacy)."`
	GuestsCanModify         *bool    `json:"guests_can_modify,omitempty" jsonschema:"Whether attendees other than the organizer can modify the event."`
	GuestsCanInviteOthers   *bool    `json:"guests_can_invite_others,omitempty" jsonschema:"Whether attendees other than the organizer can invite others to the event."`
	GuestsCanSeeOtherGuests *bool    `json:"guests_can_see_other_guests,omitempty" jsonschema:"Whether attendees other than the organizer can see who the event's attendees are."`
}

func registerCreateEvent(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[createEventIn](&mcp.Tool{
		Name:        "create_event",
		Description: "Creates a new event.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "transparency", "opaque", "transparent")
		setEnum(sch, "visibility", "default", "public", "private", "confidential")
	})
	mcp.AddTool(s, tool, handleCreateEvent(getClient))
}

func handleCreateEvent(getClient httpClientFunc) mcp.ToolHandlerFor[createEventIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createEventIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		summary := in.Summary
		startTime := in.StartTime
		endTime := in.EndTime

		calendarID := in.CalendarID
		if calendarID == "" {
			calendarID = "primary"
		}
		description := in.Description
		location := in.Location
		attendees := in.Attendees
		timezone := in.Timezone
		attachmentIDs := in.Attachments
		addGoogleMeet := in.AddGoogleMeet
		remindersStr := in.Reminders
		useDefaultReminders := true
		if in.UseDefaultReminders != nil {
			useDefaultReminders = *in.UseDefaultReminders
		}
		transparency := in.Transparency
		visibility := in.Visibility

		svc, err := newCalendarService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Build event body
		eventBody := &calendar.Event{
			Summary: summary,
		}

		// Set start/end times
		if isAllDay(startTime) {
			eventBody.Start = &calendar.EventDateTime{Date: startTime}
		} else {
			eventBody.Start = &calendar.EventDateTime{DateTime: startTime}
		}
		if isAllDay(endTime) {
			eventBody.End = &calendar.EventDateTime{Date: endTime}
		} else {
			eventBody.End = &calendar.EventDateTime{DateTime: endTime}
		}

		// Apply timezone if set
		if timezone != "" {
			if eventBody.Start.DateTime != "" {
				eventBody.Start.TimeZone = timezone
			}
			if eventBody.End.DateTime != "" {
				eventBody.End.TimeZone = timezone
			}
		}

		if description != "" {
			eventBody.Description = description
		}
		if location != "" {
			eventBody.Location = location
		}
		if len(attendees) > 0 {
			for _, a := range attendees {
				eventBody.Attendees = append(eventBody.Attendees, &calendar.EventAttendee{Email: a})
			}
		}

		// Handle reminders
		if remindersStr != "" || !useDefaultReminders {
			effectiveUseDefault := useDefaultReminders && remindersStr == ""
			reminderData := &calendar.EventReminders{
				UseDefault:      effectiveUseDefault,
				ForceSendFields: []string{"UseDefault"},
			}
			if remindersStr != "" {
				overrides, errMsg := parseRemindersJSON(remindersStr)
				if errMsg != "" {
					return nil, nil, toolErr("%s", errMsg)
				}
				reminderData.Overrides = overrides
				reminderData.UseDefault = false
			}
			eventBody.Reminders = reminderData
		}

		// Handle transparency
		if transparency != "" {
			eventBody.Transparency = transparency
		}

		// Handle visibility
		if visibility != "" {
			eventBody.Visibility = visibility
		}

		// Handle guest permissions
		if in.GuestsCanModify != nil {
			eventBody.GuestsCanModify = *in.GuestsCanModify
			eventBody.ForceSendFields = append(eventBody.ForceSendFields, "GuestsCanModify")
		}
		if in.GuestsCanInviteOthers != nil {
			v := *in.GuestsCanInviteOthers
			eventBody.GuestsCanInviteOthers = &v
		}
		if in.GuestsCanSeeOtherGuests != nil {
			v := *in.GuestsCanSeeOtherGuests
			eventBody.GuestsCanSeeOtherGuests = &v
		}

		// Handle Google Meet
		conferenceDataVersion := int64(0)
		if addGoogleMeet {
			eventBody.ConferenceData = &calendar.ConferenceData{
				CreateRequest: &calendar.CreateConferenceRequest{
					RequestId: fmt.Sprintf("meet-%d", time.Now().UnixNano()),
					ConferenceSolutionKey: &calendar.ConferenceSolutionKey{
						Type: "hangoutsMeet",
					},
				},
			}
			conferenceDataVersion = 1
		}

		// Handle attachments
		if len(attachmentIDs) > 0 {
			for _, att := range attachmentIDs {
				fileID := extractDriveFileID(att)
				if fileID != "" {
					eventBody.Attachments = append(eventBody.Attachments, &calendar.EventAttachment{
						FileUrl: fmt.Sprintf("https://drive.google.com/open?id=%s", fileID),
						Title:   "Drive Attachment",
					})
				}
			}
		}

		call := svc.Events.Insert(calendarID, eventBody).
			ConferenceDataVersion(conferenceDataVersion)
		if len(attachmentIDs) > 0 {
			call = call.SupportsAttachments(true)
		}

		created, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("creating event: %v", err)
		}

		link := created.HtmlLink
		if link == "" {
			link = "No link available"
		}
		msg := fmt.Sprintf("Successfully created event '%s' for %s. Link: %s", created.Summary, email, link)

		// Add Meet link if created
		if addGoogleMeet && created.ConferenceData != nil {
			for _, ep := range created.ConferenceData.EntryPoints {
				if ep.EntryPointType == "video" && ep.Uri != "" {
					msg += fmt.Sprintf(" Google Meet: %s", ep.Uri)
					break
				}
			}
		}

		return textResult(msg)
	}
}

// --- modify_event ---

// modifyEventIn is the input schema for modify_event.
type modifyEventIn struct {
	UserGoogleEmail         string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	EventID                 string   `json:"event_id" jsonschema:"The ID of the event to modify. Required."`
	CalendarID              string   `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default: 'primary')."`
	Summary                 string   `json:"summary,omitempty" jsonschema:"New event title."`
	StartTime               string   `json:"start_time,omitempty" jsonschema:"New start time (RFC3339, e.g., \"2023-10-27T10:00:00-07:00\" or \"2023-10-27\" for all-day)."`
	EndTime                 string   `json:"end_time,omitempty" jsonschema:"New end time (RFC3339, e.g., \"2023-10-27T11:00:00-07:00\" or \"2023-10-28\" for all-day)."`
	Description             string   `json:"description,omitempty" jsonschema:"New event description."`
	Location                string   `json:"location,omitempty" jsonschema:"New event location."`
	Attendees               []string `json:"attendees,omitempty" jsonschema:"Attendees as email strings. Supports: [\"email@example.com\"]. New attendees default to responseStatus=\"needsAction\"."`
	Timezone                string   `json:"timezone,omitempty" jsonschema:"New timezone (e.g., \"America/New_York\")."`
	AddGoogleMeet           *bool    `json:"add_google_meet,omitempty" jsonschema:"Whether to add or remove Google Meet video conference. If True, adds Google Meet; if False, removes it; if not provided, leaves unchanged."`
	Reminders               string   `json:"reminders,omitempty" jsonschema:"JSON string of reminder objects to replace existing reminders. Each should have 'method' (\"popup\" or \"email\") and 'minutes' (0-40320). Max 5 reminders."`
	UseDefaultReminders     *bool    `json:"use_default_reminders,omitempty" jsonschema:"Whether to use calendar's default reminders. If specified, overrides current reminder settings."`
	Transparency            string   `json:"transparency,omitempty" jsonschema:"Event transparency for busy/free status. \"opaque\" shows as Busy, \"transparent\" shows as Available/Free."`
	Visibility              string   `json:"visibility,omitempty" jsonschema:"Event visibility. \"default\" uses calendar default, \"public\" is visible to all, \"private\" is visible only to attendees, \"confidential\" is same as private (legacy)."`
	ColorID                 string   `json:"color_id,omitempty" jsonschema:"Event color ID (1-11)."`
	GuestsCanModify         *bool    `json:"guests_can_modify,omitempty" jsonschema:"Whether attendees other than the organizer can modify the event."`
	GuestsCanInviteOthers   *bool    `json:"guests_can_invite_others,omitempty" jsonschema:"Whether attendees other than the organizer can invite others to the event."`
	GuestsCanSeeOtherGuests *bool    `json:"guests_can_see_other_guests,omitempty" jsonschema:"Whether attendees other than the organizer can see who the event's attendees are."`
}

func registerModifyEvent(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[modifyEventIn](&mcp.Tool{
		Name:        "modify_event",
		Description: "Modifies an existing event.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "transparency", "opaque", "transparent")
		setEnum(sch, "visibility", "default", "public", "private", "confidential")
	})
	mcp.AddTool(s, tool, handleModifyEvent(getClient))
}

func handleModifyEvent(getClient httpClientFunc) mcp.ToolHandlerFor[modifyEventIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in modifyEventIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		eventID := in.EventID

		calendarID := in.CalendarID
		if calendarID == "" {
			calendarID = "primary"
		}

		svc, err := newCalendarService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Fetch existing event to preserve fields
		existing, err := svc.Events.Get(calendarID, eventID).Do()
		if err != nil {
			return nil, nil, toolErr("event not found: %v", err)
		}

		// Start with existing event and modify provided fields
		eventBody := existing

		if in.Summary != "" {
			eventBody.Summary = in.Summary
		}
		if in.Description != "" {
			eventBody.Description = in.Description
		}
		if in.Location != "" {
			eventBody.Location = in.Location
		}

		timezone := in.Timezone

		if in.StartTime != "" {
			s := in.StartTime
			if isAllDay(s) {
				eventBody.Start = &calendar.EventDateTime{Date: s}
			} else {
				eventBody.Start = &calendar.EventDateTime{DateTime: s}
				if timezone != "" {
					eventBody.Start.TimeZone = timezone
				}
			}
		}
		if in.EndTime != "" {
			s := in.EndTime
			if isAllDay(s) {
				eventBody.End = &calendar.EventDateTime{Date: s}
			} else {
				eventBody.End = &calendar.EventDateTime{DateTime: s}
				if timezone != "" {
					eventBody.End.TimeZone = timezone
				}
			}
		}

		// Handle attendees
		attendeeList := in.Attendees
		if attendeeList != nil {
			eventBody.Attendees = nil
			for _, a := range attendeeList {
				eventBody.Attendees = append(eventBody.Attendees, &calendar.EventAttendee{Email: a})
			}
		}

		// Handle color_id
		if in.ColorID != "" {
			eventBody.ColorId = in.ColorID
		}

		// Handle reminders
		remindersStr := in.Reminders
		useDefaultPresent := in.UseDefaultReminders != nil
		if remindersStr != "" || useDefaultPresent {
			reminderData := &calendar.EventReminders{
				ForceSendFields: []string{"UseDefault"},
			}
			if useDefaultPresent {
				reminderData.UseDefault = *in.UseDefaultReminders
			} else if existing.Reminders != nil {
				reminderData.UseDefault = existing.Reminders.UseDefault
			} else {
				reminderData.UseDefault = true
			}
			if remindersStr != "" {
				overrides, errMsg := parseRemindersJSON(remindersStr)
				if errMsg != "" {
					return nil, nil, toolErr("%s", errMsg)
				}
				reminderData.Overrides = overrides
				reminderData.UseDefault = false
			}
			eventBody.Reminders = reminderData
		}

		// Handle transparency
		if in.Transparency != "" {
			eventBody.Transparency = in.Transparency
		}

		// Handle visibility
		if in.Visibility != "" {
			eventBody.Visibility = in.Visibility
		}

		// Handle guest permissions
		if in.GuestsCanModify != nil {
			eventBody.GuestsCanModify = *in.GuestsCanModify
			eventBody.ForceSendFields = append(eventBody.ForceSendFields, "GuestsCanModify")
		}
		if in.GuestsCanInviteOthers != nil {
			v := *in.GuestsCanInviteOthers
			eventBody.GuestsCanInviteOthers = &v
		}
		if in.GuestsCanSeeOtherGuests != nil {
			v := *in.GuestsCanSeeOtherGuests
			eventBody.GuestsCanSeeOtherGuests = &v
		}

		// Handle Google Meet
		if in.AddGoogleMeet != nil {
			addMeet := *in.AddGoogleMeet
			if addMeet {
				eventBody.ConferenceData = &calendar.ConferenceData{
					CreateRequest: &calendar.CreateConferenceRequest{
						RequestId: fmt.Sprintf("meet-%d", time.Now().UnixNano()),
						ConferenceSolutionKey: &calendar.ConferenceSolutionKey{
							Type: "hangoutsMeet",
						},
					},
				}
			} else {
				// Remove Google Meet
				eventBody.ConferenceData = &calendar.ConferenceData{}
				eventBody.ForceSendFields = append(eventBody.ForceSendFields, "ConferenceData")
			}
		}

		updated, err := svc.Events.Update(calendarID, eventID, eventBody).
			ConferenceDataVersion(1).Do()
		if err != nil {
			return nil, nil, toolErr("modifying event: %v", err)
		}

		link := updated.HtmlLink
		if link == "" {
			link = "No link available"
		}
		msg := fmt.Sprintf("Successfully modified event '%s' (ID: %s) for %s. Link: %s",
			updated.Summary, eventID, email, link)

		// Add Meet link info
		if in.AddGoogleMeet != nil {
			addMeet := *in.AddGoogleMeet
			if addMeet && updated.ConferenceData != nil {
				for _, ep := range updated.ConferenceData.EntryPoints {
					if ep.EntryPointType == "video" && ep.Uri != "" {
						msg += fmt.Sprintf(" Google Meet: %s", ep.Uri)
						break
					}
				}
			} else if !addMeet {
				msg += " (Google Meet removed)"
			}
		}

		return textResult(msg)
	}
}

// --- delete_event ---

// deleteEventIn is the input schema for delete_event.
type deleteEventIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	EventID         string `json:"event_id" jsonschema:"The ID of the event to delete. Required."`
	CalendarID      string `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default: 'primary')."`
}

func registerDeleteEvent(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "delete_event",
		Description: "Deletes an existing event.",
	}
	mcp.AddTool(s, tool, handleDeleteEvent(getClient))
}

func handleDeleteEvent(getClient httpClientFunc) mcp.ToolHandlerFor[deleteEventIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteEventIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		eventID := in.EventID

		calendarID := in.CalendarID
		if calendarID == "" {
			calendarID = "primary"
		}

		svc, err := newCalendarService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Verify event exists first
		_, err = svc.Events.Get(calendarID, eventID).Do()
		if err != nil {
			return nil, nil, toolErr("Event not found. The event with ID '%s' could not be found in calendar '%s'. This may be due to incorrect ID format or the event no longer exists.", eventID, calendarID)
		}

		err = svc.Events.Delete(calendarID, eventID).Do()
		if err != nil {
			return nil, nil, toolErr("deleting event: %v", err)
		}

		return textResult(fmt.Sprintf("Successfully deleted event (ID: %s) from calendar '%s' for %s.", eventID, calendarID, email))
	}
}

// --- query_freebusy ---

// queryFreebusyIn is the input schema for query_freebusy.
type queryFreebusyIn struct {
	UserGoogleEmail      string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address. Required."`
	TimeMin              string   `json:"time_min" jsonschema:"The start of the interval for the query in RFC3339 format (e.g., '2024-05-12T10:00:00Z' or '2024-05-12')."`
	TimeMax              string   `json:"time_max" jsonschema:"The end of the interval for the query in RFC3339 format (e.g., '2024-05-12T18:00:00Z' or '2024-05-12')."`
	CalendarIDs          []string `json:"calendar_ids,omitempty" jsonschema:"List of calendar identifiers to query. If not provided, queries the primary calendar."`
	GroupExpansionMax    int      `json:"group_expansion_max,omitempty" jsonschema:"Maximum number of calendar identifiers to be provided for a single group. Maximum value is 100."`
	CalendarExpansionMax int      `json:"calendar_expansion_max,omitempty" jsonschema:"Maximum number of calendars for which FreeBusy information is to be provided. Maximum value is 50."`
}

func registerQueryFreebusy(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{
		Name:        "query_freebusy",
		Description: "Returns free/busy information for a set of calendars.",
	}
	mcp.AddTool(s, tool, handleQueryFreebusy(getClient))
}

func handleQueryFreebusy(getClient httpClientFunc) mcp.ToolHandlerFor[queryFreebusyIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in queryFreebusyIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		timeMin := in.TimeMin
		timeMax := in.TimeMax

		calendarIDs := in.CalendarIDs
		if len(calendarIDs) == 0 {
			calendarIDs = []string{"primary"}
		}
		groupExpansionMax := in.GroupExpansionMax
		calendarExpansionMax := in.CalendarExpansionMax

		svc, err := newCalendarService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Format time parameters
		formattedTimeMin := correctTimeFormatForAPI(timeMin)
		formattedTimeMax := correctTimeFormatForAPI(timeMax)

		// Build request body
		items := make([]*calendar.FreeBusyRequestItem, 0, len(calendarIDs))
		for _, id := range calendarIDs {
			items = append(items, &calendar.FreeBusyRequestItem{Id: id})
		}

		reqBody := &calendar.FreeBusyRequest{
			TimeMin: formattedTimeMin,
			TimeMax: formattedTimeMax,
			Items:   items,
		}
		if groupExpansionMax > 0 {
			reqBody.GroupExpansionMax = int64(groupExpansionMax)
		}
		if calendarExpansionMax > 0 {
			reqBody.CalendarExpansionMax = int64(calendarExpansionMax)
		}

		resp, err := svc.Freebusy.Query(reqBody).Do()
		if err != nil {
			return nil, nil, toolErr("querying free/busy: %v", err)
		}

		calendars := resp.Calendars
		timeMinResult := resp.TimeMin
		if timeMinResult == "" {
			timeMinResult = formattedTimeMin
		}
		timeMaxResult := resp.TimeMax
		if timeMaxResult == "" {
			timeMaxResult = formattedTimeMax
		}

		if len(calendars) == 0 {
			return textResult(fmt.Sprintf("No free/busy information found for the requested calendars for %s.", email))
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Free/Busy information for %s:\nTime range: %s to %s\n", email, timeMinResult, timeMaxResult)

		for calID, calData := range calendars {
			fmt.Fprintf(&b, "\nCalendar: %s", calID)

			if len(calData.Errors) > 0 {
				b.WriteString("\n  Errors:")
				for _, e := range calData.Errors {
					fmt.Fprintf(&b, "\n    - %s: %s", e.Domain, e.Reason)
				}
				b.WriteString("\n")
				continue
			}

			if len(calData.Busy) == 0 {
				b.WriteString("\n  Status: Free (no busy periods)")
			} else {
				fmt.Fprintf(&b, "\n  Busy periods: %d", len(calData.Busy))
				for _, period := range calData.Busy {
					fmt.Fprintf(&b, "\n    - %s to %s", period.Start, period.End)
				}
			}
			b.WriteString("\n")
		}

		return textResult(b.String())
	}
}

// --- Helper functions ---

// eventTime extracts the display time from an EventDateTime.
func eventTime(edt *calendar.EventDateTime) string {
	if edt == nil {
		return "Unknown"
	}
	if edt.DateTime != "" {
		return edt.DateTime
	}
	if edt.Date != "" {
		return edt.Date
	}
	return "Unknown"
}

// formatAttendeeEmails formats attendee emails as a comma-separated string.
func formatAttendeeEmails(attendees []*calendar.EventAttendee) string {
	if len(attendees) == 0 {
		return "None"
	}
	emails := make([]string, 0, len(attendees))
	for _, a := range attendees {
		emails = append(emails, a.Email)
	}
	return strings.Join(emails, ", ")
}

// formatAttendeeDetails formats attendee details with response status and flags.
func formatAttendeeDetails(attendees []*calendar.EventAttendee, indent string) string {
	if len(attendees) == 0 {
		return "None"
	}
	var b strings.Builder
	for i, a := range attendees {
		if i > 0 {
			b.WriteString(", ")
		}
		status := a.ResponseStatus
		if status == "" {
			status = "needsAction"
		}
		detail := fmt.Sprintf("%s (%s)", a.Email, status)
		if a.Organizer {
			detail += " [organizer]"
		}
		if a.Optional {
			detail += " [optional]"
		}
		b.WriteString(detail)
	}
	return b.String()
}

// formatEventAttachmentDetails formats attachment details for calendar events.
func formatEventAttachmentDetails(attachments []*calendar.EventAttachment, indent string) string {
	if len(attachments) == 0 {
		return "None"
	}
	var b strings.Builder
	for i, att := range attachments {
		if i > 0 {
			b.WriteString(", ")
		}
		title := att.Title
		if title == "" {
			title = "Untitled"
		}
		fmt.Fprintf(&b, "%s (URL: %s, MIME: %s, FileID: %s)", title, att.FileUrl, att.MimeType, att.FileId)
	}
	return b.String()
}

// formatDetailedSingleEvent formats a single event with full details.
func formatDetailedSingleEvent(item *calendar.Event, eventID string, includeAttachments bool) string {
	summary := item.Summary
	if summary == "" {
		summary = "No Title"
	}
	startTime := eventTime(item.Start)
	endTime := eventTime(item.End)
	link := item.HtmlLink
	if link == "" {
		link = "No Link"
	}
	description := item.Description
	if description == "" {
		description = "No Description"
	}
	location := item.Location
	if location == "" {
		location = "No Location"
	}
	colorID := item.ColorId
	if colorID == "" {
		colorID = "None"
	}
	attendeeEmails := formatAttendeeEmails(item.Attendees)
	attendeeDetails := formatAttendeeDetails(item.Attendees, "  ")

	var b strings.Builder
	fmt.Fprintf(&b, "Event Details:\n- Title: %s\n- Starts: %s\n- Ends: %s\n- Description: %s\n- Location: %s\n- Color ID: %s\n- Attendees: %s\n- Attendee Details: %s\n",
		summary, startTime, endTime, description, location, colorID, attendeeEmails, attendeeDetails)

	if includeAttachments {
		attachmentDetails := formatEventAttachmentDetails(item.Attachments, "  ")
		fmt.Fprintf(&b, "- Attachments: %s\n", attachmentDetails)
	}

	fmt.Fprintf(&b, "- Event ID: %s\n- Link: %s", eventID, link)
	return b.String()
}

// parseRemindersJSON parses a JSON string of reminder objects into calendar.EventReminder slice.
func parseRemindersJSON(s string) ([]*calendar.EventReminder, string) {
	var raw []map[string]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Sprintf("invalid reminders JSON: %v", err)
	}
	if len(raw) > 5 {
		return nil, "maximum 5 reminders allowed"
	}

	var result []*calendar.EventReminder
	for _, r := range raw {
		method, _ := r["method"].(string)
		if method != "popup" && method != "email" {
			return nil, fmt.Sprintf("invalid reminder method: %q (must be 'popup' or 'email')", method)
		}
		minutesRaw, ok := r["minutes"]
		if !ok {
			return nil, "reminder must have 'minutes' field"
		}
		var minutes int64
		switch v := minutesRaw.(type) {
		case float64:
			minutes = int64(v)
		case int:
			minutes = int64(v)
		default:
			return nil, fmt.Sprintf("invalid minutes value: %v", minutesRaw)
		}
		if minutes < 0 || minutes > 40320 {
			return nil, fmt.Sprintf("minutes must be between 0 and 40320, got %d", minutes)
		}
		result = append(result, &calendar.EventReminder{
			Method:  method,
			Minutes: minutes,
		})
	}
	return result, ""
}

// extractDriveFileID extracts a Drive file ID from a URL or returns the string as-is if it's already an ID.
func extractDriveFileID(input string) string {
	if strings.HasPrefix(input, "https://") {
		// Try to extract file ID from various Drive URL formats
		for _, pattern := range []string{"/d/", "/file/d/"} {
			idx := strings.Index(input, pattern)
			if idx >= 0 {
				rest := input[idx+len(pattern):]
				if slashIdx := strings.IndexAny(rest, "/?"); slashIdx >= 0 {
					return rest[:slashIdx]
				}
				return rest
			}
		}
		// Try id= parameter
		if idx := strings.Index(input, "id="); idx >= 0 {
			rest := input[idx+3:]
			if ampIdx := strings.IndexByte(rest, '&'); ampIdx >= 0 {
				return rest[:ampIdx]
			}
			return rest
		}
		return ""
	}
	return input
}
