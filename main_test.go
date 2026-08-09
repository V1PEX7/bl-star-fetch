package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

const testHash = "abcdef1234567890abcdef1234567890abcdef12"

type redirectTransport struct {
	scheme string
	host   string
}

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = rt.scheme, rt.host
	return http.DefaultTransport.RoundTrip(req)
}

func stubHTTP(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	srv := httptest.NewServer(handler)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse stub server URL: %v", err)
	}

	original := httpClient.Transport
	httpClient.Transport = redirectTransport{scheme: target.Scheme, host: target.Host}
	t.Cleanup(func() {
		httpClient.Transport = original
		srv.Close()
	})
}

func TestExtractMapCode(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKind  inputKind
		wantValue string
	}{
		{"bsr command", "!bsr 52eb5", kindMapCode, "52eb5"},
		{"bsr with spaces", " !bsr  abc12 ", kindMapCode, "abc12"},
		{"BeatSaver URL", "https://beatsaver.com/maps/12345", kindMapCode, "12345"},
		{"BeatLeader URL", "https://beatleader.com/leaderboard/global/1234567/1", kindLeaderboardID, "1234567"},
		{"ScoreSaber URL", "https://scoresaber.com/map/98765?difficulty=3", kindScoreSaberID, "98765"},
		{"Raw Hash lowercase", "abcdef1234567890abcdef1234567890abcdef12", kindHash, "abcdef1234567890abcdef1234567890abcdef12"},
		{"Raw Hash uppercase", "ABCDEF1234567890ABCDEF1234567890ABCDEF12", kindHash, "ABCDEF1234567890ABCDEF1234567890ABCDEF12"},
		{"Raw Map Code", "52eb5", kindMapCode, "52eb5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKind, gotValue := extractMapCode(tt.input)
			if gotKind != tt.wantKind {
				t.Errorf("extractMapCode() gotKind = %v, want %v", gotKind, tt.wantKind)
			}
			if gotValue != tt.wantValue {
				t.Errorf("extractMapCode() gotValue = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestFirstPathSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"Clean ID", "12345", "12345"},
		{"With trailing slash", "12345/6789", "12345"},
		{"With query parameters", "12345?diff=1", "12345"},
		{"With anchor tag", "12345#header", "12345"},
		{"Complex URL tail", "12345/user?name=test#hash", "12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstPathSegment(tt.input); got != tt.want {
				t.Errorf("firstPathSegment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLooksLikeHash(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"Valid 40-char hex lowercase", "abcdef1234567890abcdef1234567890abcdef12", true},
		{"Valid 40-char hex uppercase", "ABCDEF1234567890ABCDEF1234567890ABCDEF12", true},
		{"Too short", "abcdef1234567890abcdef1234567890abcdef1", false},
		{"Too long", "abcdef1234567890abcdef1234567890abcdef123", false},
		{"Invalid characters (not hex)", "ghijklmnopqrstuvwxyz12345678901234567890", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeHash(tt.input); got != tt.want {
				t.Errorf("looksLikeHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFmtFloat(t *testing.T) {
	floatPtr := func(f float64) *float64 { return &f }

	tests := []struct {
		name     string
		val      *float64
		decimals int
		suffix   string
		want     string
	}{
		{"Nil value", nil, 2, "%", "N/A"},
		{"Valid float with %", floatPtr(95.678), 2, "%", "95.68%"},
		{"Valid float 0 decimals", floatPtr(12.34), 0, "", "12"},
		{"Valid float precision padding", floatPtr(5.1), 3, "", "5.100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmtFloat(tt.val, tt.decimals, tt.suffix); got != tt.want {
				t.Errorf("fmtFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveDifficulty(t *testing.T) {
	available := []mapDifficulty{
		{Characteristic: "Standard", Difficulty: "Easy", Value: 1},
		{Characteristic: "Standard", Difficulty: "ExpertPlus", Value: 9},
		{Characteristic: "Lawless", Difficulty: "ExpertPlus", Value: 9},
	}

	t.Run("Resolve shorthand Ex+", func(t *testing.T) {
		got, err := resolveDifficulty(available, "ex+")
		if err != nil {
			t.Fatalf("resolveDifficulty() unexpected error: %v", err)
		}
		if got.Characteristic != "Standard" || got.Difficulty != "ExpertPlus" {
			t.Errorf("resolveDifficulty() = %v, want Standard/ExpertPlus", got)
		}
	})

	t.Run("Unknown shorthand", func(t *testing.T) {
		_, err := resolveDifficulty(available, "fake")
		if err == nil {
			t.Fatal("resolveDifficulty() expected error for unknown shorthand, got nil")
		}
	})

	t.Run("Difficulty mapped but missing in song", func(t *testing.T) {
		_, err := resolveDifficulty(available, "n") // Normal
		if err == nil {
			t.Fatal("resolveDifficulty() expected error for unmapped difficulty, got nil")
		}
	})
}

func stubStdin(t *testing.T, content string) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatalf("failed to write stub stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stub stdin writer: %v", err)
	}

	original := os.Stdin
	os.Stdin, stdinReader = r, nil
	t.Cleanup(func() {
		os.Stdin, stdinReader = original, nil
		_ = r.Close()
	})
}

func TestFallbackReadLineConsecutivePrompts(t *testing.T) {
	stubStdin(t, "52eb5\n1\n")

	for _, want := range []string{"52eb5", "1"} {
		got, ok := fallbackReadLine("")
		if !ok {
			t.Fatalf("fallbackReadLine() ok = false, want the buffered line %q", want)
		}
		if got != want {
			t.Errorf("fallbackReadLine() = %q, want %q", got, want)
		}
	}

	if _, ok := fallbackReadLine(""); ok {
		t.Error("fallbackReadLine() ok = true after input was exhausted, want false")
	}
}

func TestResolveDifficultyPrompt(t *testing.T) {
	available := []mapDifficulty{
		{Characteristic: "Standard", Difficulty: "Easy", Value: 1},
		{Characteristic: "Standard", Difficulty: "ExpertPlus", Value: 9},
	}

	tests := []struct {
		name  string
		stdin string
		want  mapDifficulty
	}{
		{"Explicit selection", "1\n", available[0]},
		{"Selection without trailing newline", "1", available[0]},
		{"Empty line uses default", "\n", available[1]},
		{"EOF uses default", "", available[1]},
		{"Invalid input uses default", "banana\n", available[1]},
		{"Out of range uses default", "99\n", available[1]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubStdin(t, tt.stdin)

			got, err := resolveDifficulty(available, "")
			if err != nil {
				t.Fatalf("resolveDifficulty() unexpected error: %v", err)
			}
			if *got != tt.want {
				t.Errorf("resolveDifficulty() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestParseAvailableDiffs(t *testing.T) {
	version := mapVersion{
		Diffs: []mapDiff{
			{Characteristic: "Standard", Difficulty: "Hard"},
			{Characteristic: "Standard", Difficulty: "Expert"},
			{Characteristic: "Standard", Difficulty: "Hard"},         // Duplicate, should be ignored
			{Characteristic: "Lightshow", Difficulty: "UnknownDiff"}, // Unmapped diff, should be ignored
		},
	}

	got := parseAvailableDiffs(version)

	want := []mapDifficulty{
		{Characteristic: "Standard", Difficulty: "Hard", Value: 5},
		{Characteristic: "Standard", Difficulty: "Expert", Value: 7},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseAvailableDiffs() = %v, want %v", got, want)
	}
}

func TestValidateHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		want    string
		wantErr bool
	}{
		{"Valid hash", testHash, testHash, false},
		{"Empty hash", "", "", true},
		{"Truncated hash", testHash[:20], "", true},
		{"Non-hex hash", strings.Repeat("z", 40), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateHash(tt.hash, "BeatLeader")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateHash() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("validateHash() = %v, want %v", got, tt.want)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "BeatLeader") {
				t.Errorf("validateHash() error = %v, want it to name the source", err)
			}
		})
	}
}

func TestGetBeatSaverMap(t *testing.T) {
	tests := []struct {
		name     string
		kind     inputKind
		value    string
		wantPath string
	}{
		{"ID route", kindMapCode, "52eb5", "/maps/id/52eb5"},
		{"Hash route", kindHash, testHash, "/maps/hash/" + testHash},
		{"Hash route lowercases", kindHash, strings.ToUpper(testHash), "/maps/hash/" + testHash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			stubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = io.WriteString(w, `{"id":"52eb5","name":"Song"}`)
			})

			m, err := getBeatSaverMap(tt.kind, tt.value)
			if err != nil {
				t.Fatalf("getBeatSaverMap() unexpected error: %v", err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("getBeatSaverMap() requested %v, want %v", gotPath, tt.wantPath)
			}
			if m.ID != "52eb5" || m.Name != "Song" {
				t.Errorf("getBeatSaverMap() = %+v, want ID 52eb5 and name Song", m)
			}
		})
	}

	t.Run("Not found", func(t *testing.T) {
		stubHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

		_, err := getBeatSaverMap(kindMapCode, "nope")
		if err == nil {
			t.Fatal("getBeatSaverMap() expected error for missing map, got nil")
		}
		if err.Error() != "'nope' not found" {
			t.Errorf("getBeatSaverMap() error = %v, want \"'nope' not found\"", err)
		}
	})
}

func stubBeatSaverRoutes(t *testing.T, gotPath *string) {
	t.Helper()

	stubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/maps/"):
			*gotPath = r.URL.Path
			_, _ = io.WriteString(w, `{"id":"52eb5"}`)
		case strings.HasPrefix(r.URL.Path, "/leaderboard/"):
			_, _ = io.WriteString(w, `{"Song":{"Hash":"`+testHash+`"}}`)
		case strings.HasPrefix(r.URL.Path, "/api/v2/maps/"):
			_, _ = io.WriteString(w, `{"hash":"`+testHash+`"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestFetchMap(t *testing.T) {
	tests := []struct {
		name     string
		kind     inputKind
		value    string
		wantPath string
	}{
		{"Map code", kindMapCode, "52eb5", "/maps/id/52eb5"},
		{"Hash", kindHash, strings.ToUpper(testHash), "/maps/hash/" + testHash},
		{"BeatLeader leaderboard", kindLeaderboardID, "1234567", "/maps/hash/" + testHash},
		{"ScoreSaber map", kindScoreSaberID, "98765", "/maps/hash/" + testHash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			stubBeatSaverRoutes(t, &gotPath)

			m, err := fetchMap(tt.kind, tt.value)
			if err != nil {
				t.Fatalf("fetchMap() unexpected error: %v", err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("fetchMap() requested %v, want %v", gotPath, tt.wantPath)
			}
			if m.ID != "52eb5" {
				t.Errorf("fetchMap() = %+v, want ID 52eb5", m)
			}
		})
	}
}

func TestFetchMapErrors(t *testing.T) {
	tests := []struct {
		name     string
		kind     inputKind
		handler  http.HandlerFunc
		wantWrap string
	}{
		{
			"Unresolvable leaderboard",
			kindLeaderboardID,
			func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"Song":{"Hash":"not-a-hash"}}`)
			},
			"failed to resolve BeatLeader leaderboard",
		},
		{
			"Unresolvable ScoreSaber map",
			kindScoreSaberID,
			func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"hash":""}`)
			},
			"failed to resolve ScoreSaber map",
		},
		{
			"BeatSaver failure",
			kindMapCode,
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			"failed to query BeatSaver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubHTTP(t, tt.handler)

			_, err := fetchMap(tt.kind, "12345")
			if err == nil {
				t.Fatal("fetchMap() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantWrap) {
				t.Errorf("fetchMap() error = %v, want it to contain %v", err, tt.wantWrap)
			}
		})
	}
}
