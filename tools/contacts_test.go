package tools

import (
	"strings"
	"testing"

	people "google.golang.org/api/people/v1"
)

// --- formatContact ---

func TestContactsFormatContact(t *testing.T) {
	tests := []struct {
		name         string
		person       *people.Person
		detailed     bool
		wantContains []string
		wantExact    string
	}{
		{
			"nil person returns empty",
			nil,
			false,
			nil,
			"",
		},
		{
			"minimal person with resource name",
			&people.Person{ResourceName: "people/c123"},
			false,
			[]string{"Contact ID: c123"},
			"",
		},
		{
			"empty resource name shows Unknown",
			&people.Person{ResourceName: ""},
			false,
			[]string{"Contact ID: Unknown"},
			"",
		},
		{
			"resource name without people/ prefix",
			&people.Person{ResourceName: "c456"},
			false,
			[]string{"Contact ID: c456"},
			"",
		},
		{
			"person with name",
			&people.Person{
				ResourceName: "people/c1",
				Names:        []*people.Name{{DisplayName: "John Doe"}},
			},
			false,
			[]string{"Contact ID: c1", "Name: John Doe"},
			"",
		},
		{
			"person with emails",
			&people.Person{
				ResourceName:   "people/c1",
				EmailAddresses: []*people.EmailAddress{{Value: "a@b.com"}, {Value: "c@d.com"}},
			},
			false,
			[]string{"Email: a@b.com, c@d.com"},
			"",
		},
		{
			"person with empty email values skipped",
			&people.Person{
				ResourceName:   "people/c1",
				EmailAddresses: []*people.EmailAddress{{Value: ""}, {Value: "x@y.com"}},
			},
			false,
			[]string{"Email: x@y.com"},
			"",
		},
		{
			"person with phone numbers",
			&people.Person{
				ResourceName: "people/c1",
				PhoneNumbers: []*people.PhoneNumber{{Value: "555-1234"}, {Value: "555-5678"}},
			},
			false,
			[]string{"Phone: 555-1234, 555-5678"},
			"",
		},
		{
			"person with organization title and name",
			&people.Person{
				ResourceName:  "people/c1",
				Organizations: []*people.Organization{{Title: "Engineer", Name: "Acme Corp"}},
			},
			false,
			[]string{"Organization: Engineer at Acme Corp"},
			"",
		},
		{
			"person with organization name only",
			&people.Person{
				ResourceName:  "people/c1",
				Organizations: []*people.Organization{{Name: "Acme Corp"}},
			},
			false,
			[]string{"Organization: at Acme Corp"},
			"",
		},
		{
			"person with organization title only",
			&people.Person{
				ResourceName:  "people/c1",
				Organizations: []*people.Organization{{Title: "Engineer"}},
			},
			false,
			[]string{"Organization: Engineer"},
			"",
		},
		{
			"detailed=false skips address, birthday, urls, bio, sources",
			&people.Person{
				ResourceName: "people/c1",
				Addresses:    []*people.Address{{FormattedValue: "123 Main St"}},
				Birthdays:    []*people.Birthday{{Date: &people.Date{Year: 1990, Month: 6, Day: 15}}},
				Urls:         []*people.Url{{Value: "https://example.com"}},
				Biographies:  []*people.Biography{{Value: "A bio"}},
			},
			false,
			[]string{"Contact ID: c1"},
			"",
		},
		{
			"detailed=true includes address",
			&people.Person{
				ResourceName: "people/c1",
				Addresses:    []*people.Address{{FormattedValue: "123 Main St"}},
			},
			true,
			[]string{"Address: 123 Main St"},
			"",
		},
		{
			"detailed=true includes birthday with year",
			&people.Person{
				ResourceName: "people/c1",
				Birthdays:    []*people.Birthday{{Date: &people.Date{Year: 1990, Month: 6, Day: 15}}},
			},
			true,
			[]string{"Birthday: 1990/6/15"},
			"",
		},
		{
			"detailed=true includes birthday without year",
			&people.Person{
				ResourceName: "people/c1",
				Birthdays:    []*people.Birthday{{Date: &people.Date{Year: 0, Month: 3, Day: 25}}},
			},
			true,
			[]string{"Birthday: 3/25"},
			"",
		},
		{
			"detailed=true includes URLs",
			&people.Person{
				ResourceName: "people/c1",
				Urls:         []*people.Url{{Value: "https://a.com"}, {Value: "https://b.com"}},
			},
			true,
			[]string{"URLs: https://a.com, https://b.com"},
			"",
		},
		{
			"detailed=true includes bio truncated at 200",
			&people.Person{
				ResourceName: "people/c1",
				Biographies:  []*people.Biography{{Value: strings.Repeat("x", 250)}},
			},
			true,
			[]string{"Notes: " + strings.Repeat("x", 200) + "..."},
			"",
		},
		{
			"detailed=true includes short bio without truncation",
			&people.Person{
				ResourceName: "people/c1",
				Biographies:  []*people.Biography{{Value: "Short bio"}},
			},
			true,
			[]string{"Notes: Short bio"},
			"",
		},
		{
			"detailed=true includes metadata sources",
			&people.Person{
				ResourceName: "people/c1",
				Metadata: &people.PersonMetadata{
					Sources: []*people.Source{{Type: "CONTACT"}, {Type: "PROFILE"}},
				},
			},
			true,
			[]string{"Sources: CONTACT, PROFILE"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatContact(tt.person, tt.detailed)
			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("formatContact() = %q, want %q", got, tt.wantExact)
				}
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("formatContact() = %q, want contains %q", got, want)
				}
			}
			// Verify detailed fields are NOT present when detailed=false
			if !tt.detailed && tt.person != nil {
				if strings.Contains(got, "Address:") {
					t.Error("non-detailed should not contain Address")
				}
				if strings.Contains(got, "Birthday:") {
					t.Error("non-detailed should not contain Birthday")
				}
				if strings.Contains(got, "URLs:") {
					t.Error("non-detailed should not contain URLs")
				}
				if strings.Contains(got, "Notes:") {
					t.Error("non-detailed should not contain Notes")
				}
				if strings.Contains(got, "Sources:") {
					t.Error("non-detailed should not contain Sources")
				}
			}
		})
	}
}

// --- normalizeContactResourceName ---

func TestContactsNormalizeContactResourceName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already prefixed", "people/c123", "people/c123"},
		{"without prefix", "c123", "people/c123"},
		{"empty string", "", "people/"},
		{"nested prefix not doubled", "people/people/c1", "people/people/c1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeContactResourceName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeContactResourceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- normalizeGroupResourceName ---

func TestContactsNormalizeGroupResourceName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already prefixed", "contactGroups/abc", "contactGroups/abc"},
		{"without prefix", "abc", "contactGroups/abc"},
		{"empty string", "", "contactGroups/"},
		{"nested prefix not doubled", "contactGroups/contactGroups/x", "contactGroups/contactGroups/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeGroupResourceName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGroupResourceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- buildPersonBody ---

func TestContactsBuildPersonBody(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		wantNil    bool
		wantChecks func(t *testing.T, p *people.Person)
	}{
		{
			"nil args returns nil",
			nil,
			true,
			nil,
		},
		{
			"empty args returns nil",
			map[string]any{},
			true,
			nil,
		},
		{
			"non-string values return nil",
			map[string]any{"given_name": 123},
			true,
			nil,
		},
		{
			"given_name only",
			map[string]any{"given_name": "John"},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.Names) != 1 || p.Names[0].GivenName != "John" {
					t.Errorf("expected given_name 'John', got %+v", p.Names)
				}
			},
		},
		{
			"family_name only",
			map[string]any{"family_name": "Doe"},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.Names) != 1 || p.Names[0].FamilyName != "Doe" {
					t.Errorf("expected family_name 'Doe', got %+v", p.Names)
				}
			},
		},
		{
			"email only",
			map[string]any{"email": "test@example.com"},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.EmailAddresses) != 1 || p.EmailAddresses[0].Value != "test@example.com" {
					t.Errorf("expected email 'test@example.com', got %+v", p.EmailAddresses)
				}
			},
		},
		{
			"phone only",
			map[string]any{"phone": "555-1234"},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.PhoneNumbers) != 1 || p.PhoneNumbers[0].Value != "555-1234" {
					t.Errorf("expected phone '555-1234', got %+v", p.PhoneNumbers)
				}
			},
		},
		{
			"organization and job_title",
			map[string]any{"organization": "Acme", "job_title": "Dev"},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.Organizations) != 1 {
					t.Fatalf("expected 1 org, got %d", len(p.Organizations))
				}
				if p.Organizations[0].Name != "Acme" {
					t.Errorf("expected org name 'Acme', got %q", p.Organizations[0].Name)
				}
				if p.Organizations[0].Title != "Dev" {
					t.Errorf("expected title 'Dev', got %q", p.Organizations[0].Title)
				}
			},
		},
		{
			"notes field sets biography",
			map[string]any{"notes": "Some notes"},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.Biographies) != 1 || p.Biographies[0].Value != "Some notes" {
					t.Errorf("expected biography 'Some notes', got %+v", p.Biographies)
				}
				if p.Biographies[0].ContentType != "TEXT_PLAIN" {
					t.Errorf("expected content type TEXT_PLAIN, got %q", p.Biographies[0].ContentType)
				}
			},
		},
		{
			"all fields set",
			map[string]any{
				"given_name":   "John",
				"family_name":  "Doe",
				"email":        "john@example.com",
				"phone":        "555-0000",
				"organization": "Corp",
				"job_title":    "CEO",
				"notes":        "VIP",
			},
			false,
			func(t *testing.T, p *people.Person) {
				if len(p.Names) != 1 {
					t.Error("expected names")
				}
				if len(p.EmailAddresses) != 1 {
					t.Error("expected emails")
				}
				if len(p.PhoneNumbers) != 1 {
					t.Error("expected phones")
				}
				if len(p.Organizations) != 1 {
					t.Error("expected orgs")
				}
				if len(p.Biographies) != 1 {
					t.Error("expected bios")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPersonBody(tt.args)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil person")
			}
			if tt.wantChecks != nil {
				tt.wantChecks(t, got)
			}
		})
	}
}

// --- updatePersonFields ---

func TestContactsUpdatePersonFields(t *testing.T) {
	tests := []struct {
		name          string
		person        *people.Person
		wantFields    []string
		wantNotFields []string
	}{
		{
			"empty person",
			&people.Person{},
			nil,
			[]string{"names", "emailAddresses", "phoneNumbers", "organizations", "biographies", "addresses"},
		},
		{
			"person with names",
			&people.Person{Names: []*people.Name{{GivenName: "John"}}},
			[]string{"names"},
			[]string{"emailAddresses"},
		},
		{
			"person with emails",
			&people.Person{EmailAddresses: []*people.EmailAddress{{Value: "a@b.com"}}},
			[]string{"emailAddresses"},
			[]string{"names"},
		},
		{
			"person with phones",
			&people.Person{PhoneNumbers: []*people.PhoneNumber{{Value: "555"}}},
			[]string{"phoneNumbers"},
			nil,
		},
		{
			"person with organizations",
			&people.Person{Organizations: []*people.Organization{{Name: "Acme"}}},
			[]string{"organizations"},
			nil,
		},
		{
			"person with biographies",
			&people.Person{Biographies: []*people.Biography{{Value: "bio"}}},
			[]string{"biographies"},
			nil,
		},
		{
			"person with addresses",
			&people.Person{Addresses: []*people.Address{{FormattedValue: "123 Main"}}},
			[]string{"addresses"},
			nil,
		},
		{
			"person with all fields",
			&people.Person{
				Names:          []*people.Name{{GivenName: "J"}},
				EmailAddresses: []*people.EmailAddress{{Value: "e"}},
				PhoneNumbers:   []*people.PhoneNumber{{Value: "p"}},
				Organizations:  []*people.Organization{{Name: "o"}},
				Biographies:    []*people.Biography{{Value: "b"}},
				Addresses:      []*people.Address{{FormattedValue: "a"}},
			},
			[]string{"names", "emailAddresses", "phoneNumbers", "organizations", "biographies", "addresses"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updatePersonFields(tt.person)
			for _, f := range tt.wantFields {
				if !strings.Contains(got, f) {
					t.Errorf("updatePersonFields() = %q, want contains %q", got, f)
				}
			}
			for _, f := range tt.wantNotFields {
				if strings.Contains(got, f) {
					t.Errorf("updatePersonFields() = %q, should not contain %q", got, f)
				}
			}
		})
	}
}
