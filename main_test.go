package main

import (
	"reflect"
	"testing"
)

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
	available := []MapDifficulty{
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

func TestParseAvailableDiffs(t *testing.T) {
	version := MapVersion{
		Diffs: []struct {
			Characteristic string `json:"characteristic"`
			Difficulty     string `json:"difficulty"`
		}{
			{Characteristic: "Standard", Difficulty: "Hard"},
			{Characteristic: "Standard", Difficulty: "Expert"},
			{Characteristic: "Standard", Difficulty: "Hard"},         // Duplicate, should be ignored
			{Characteristic: "Lightshow", Difficulty: "UnknownDiff"}, // Unmapped diff, should be ignored
		},
	}

	got := parseAvailableDiffs(version)

	want := []MapDifficulty{
		{Characteristic: "Standard", Difficulty: "Hard", Value: 5},
		{Characteristic: "Standard", Difficulty: "Expert", Value: 7},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseAvailableDiffs() = %v, want %v", got, want)
	}
}
