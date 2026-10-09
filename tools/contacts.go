package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	people "google.golang.org/api/people/v1"

	"github.com/magks/google-workspace-mcp-go/internal/google"
	"github.com/magks/google-workspace-mcp-go/server"
)

const (
	defaultPersonFields  = "names,emailAddresses,phoneNumbers,organizations"
	detailedPersonFields = "names,emailAddresses,phoneNumbers,organizations,biographies,addresses,birthdays,urls,photos,metadata,memberships"
	contactGroupFields   = "name,groupType,memberCount,metadata"
)

// searchCacheWarmed tracks whether the People API search cache has been
// warmed up for each user email.
var (
	searchCacheWarmed   = make(map[string]bool)
	searchCacheWarmedMu sync.Mutex
)

// RegisterContactsTools registers all Contacts tools with the MCP server.
func RegisterContactsTools(s *mcp.Server, _ server.Config) {
	getClient := clientFuncFromCache(google.DefaultClientCache())

	// Read tools (US-021)
	registerListContacts(s, getClient)
	registerGetContact(s, getClient)
	registerSearchContacts(s, getClient)
	registerListContactGroups(s, getClient)
	registerGetContactGroup(s, getClient)

	// Write tools (US-022)
	registerCreateContact(s, getClient)
	registerUpdateContact(s, getClient)
	registerDeleteContact(s, getClient)
	registerBatchCreateContacts(s, getClient)
	registerBatchUpdateContacts(s, getClient)
	registerBatchDeleteContacts(s, getClient)
	registerCreateContactGroup(s, getClient)
	registerUpdateContactGroup(s, getClient)
	registerDeleteContactGroup(s, getClient)
	registerModifyContactGroupMembers(s, getClient)
}

// newPeopleService creates a people.Service for the given user email.
func newPeopleService(ctx context.Context, getClient httpClientFunc, email string) (*people.Service, error) {
	httpClient, err := getClient(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("authenticating for %s: %w", email, err)
	}
	svc, err := people.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating People service: %w", err)
	}
	return svc, nil
}

// warmupSearchCache warms up the People API search cache for the given user.
// The People API requires an initial empty query before searches return results.
func warmupSearchCache(svc *people.Service, email string) {
	searchCacheWarmedMu.Lock()
	if searchCacheWarmed[email] {
		searchCacheWarmedMu.Unlock()
		return
	}
	searchCacheWarmedMu.Unlock()

	// Fire and ignore errors — warmup failure is non-fatal.
	_, _ = svc.People.SearchContacts().Query("").ReadMask("names").PageSize(1).Do()

	searchCacheWarmedMu.Lock()
	searchCacheWarmed[email] = true
	searchCacheWarmedMu.Unlock()
}

// formatContact formats a Person resource into a readable string.
func formatContact(person *people.Person, detailed bool) string {
	if person == nil {
		return ""
	}

	resourceName := person.ResourceName
	contactID := strings.TrimPrefix(resourceName, "people/")
	if contactID == "" {
		contactID = "Unknown"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Contact ID: %s", contactID))

	// Names
	if len(person.Names) > 0 {
		displayName := person.Names[0].DisplayName
		if displayName != "" {
			lines = append(lines, fmt.Sprintf("Name: %s", displayName))
		}
	}

	// Email addresses
	if len(person.EmailAddresses) > 0 {
		var emails []string
		for _, e := range person.EmailAddresses {
			if e.Value != "" {
				emails = append(emails, e.Value)
			}
		}
		if len(emails) > 0 {
			lines = append(lines, fmt.Sprintf("Email: %s", strings.Join(emails, ", ")))
		}
	}

	// Phone numbers
	if len(person.PhoneNumbers) > 0 {
		var phones []string
		for _, p := range person.PhoneNumbers {
			if p.Value != "" {
				phones = append(phones, p.Value)
			}
		}
		if len(phones) > 0 {
			lines = append(lines, fmt.Sprintf("Phone: %s", strings.Join(phones, ", ")))
		}
	}

	// Organizations
	if len(person.Organizations) > 0 {
		org := person.Organizations[0]
		var orgParts []string
		if org.Title != "" {
			orgParts = append(orgParts, org.Title)
		}
		if org.Name != "" {
			orgParts = append(orgParts, fmt.Sprintf("at %s", org.Name))
		}
		if len(orgParts) > 0 {
			lines = append(lines, fmt.Sprintf("Organization: %s", strings.Join(orgParts, " ")))
		}
	}

	if detailed {
		// Addresses
		if len(person.Addresses) > 0 {
			if fv := person.Addresses[0].FormattedValue; fv != "" {
				lines = append(lines, fmt.Sprintf("Address: %s", fv))
			}
		}

		// Birthday
		if len(person.Birthdays) > 0 {
			if bday := person.Birthdays[0].Date; bday != nil {
				bdayStr := fmt.Sprintf("%d/%d", bday.Month, bday.Day)
				if bday.Year > 0 {
					bdayStr = fmt.Sprintf("%d/%s", bday.Year, bdayStr)
				}
				lines = append(lines, fmt.Sprintf("Birthday: %s", bdayStr))
			}
		}

		// URLs
		if len(person.Urls) > 0 {
			var urls []string
			for _, u := range person.Urls {
				if u.Value != "" {
					urls = append(urls, u.Value)
				}
			}
			if len(urls) > 0 {
				lines = append(lines, fmt.Sprintf("URLs: %s", strings.Join(urls, ", ")))
			}
		}

		// Biography/Notes
		if len(person.Biographies) > 0 {
			bio := person.Biographies[0].Value
			if bio != "" {
				if len(bio) > 200 {
					bio = bio[:200] + "..."
				}
				lines = append(lines, fmt.Sprintf("Notes: %s", bio))
			}
		}

		// Metadata sources
		if person.Metadata != nil && len(person.Metadata.Sources) > 0 {
			var sourceTypes []string
			for _, src := range person.Metadata.Sources {
				if src.Type != "" {
					sourceTypes = append(sourceTypes, src.Type)
				}
			}
			if len(sourceTypes) > 0 {
				lines = append(lines, fmt.Sprintf("Sources: %s", strings.Join(sourceTypes, ", ")))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// list_contacts
// ---------------------------------------------------------------------------

// listContactsIn is the input schema for list_contacts.
type listContactsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Maximum number of contacts to return (default: 100, max: 1000)."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for pagination."`
	SortOrder       string `json:"sort_order,omitempty" jsonschema:"Sort order."`
}

func registerListContacts(s *mcp.Server, getClient httpClientFunc) {
	tool := inputSchema[listContactsIn](&mcp.Tool{
		Name:        "list_contacts",
		Description: "List contacts for the authenticated user.",
	}, func(sch *jsonschema.Schema) {
		setEnum(sch, "sort_order", "LAST_MODIFIED_ASCENDING", "LAST_MODIFIED_DESCENDING", "FIRST_NAME_ASCENDING", "LAST_NAME_ASCENDING")
	})
	mcp.AddTool(s, tool, handleListContacts(getClient))
}

func handleListContacts(getClient httpClientFunc) mcp.ToolHandlerFor[listContactsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listContactsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 100
		}
		if pageSize > 1000 {
			pageSize = 1000
		}
		pageToken := in.PageToken
		sortOrder := in.SortOrder

		call := svc.People.Connections.List("people/me").
			PersonFields(defaultPersonFields).
			PageSize(int64(pageSize))

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		if sortOrder != "" {
			call = call.SortOrder(sortOrder)
		}

		result, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("listing contacts: %v", err)
		}

		connections := result.Connections
		if len(connections) == 0 {
			return textResult(fmt.Sprintf("No contacts found for %s.", email))
		}

		totalPeople := int(result.TotalPeople)
		if totalPeople == 0 {
			totalPeople = len(connections)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contacts for %s (%d of %d):\n\n", email, len(connections), totalPeople)

		for _, person := range connections {
			sb.WriteString(formatContact(person, false))
			sb.WriteString("\n\n")
		}

		if result.NextPageToken != "" {
			fmt.Fprintf(&sb, "Next page token: %s", result.NextPageToken)
		}

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// get_contact
// ---------------------------------------------------------------------------

// getContactIn is the input schema for get_contact.
type getContactIn struct {
	ContactID       string `json:"contact_id" jsonschema:"The contact ID (e.g., \"c1234567890\" or full resource name \"people/c1234567890\")."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerGetContact(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "get_contact", Description: "Get detailed information about a specific contact."}
	mcp.AddTool(s, tool, handleGetContact(getClient))
}

func handleGetContact(getClient httpClientFunc) mcp.ToolHandlerFor[getContactIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getContactIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		contactID := in.ContactID

		// Normalize resource name
		resourceName := contactID
		if !strings.HasPrefix(contactID, "people/") {
			resourceName = "people/" + contactID
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		person, err := svc.People.Get(resourceName).PersonFields(detailedPersonFields).Do()
		if err != nil {
			return nil, nil, toolErr("getting contact %s: %v", contactID, err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Details for %s:\n\n", email)
		sb.WriteString(formatContact(person, true))

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// search_contacts
// ---------------------------------------------------------------------------

// searchContactsIn is the input schema for search_contacts.
type searchContactsIn struct {
	Query           string `json:"query" jsonschema:"Search query string (searches names, emails, phone numbers)."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Maximum number of results to return (default: 30, max: 30)."`
}

func registerSearchContacts(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "search_contacts", Description: "Search contacts by name, email, phone number, or other fields."}
	mcp.AddTool(s, tool, handleSearchContacts(getClient))
}

func handleSearchContacts(getClient httpClientFunc) mcp.ToolHandlerFor[searchContactsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchContactsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		query := in.Query

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Warm up the search cache if needed
		warmupSearchCache(svc, email)

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 30
		}
		if pageSize > 30 {
			pageSize = 30
		}

		result, err := svc.People.SearchContacts().
			Query(query).
			ReadMask(defaultPersonFields).
			PageSize(int64(pageSize)).
			Do()
		if err != nil {
			return nil, nil, toolErr("searching contacts: %v", err)
		}

		results := result.Results
		if len(results) == 0 {
			return textResult(fmt.Sprintf("No contacts found matching '%s' for %s.", query, email))
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Search Results for '%s' (%d found):\n\n", query, len(results))

		for _, item := range results {
			sb.WriteString(formatContact(item.Person, false))
			sb.WriteString("\n\n")
		}

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// list_contact_groups
// ---------------------------------------------------------------------------

// listContactGroupsIn is the input schema for list_contact_groups.
type listContactGroupsIn struct {
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	PageSize        int    `json:"page_size,omitempty" jsonschema:"Maximum number of groups to return (default: 100, max: 1000)."`
	PageToken       string `json:"page_token,omitempty" jsonschema:"Token for pagination."`
}

func registerListContactGroups(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "list_contact_groups", Description: "List contact groups (labels) for the user."}
	mcp.AddTool(s, tool, handleListContactGroups(getClient))
}

func handleListContactGroups(getClient httpClientFunc) mcp.ToolHandlerFor[listContactGroupsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listContactGroupsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		pageSize := in.PageSize
		if pageSize == 0 {
			pageSize = 100
		}
		if pageSize > 1000 {
			pageSize = 1000
		}
		pageToken := in.PageToken

		call := svc.ContactGroups.List().
			PageSize(int64(pageSize)).
			GroupFields(contactGroupFields)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return nil, nil, toolErr("listing contact groups: %v", err)
		}

		groups := result.ContactGroups
		if len(groups) == 0 {
			return textResult(fmt.Sprintf("No contact groups found for %s.", email))
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Groups for %s:\n\n", email)

		for _, group := range groups {
			groupID := strings.TrimPrefix(group.ResourceName, "contactGroups/")
			name := group.Name
			if name == "" {
				name = "Unnamed"
			}
			groupType := group.GroupType
			if groupType == "" {
				groupType = "USER_CONTACT_GROUP"
			}

			fmt.Fprintf(&sb, "- %s\n", name)
			fmt.Fprintf(&sb, "  ID: %s\n", groupID)
			fmt.Fprintf(&sb, "  Type: %s\n", groupType)
			fmt.Fprintf(&sb, "  Members: %d\n\n", group.MemberCount)
		}

		if result.NextPageToken != "" {
			fmt.Fprintf(&sb, "Next page token: %s", result.NextPageToken)
		}

		return textResult(sb.String())
	}
}

// normalizeContactResourceName ensures a contact ID is prefixed with "people/".
func normalizeContactResourceName(contactID string) string {
	if strings.HasPrefix(contactID, "people/") {
		return contactID
	}
	return "people/" + contactID
}

// normalizeGroupResourceName ensures a group ID is prefixed with "contactGroups/".
func normalizeGroupResourceName(groupID string) string {
	if strings.HasPrefix(groupID, "contactGroups/") {
		return groupID
	}
	return "contactGroups/" + groupID
}

// buildPersonBody constructs a people.Person from optional contact fields.
// Returns nil if no fields are set.
func buildPersonBody(args map[string]any) *people.Person {
	givenName, _ := args["given_name"].(string)
	familyName, _ := args["family_name"].(string)
	email, _ := args["email"].(string)
	phone, _ := args["phone"].(string)
	organization, _ := args["organization"].(string)
	jobTitle, _ := args["job_title"].(string)
	notes, _ := args["notes"].(string)

	p := &people.Person{}
	hasField := false

	if givenName != "" || familyName != "" {
		p.Names = []*people.Name{{GivenName: givenName, FamilyName: familyName}}
		hasField = true
	}
	if email != "" {
		p.EmailAddresses = []*people.EmailAddress{{Value: email}}
		hasField = true
	}
	if phone != "" {
		p.PhoneNumbers = []*people.PhoneNumber{{Value: phone}}
		hasField = true
	}
	if organization != "" || jobTitle != "" {
		org := &people.Organization{}
		if organization != "" {
			org.Name = organization
		}
		if jobTitle != "" {
			org.Title = jobTitle
		}
		p.Organizations = []*people.Organization{org}
		hasField = true
	}
	if notes != "" {
		p.Biographies = []*people.Biography{{Value: notes, ContentType: "TEXT_PLAIN"}}
		hasField = true
	}

	if !hasField {
		return nil
	}
	return p
}

// updatePersonFields returns the comma-separated list of fields present in a Person.
func updatePersonFields(p *people.Person) string {
	var fields []string
	if len(p.Names) > 0 {
		fields = append(fields, "names")
	}
	if len(p.EmailAddresses) > 0 {
		fields = append(fields, "emailAddresses")
	}
	if len(p.PhoneNumbers) > 0 {
		fields = append(fields, "phoneNumbers")
	}
	if len(p.Organizations) > 0 {
		fields = append(fields, "organizations")
	}
	if len(p.Biographies) > 0 {
		fields = append(fields, "biographies")
	}
	if len(p.Addresses) > 0 {
		fields = append(fields, "addresses")
	}
	return strings.Join(fields, ",")
}

// ---------------------------------------------------------------------------
// get_contact_group
// ---------------------------------------------------------------------------

// getContactGroupIn is the input schema for get_contact_group.
type getContactGroupIn struct {
	GroupID         string `json:"group_id" jsonschema:"The contact group ID."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	MaxMembers      int    `json:"max_members,omitempty" jsonschema:"Maximum number of members to return (default: 100, max: 1000)."`
}

func registerGetContactGroup(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "get_contact_group", Description: "Get details of a specific contact group including its members."}
	mcp.AddTool(s, tool, handleGetContactGroup(getClient))
}

func handleGetContactGroup(getClient httpClientFunc) mcp.ToolHandlerFor[getContactGroupIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in getContactGroupIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		groupID := in.GroupID

		// Normalize resource name
		resourceName := groupID
		if !strings.HasPrefix(groupID, "contactGroups/") {
			resourceName = "contactGroups/" + groupID
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		maxMembers := in.MaxMembers
		if maxMembers == 0 {
			maxMembers = 100
		}
		if maxMembers > 1000 {
			maxMembers = 1000
		}

		result, err := svc.ContactGroups.Get(resourceName).
			MaxMembers(int64(maxMembers)).
			GroupFields(contactGroupFields).
			Do()
		if err != nil {
			return nil, nil, toolErr("getting contact group %s: %v", groupID, err)
		}

		name := result.Name
		if name == "" {
			name = "Unnamed"
		}
		groupType := result.GroupType
		if groupType == "" {
			groupType = "USER_CONTACT_GROUP"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Group Details for %s:\n\n", email)
		fmt.Fprintf(&sb, "Name: %s\n", name)
		fmt.Fprintf(&sb, "ID: %s\n", groupID)
		fmt.Fprintf(&sb, "Type: %s\n", groupType)
		fmt.Fprintf(&sb, "Total Members: %d\n", result.MemberCount)

		if len(result.MemberResourceNames) > 0 {
			fmt.Fprintf(&sb, "\nMembers (%d shown):\n", len(result.MemberResourceNames))
			for _, member := range result.MemberResourceNames {
				contactID := strings.TrimPrefix(member, "people/")
				fmt.Fprintf(&sb, "  - %s\n", contactID)
			}
		}

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// create_contact
// ---------------------------------------------------------------------------

// createContactIn is the input schema for create_contact.
type createContactIn struct {
	GivenName       string `json:"given_name,omitempty" jsonschema:"First name."`
	FamilyName      string `json:"family_name,omitempty" jsonschema:"Last name."`
	Email           string `json:"email,omitempty" jsonschema:"Email address."`
	Phone           string `json:"phone,omitempty" jsonschema:"Phone number."`
	Organization    string `json:"organization,omitempty" jsonschema:"Company/organization name."`
	JobTitle        string `json:"job_title,omitempty" jsonschema:"Job title."`
	Notes           string `json:"notes,omitempty" jsonschema:"Additional notes."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerCreateContact(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "create_contact", Description: "Create a new contact."}
	mcp.AddTool(s, tool, handleCreateContact(getClient))
}

func handleCreateContact(getClient httpClientFunc) mcp.ToolHandlerFor[createContactIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createContactIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		person := buildPersonBody(map[string]any{
			"given_name":   in.GivenName,
			"family_name":  in.FamilyName,
			"email":        in.Email,
			"phone":        in.Phone,
			"organization": in.Organization,
			"job_title":    in.JobTitle,
			"notes":        in.Notes,
		})
		if person == nil {
			return nil, nil, toolErr("At least one field (name, email, phone, etc.) must be provided.")
		}

		result, err := svc.People.CreateContact(person).
			PersonFields(detailedPersonFields).
			Do()
		if err != nil {
			return nil, nil, toolErr("creating contact: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Created for %s:\n\n", email)
		sb.WriteString(formatContact(result, true))

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// update_contact
// ---------------------------------------------------------------------------

// updateContactIn is the input schema for update_contact.
type updateContactIn struct {
	ContactID       string `json:"contact_id" jsonschema:"The contact ID to update."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	GivenName       string `json:"given_name,omitempty" jsonschema:"New first name."`
	FamilyName      string `json:"family_name,omitempty" jsonschema:"New last name."`
	Email           string `json:"email,omitempty" jsonschema:"New email address."`
	Phone           string `json:"phone,omitempty" jsonschema:"New phone number."`
	Organization    string `json:"organization,omitempty" jsonschema:"New company/organization name."`
	JobTitle        string `json:"job_title,omitempty" jsonschema:"New job title."`
	Notes           string `json:"notes,omitempty" jsonschema:"New notes."`
}

func registerUpdateContact(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "update_contact", Description: "Update an existing contact. Note: This replaces fields, not merges them."}
	mcp.AddTool(s, tool, handleUpdateContact(getClient))
}

func handleUpdateContact(getClient httpClientFunc) mcp.ToolHandlerFor[updateContactIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateContactIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		contactID := in.ContactID

		resourceName := normalizeContactResourceName(contactID)

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Fetch current contact for etag
		current, err := svc.People.Get(resourceName).PersonFields(detailedPersonFields).Do()
		if err != nil {
			return nil, nil, toolErr("getting contact %s: %v", contactID, err)
		}
		if current.Etag == "" {
			return nil, nil, toolErr("Unable to get contact etag for update.")
		}

		person := buildPersonBody(map[string]any{
			"given_name":   in.GivenName,
			"family_name":  in.FamilyName,
			"email":        in.Email,
			"phone":        in.Phone,
			"organization": in.Organization,
			"job_title":    in.JobTitle,
			"notes":        in.Notes,
		})
		if person == nil {
			return nil, nil, toolErr("At least one field (name, email, phone, etc.) must be provided.")
		}
		person.Etag = current.Etag

		upf := updatePersonFields(person)
		if upf == "" {
			return nil, nil, toolErr("No fields to update.")
		}

		result, err := svc.People.UpdateContact(resourceName, person).
			UpdatePersonFields(upf).
			PersonFields(detailedPersonFields).
			Do()
		if err != nil {
			return nil, nil, toolErr("updating contact %s: %v", contactID, err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Updated for %s:\n\n", email)
		sb.WriteString(formatContact(result, true))

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// delete_contact
// ---------------------------------------------------------------------------

// deleteContactIn is the input schema for delete_contact.
type deleteContactIn struct {
	ContactID       string `json:"contact_id" jsonschema:"The contact ID to delete."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerDeleteContact(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "delete_contact", Description: "Delete a contact."}
	mcp.AddTool(s, tool, handleDeleteContact(getClient))
}

func handleDeleteContact(getClient httpClientFunc) mcp.ToolHandlerFor[deleteContactIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteContactIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		contactID := in.ContactID

		resourceName := normalizeContactResourceName(contactID)

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		_, err = svc.People.DeleteContact(resourceName).Do()
		if err != nil {
			return nil, nil, toolErr("deleting contact %s: %v", contactID, err)
		}

		return textResult(fmt.Sprintf("Contact %s has been deleted for %s.", contactID, email))
	}
}

// ---------------------------------------------------------------------------
// batch_create_contacts
// ---------------------------------------------------------------------------

// batchCreateContactsIn is the input schema for batch_create_contacts.
type batchCreateContactsIn struct {
	Contacts        []map[string]any `json:"contacts" jsonschema:"List of contact objects with fields: given_name, family_name, email, phone, organization, job_title."`
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerBatchCreateContacts(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "batch_create_contacts", Description: "Create multiple contacts in a batch operation."}
	mcp.AddTool(s, tool, handleBatchCreateContacts(getClient))
}

func handleBatchCreateContacts(getClient httpClientFunc) mcp.ToolHandlerFor[batchCreateContactsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchCreateContactsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		if len(in.Contacts) == 0 {
			return nil, nil, toolErr("At least one contact must be provided.")
		}
		if len(in.Contacts) > 200 {
			return nil, nil, toolErr("Maximum 200 contacts can be created in a batch.")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		var contacts []*people.ContactToCreate
		for _, m := range in.Contacts {
			person := buildPersonBody(m)
			if person != nil {
				contacts = append(contacts, &people.ContactToCreate{ContactPerson: person})
			}
		}

		if len(contacts) == 0 {
			return nil, nil, toolErr("No valid contact data provided.")
		}

		result, err := svc.People.BatchCreateContacts(&people.BatchCreateContactsRequest{
			Contacts: contacts,
			ReadMask: defaultPersonFields,
		}).Do()
		if err != nil {
			return nil, nil, toolErr("batch creating contacts: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Batch Create Results for %s:\n\n", email)
		fmt.Fprintf(&sb, "Created %d contacts:\n\n", len(result.CreatedPeople))

		for _, item := range result.CreatedPeople {
			if item.Person != nil {
				sb.WriteString(formatContact(item.Person, false))
				sb.WriteString("\n\n")
			}
		}

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// batch_update_contacts
// ---------------------------------------------------------------------------

// batchUpdateContactsIn is the input schema for batch_update_contacts.
type batchUpdateContactsIn struct {
	Updates         []map[string]any `json:"updates" jsonschema:"List of update objects with fields: contact_id (required), given_name, family_name, email, phone, organization, job_title."`
	UserGoogleEmail string           `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerBatchUpdateContacts(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "batch_update_contacts", Description: "Update multiple contacts in a batch operation."}
	mcp.AddTool(s, tool, handleBatchUpdateContacts(getClient))
}

func handleBatchUpdateContacts(getClient httpClientFunc) mcp.ToolHandlerFor[batchUpdateContactsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchUpdateContactsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		if len(in.Updates) == 0 {
			return nil, nil, toolErr("At least one update must be provided.")
		}
		if len(in.Updates) > 200 {
			return nil, nil, toolErr("Maximum 200 contacts can be updated in a batch.")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		// Collect resource names for batch etag lookup
		var resourceNames []string
		for _, m := range in.Updates {
			cid, _ := m["contact_id"].(string)
			if cid == "" {
				return nil, nil, toolErr("Each update must include a contact_id.")
			}
			resourceNames = append(resourceNames, normalizeContactResourceName(cid))
		}

		// Batch fetch etags
		batchGet, err := svc.People.GetBatchGet().
			ResourceNames(resourceNames...).
			PersonFields("metadata").
			Do()
		if err != nil {
			return nil, nil, toolErr("fetching contact etags: %v", err)
		}

		etags := make(map[string]string)
		for _, resp := range batchGet.Responses {
			if resp.Person != nil && resp.Person.ResourceName != "" && resp.Person.Etag != "" {
				etags[resp.Person.ResourceName] = resp.Person.Etag
			}
		}

		// Build batch update body
		contactsMap := make(map[string]people.Person)
		updateFieldsSet := make(map[string]bool)

		for _, m := range in.Updates {
			cid, _ := m["contact_id"].(string)
			rn := normalizeContactResourceName(cid)

			etag, ok := etags[rn]
			if !ok {
				continue
			}

			person := buildPersonBody(m)
			if person == nil {
				continue
			}
			person.ResourceName = rn
			person.Etag = etag

			// Track update fields
			for _, f := range strings.Split(updatePersonFields(person), ",") {
				if f != "" {
					updateFieldsSet[f] = true
				}
			}

			contactsMap[rn] = *person
		}

		if len(contactsMap) == 0 {
			return nil, nil, toolErr("No valid update data provided.")
		}

		var updateMaskParts []string
		for f := range updateFieldsSet {
			updateMaskParts = append(updateMaskParts, f)
		}

		result, err := svc.People.BatchUpdateContacts(&people.BatchUpdateContactsRequest{
			Contacts:   contactsMap,
			UpdateMask: strings.Join(updateMaskParts, ","),
			ReadMask:   defaultPersonFields,
		}).Do()
		if err != nil {
			return nil, nil, toolErr("batch updating contacts: %v", err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Batch Update Results for %s:\n\n", email)
		fmt.Fprintf(&sb, "Updated %d contacts:\n\n", len(result.UpdateResult))

		for _, personResp := range result.UpdateResult {
			if personResp.Person != nil {
				sb.WriteString(formatContact(personResp.Person, false))
				sb.WriteString("\n\n")
			}
		}

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// batch_delete_contacts
// ---------------------------------------------------------------------------

// batchDeleteContactsIn is the input schema for batch_delete_contacts.
type batchDeleteContactsIn struct {
	ContactIDs      []string `json:"contact_ids" jsonschema:"List of contact IDs to delete."`
	UserGoogleEmail string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerBatchDeleteContacts(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "batch_delete_contacts", Description: "Delete multiple contacts in a batch operation."}
	mcp.AddTool(s, tool, handleBatchDeleteContacts(getClient))
}

func handleBatchDeleteContacts(getClient httpClientFunc) mcp.ToolHandlerFor[batchDeleteContactsIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in batchDeleteContactsIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}

		contactIDs := in.ContactIDs
		if len(contactIDs) == 0 {
			return nil, nil, toolErr("At least one contact ID must be provided.")
		}
		if len(contactIDs) > 500 {
			return nil, nil, toolErr("Maximum 500 contacts can be deleted in a batch.")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		resourceNames := make([]string, len(contactIDs))
		for i, cid := range contactIDs {
			resourceNames[i] = normalizeContactResourceName(cid)
		}

		_, err = svc.People.BatchDeleteContacts(&people.BatchDeleteContactsRequest{
			ResourceNames: resourceNames,
		}).Do()
		if err != nil {
			return nil, nil, toolErr("batch deleting contacts: %v", err)
		}

		return textResult(fmt.Sprintf("Batch deleted %d contacts for %s.", len(contactIDs), email))
	}
}

// ---------------------------------------------------------------------------
// create_contact_group
// ---------------------------------------------------------------------------

// createContactGroupIn is the input schema for create_contact_group.
type createContactGroupIn struct {
	Name            string `json:"name" jsonschema:"The name of the new contact group."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerCreateContactGroup(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "create_contact_group", Description: "Create a new contact group (label)."}
	mcp.AddTool(s, tool, handleCreateContactGroup(getClient))
}

func handleCreateContactGroup(getClient httpClientFunc) mcp.ToolHandlerFor[createContactGroupIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createContactGroupIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		name := in.Name

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		result, err := svc.ContactGroups.Create(&people.CreateContactGroupRequest{
			ContactGroup: &people.ContactGroup{Name: name},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("creating contact group: %v", err)
		}

		groupID := strings.TrimPrefix(result.ResourceName, "contactGroups/")
		createdName := result.Name
		if createdName == "" {
			createdName = name
		}
		groupType := result.GroupType
		if groupType == "" {
			groupType = "USER_CONTACT_GROUP"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Group Created for %s:\n\n", email)
		fmt.Fprintf(&sb, "Name: %s\n", createdName)
		fmt.Fprintf(&sb, "ID: %s\n", groupID)
		fmt.Fprintf(&sb, "Type: %s\n", groupType)

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// update_contact_group
// ---------------------------------------------------------------------------

// updateContactGroupIn is the input schema for update_contact_group.
type updateContactGroupIn struct {
	GroupID         string `json:"group_id" jsonschema:"The contact group ID to update."`
	Name            string `json:"name" jsonschema:"The new name for the contact group."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
}

func registerUpdateContactGroup(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "update_contact_group", Description: "Update a contact group's name."}
	mcp.AddTool(s, tool, handleUpdateContactGroup(getClient))
}

func handleUpdateContactGroup(getClient httpClientFunc) mcp.ToolHandlerFor[updateContactGroupIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in updateContactGroupIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		groupID := in.GroupID
		name := in.Name

		resourceName := normalizeGroupResourceName(groupID)

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		result, err := svc.ContactGroups.Update(resourceName, &people.UpdateContactGroupRequest{
			ContactGroup: &people.ContactGroup{Name: name},
		}).Do()
		if err != nil {
			return nil, nil, toolErr("updating contact group %s: %v", groupID, err)
		}

		updatedName := result.Name
		if updatedName == "" {
			updatedName = name
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Group Updated for %s:\n\n", email)
		fmt.Fprintf(&sb, "Name: %s\n", updatedName)
		fmt.Fprintf(&sb, "ID: %s\n", groupID)

		return textResult(sb.String())
	}
}

// ---------------------------------------------------------------------------
// delete_contact_group
// ---------------------------------------------------------------------------

// deleteContactGroupIn is the input schema for delete_contact_group.
type deleteContactGroupIn struct {
	GroupID         string `json:"group_id" jsonschema:"The contact group ID to delete."`
	UserGoogleEmail string `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	DeleteContacts  bool   `json:"delete_contacts,omitempty" jsonschema:"If true, also delete contacts in the group (default: false)."`
}

func registerDeleteContactGroup(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "delete_contact_group", Description: "Delete a contact group."}
	mcp.AddTool(s, tool, handleDeleteContactGroup(getClient))
}

func handleDeleteContactGroup(getClient httpClientFunc) mcp.ToolHandlerFor[deleteContactGroupIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in deleteContactGroupIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		groupID := in.GroupID

		resourceName := normalizeGroupResourceName(groupID)
		deleteContacts := in.DeleteContacts

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		_, err = svc.ContactGroups.Delete(resourceName).
			DeleteContacts(deleteContacts).
			Do()
		if err != nil {
			return nil, nil, toolErr("deleting contact group %s: %v", groupID, err)
		}

		msg := fmt.Sprintf("Contact group %s has been deleted for %s.", groupID, email)
		if deleteContacts {
			msg += " Contacts in the group were also deleted."
		} else {
			msg += " Contacts in the group were preserved."
		}

		return textResult(msg)
	}
}

// ---------------------------------------------------------------------------
// modify_contact_group_members
// ---------------------------------------------------------------------------

// modifyContactGroupMembersIn is the input schema for modify_contact_group_members.
type modifyContactGroupMembersIn struct {
	GroupID          string   `json:"group_id" jsonschema:"The contact group ID."`
	UserGoogleEmail  string   `json:"user_google_email,omitempty" jsonschema:"The user's Google email address."`
	AddContactIDs    []string `json:"add_contact_ids,omitempty" jsonschema:"Contact IDs to add to the group."`
	RemoveContactIDs []string `json:"remove_contact_ids,omitempty" jsonschema:"Contact IDs to remove from the group."`
}

func registerModifyContactGroupMembers(s *mcp.Server, getClient httpClientFunc) {
	tool := &mcp.Tool{Name: "modify_contact_group_members", Description: "Add or remove contacts from a contact group."}
	mcp.AddTool(s, tool, handleModifyContactGroupMembers(getClient))
}

func handleModifyContactGroupMembers(getClient httpClientFunc) mcp.ToolHandlerFor[modifyContactGroupMembersIn, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in modifyContactGroupMembersIn) (*mcp.CallToolResult, any, error) {
		email, err := resolveEmail(in.UserGoogleEmail)
		if err != nil {
			return nil, nil, toolErr("user_google_email is required")
		}
		groupID := in.GroupID

		resourceName := normalizeGroupResourceName(groupID)

		addIDs := in.AddContactIDs
		removeIDs := in.RemoveContactIDs

		if len(addIDs) == 0 && len(removeIDs) == 0 {
			return nil, nil, toolErr("At least one of add_contact_ids or remove_contact_ids must be provided.")
		}

		svc, err := newPeopleService(ctx, getClient, email)
		if err != nil {
			return nil, nil, err
		}

		body := &people.ModifyContactGroupMembersRequest{}

		if len(addIDs) > 0 {
			addNames := make([]string, len(addIDs))
			for i, cid := range addIDs {
				addNames[i] = normalizeContactResourceName(cid)
			}
			body.ResourceNamesToAdd = addNames
		}
		if len(removeIDs) > 0 {
			removeNames := make([]string, len(removeIDs))
			for i, cid := range removeIDs {
				removeNames[i] = normalizeContactResourceName(cid)
			}
			body.ResourceNamesToRemove = removeNames
		}

		result, err := svc.ContactGroups.Members.Modify(resourceName, body).Do()
		if err != nil {
			return nil, nil, toolErr("modifying contact group members %s: %v", groupID, err)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Contact Group Members Modified for %s:\n\n", email)
		fmt.Fprintf(&sb, "Group: %s\n", groupID)

		if len(addIDs) > 0 {
			fmt.Fprintf(&sb, "Added: %d contacts\n", len(addIDs))
		}
		if len(removeIDs) > 0 {
			fmt.Fprintf(&sb, "Removed: %d contacts\n", len(removeIDs))
		}

		if len(result.NotFoundResourceNames) > 0 {
			fmt.Fprintf(&sb, "\nNot found: %s\n", strings.Join(result.NotFoundResourceNames, ", "))
		}
		if len(result.CanNotRemoveLastContactGroupResourceNames) > 0 {
			fmt.Fprintf(&sb, "\nCannot remove (last group): %s\n", strings.Join(result.CanNotRemoveLastContactGroupResourceNames, ", "))
		}

		return textResult(sb.String())
	}
}
