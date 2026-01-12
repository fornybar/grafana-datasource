package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "RFC3339Nano",
			input: "2024-01-15T10:30:45.123456789Z",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC),
		},
		{
			name:  "RFC3339",
			input: "2024-01-15T10:30:45Z",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
		},
		{
			name:  "datetime without timezone",
			input: "2024-01-15T10:30:45",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
		},
		{
			name:  "datetime with space and nanoseconds",
			input: "2024-01-15 10:30:45.123456789",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC),
		},
		{
			name:  "datetime with space and microseconds",
			input: "2024-01-15 10:30:45.123456",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 123456000, time.UTC),
		},
		{
			name:  "datetime with space and milliseconds",
			input: "2024-01-15 10:30:45.123",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC),
		},
		{
			name:  "datetime with space",
			input: "2024-01-15 10:30:45",
			want:  time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
		},
		{
			name:  "date only",
			input: "2024-01-15",
			want:  time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid format",
			input:   "not-a-timestamp",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimestamp(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTimestamp(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTimestamp(%q) error = %v", tt.input, err)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseTimestamp(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTimestampColumn(t *testing.T) {
	tests := []struct {
		name   string
		values []any
		want   bool
	}{
		{
			name:   "valid timestamps",
			values: []any{"2024-01-15T10:30:45Z", "2024-01-16T11:00:00Z"},
			want:   true,
		},
		{
			name:   "with nil values",
			values: []any{nil, "2024-01-15T10:30:45Z", nil},
			want:   true,
		},
		{
			name:   "all nil values",
			values: []any{nil, nil, nil},
			want:   false,
		},
		{
			name:   "non-timestamp strings",
			values: []any{"hello", "world"},
			want:   false,
		},
		{
			name:   "numeric values",
			values: []any{1.0, 2.0, 3.0},
			want:   false,
		},
		{
			name:   "empty slice",
			values: []any{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTimestampColumn(tt.values); got != tt.want {
				t.Errorf("isTimestampColumn(%v) = %v, want %v", tt.values, got, tt.want)
			}
		})
	}
}

func TestIsNumericColumn(t *testing.T) {
	tests := []struct {
		name   string
		values []any
		want   bool
	}{
		{
			name:   "all floats",
			values: []any{1.0, 2.5, 3.14},
			want:   true,
		},
		{
			name:   "with nil values",
			values: []any{nil, 1.0, nil, 2.0},
			want:   true,
		},
		{
			name:   "all nil values",
			values: []any{nil, nil},
			want:   true,
		},
		{
			name:   "mixed types",
			values: []any{1.0, "string"},
			want:   false,
		},
		{
			name:   "strings",
			values: []any{"hello", "world"},
			want:   false,
		},
		{
			name:   "empty slice",
			values: []any{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNumericColumn(tt.values); got != tt.want {
				t.Errorf("isNumericColumn(%v) = %v, want %v", tt.values, got, tt.want)
			}
		})
	}
}

func TestCreateField(t *testing.T) {
	t.Run("timestamp field", func(t *testing.T) {
		values := []any{"2024-01-15T10:30:45Z", nil, "2024-01-16T11:00:00Z"}
		field := createField("ts", values)

		if field.Name != "ts" {
			t.Errorf("field.Name = %q, want %q", field.Name, "ts")
		}
		if field.Len() != 3 {
			t.Errorf("field.Len() = %d, want 3", field.Len())
		}
	})

	t.Run("numeric field", func(t *testing.T) {
		values := []any{1.0, nil, 3.14}
		field := createField("value", values)

		if field.Name != "value" {
			t.Errorf("field.Name = %q, want %q", field.Name, "value")
		}
		if field.Len() != 3 {
			t.Errorf("field.Len() = %d, want 3", field.Len())
		}
	})

	t.Run("string field", func(t *testing.T) {
		values := []any{"hello", nil, "world"}
		field := createField("name", values)

		if field.Name != "name" {
			t.Errorf("field.Name = %q, want %q", field.Name, "name")
		}
		if field.Len() != 3 {
			t.Errorf("field.Len() = %d, want 3", field.Len())
		}
	})

	t.Run("empty values", func(t *testing.T) {
		values := []any{}
		field := createField("empty", values)

		if field.Name != "empty" {
			t.Errorf("field.Name = %q, want %q", field.Name, "empty")
		}
		if field.Len() != 0 {
			t.Errorf("field.Len() = %d, want 0", field.Len())
		}
	})
}

func TestParseJSONResponse(t *testing.T) {
	t.Run("single row with mixed types", func(t *testing.T) {
		contents := []byte(`{"ts": "2024-01-15T10:30:45Z", "value": 42.5, "name": "test"}`)
		frame, err := parseJSONResponse(contents)
		if err != nil {
			t.Fatalf("parseJSONResponse() error = %v", err)
		}
		if frame.Name != "response" {
			t.Errorf("frame.Name = %q, want %q", frame.Name, "response")
		}
		if len(frame.Fields) != 3 {
			t.Errorf("len(frame.Fields) = %d, want 3", len(frame.Fields))
		}
	})

	t.Run("multiple rows newline delimited", func(t *testing.T) {
		contents := []byte(`{"id": 1.0, "name": "a"}
{"id": 2.0, "name": "b"}
{"id": 3.0, "name": "c"}`)
		frame, err := parseJSONResponse(contents)
		if err != nil {
			t.Fatalf("parseJSONResponse() error = %v", err)
		}
		if len(frame.Fields) != 2 {
			t.Errorf("len(frame.Fields) = %d, want 2", len(frame.Fields))
		}
		for _, field := range frame.Fields {
			if field.Len() != 3 {
				t.Errorf("field %q has length %d, want 3", field.Name, field.Len())
			}
		}
	})

	t.Run("empty response", func(t *testing.T) {
		contents := []byte(``)
		frame, err := parseJSONResponse(contents)
		if err != nil {
			t.Fatalf("parseJSONResponse() error = %v", err)
		}
		if frame.Name != "response" {
			t.Errorf("frame.Name = %q, want %q", frame.Name, "response")
		}
	})

	t.Run("timestamps converted correctly", func(t *testing.T) {
		contents := []byte(`{"ts": "2024-01-15 10:30:45.123456"}`)
		frame, err := parseJSONResponse(contents)
		if err != nil {
			t.Fatalf("parseJSONResponse() error = %v", err)
		}

		var tsField *struct {
			name string
		}
		for _, f := range frame.Fields {
			if f.Name == "ts" {
				tsField = &struct{ name string }{f.Name}
				break
			}
		}
		if tsField == nil {
			t.Error("expected ts field in frame")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		contents := []byte(`{invalid json}`)
		_, err := parseJSONResponse(contents)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestQueryData(t *testing.T) {
	ds := Datasource{
		baseUrl: "http://localhost:28080",
		pipeline: "blah",
	}

	query := []byte(`{"queryText": "SELECT * FROM v0 where ts BETWEEN $__timeFrom() AND $__timeTo() LIMIT 10"}`)

	resp, err := ds.QueryData(
		context.Background(),
		&backend.QueryDataRequest{
			Queries: []backend.DataQuery{
				{RefID: "A", JSON: query, TimeRange: backend.TimeRange{From: time.Now(), To: time.Now()}},
			},
		},
	)
	if err != nil {
		t.Error(err)
	}

	if len(resp.Responses) != 1 {
		t.Fatal("QueryData must return a response")
	}
}
