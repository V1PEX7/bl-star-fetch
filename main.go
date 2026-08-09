package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

var (
	diffMapping = map[string]int{
		"Easy": 1, "Normal": 3, "Hard": 5, "Expert": 7, "ExpertPlus": 9,
	}

	diffShorthands = map[string]string{
		"e+": "ExpertPlus", "expertplus": "ExpertPlus", "ex+": "ExpertPlus",
		"e": "Expert", "expert": "Expert",
		"h": "Hard", "hard": "Hard",
		"n": "Normal", "normal": "Normal",
		"easy": "Easy", "es": "Easy",
	}

	useColorStdout = true
	useColorStderr = true
)

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
)

type BeatSaverMap struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Metadata struct {
		SongAuthorName  string `json:"songAuthorName"`
		LevelAuthorName string `json:"levelAuthorName"`
	} `json:"metadata"`
	Versions []MapVersion `json:"versions"`
}

type MapDiff struct {
	Characteristic string `json:"characteristic"`
	Difficulty     string `json:"difficulty"`
}

type MapVersion struct {
	Hash        string    `json:"hash"`
	DownloadURL string    `json:"downloadURL"`
	Diffs       []MapDiff `json:"diffs"`
}

type ModifierData struct {
	StarRating         *float64         `json:"star_rating"`
	AccRating          *float64         `json:"acc_rating"`
	LackMapCalculation *LackCalculation `json:"lack_map_calculation"`
	PredictedAcc       *float64         `json:"predicted_acc"`
}

type LackCalculation struct {
	BalancedPassDiff *float64 `json:"balanced_pass_diff"`
	BalancedTech     *float64 `json:"balanced_tech"`
}

type MapDifficulty struct {
	Characteristic string
	Difficulty     string
	Value          int
}

type inputKind int

const (
	kindMapCode       inputKind = iota // a BeatSaver map ID, e.g. "52eb5"
	kindHash                           // a raw 40-char map hash
	kindLeaderboardID                  // a BeatLeader leaderboard ID (needs resolving)
	kindScoreSaberID                   // a ScoreSaber difficulty/leaderboard ID (needs resolving)
)

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

func init() {
	noColor := os.Getenv("NO_COLOR") != ""
	useColorStdout = !noColor && term.IsTerminal(int(os.Stdout.Fd()))
	useColorStderr = !noColor && term.IsTerminal(int(os.Stderr.Fd()))
	flag.Usage = printUsage
}

func printUsage() {
	binName := filepath.Base(os.Args[0])

	fmt.Fprintf(os.Stderr, "%s\n", cErr(colorBold, "BeatLeader Calculation Tool"))
	fmt.Fprintln(os.Stderr, "Fetches star ratings, accuracy ratings, and modifier calculations for Beat Saber maps.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", cErr(colorBold, "USAGE:"))
	fmt.Fprintf(os.Stderr, "  %s [flags] [map ID or URL]\n\n", binName)
	fmt.Fprintf(os.Stderr, "%s\n", cErr(colorBold, "FLAGS:"))
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\n%s\n", cErr(colorBold, "SUPPORTED INPUTS:"))
	fmt.Fprintln(os.Stderr, "  • BeatSaver ID     (e.g. 52eb5 or !bsr 52eb5)")
	fmt.Fprintln(os.Stderr, "  • BeatSaver URL    (e.g. https://beatsaver.com/maps/52eb5)")
	fmt.Fprintln(os.Stderr, "  • BeatLeader URL   (e.g. https://beatleader.com/leaderboard/global/12345)")
	fmt.Fprintln(os.Stderr, "  • ScoreSaber URL   (e.g. https://scoresaber.com/map/12345)")
	fmt.Fprintln(os.Stderr, "  • 40-character Map Hash")
	fmt.Fprintf(os.Stderr, "\n%s\n", cErr(colorBold, "DIFFICULTY SHORTHANDS (-d, -diff):"))
	fmt.Fprintln(os.Stderr, "  • ExpertPlus : e+, ex+, expertplus")
	fmt.Fprintln(os.Stderr, "  • Expert     : e, expert")
	fmt.Fprintln(os.Stderr, "  • Hard       : h, hard")
	fmt.Fprintln(os.Stderr, "  • Normal     : n, normal")
	fmt.Fprintln(os.Stderr, "  • Easy       : es, easy")
	fmt.Fprintf(os.Stderr, "\n%s\n", cErr(colorBold, "EXAMPLES:"))
	fmt.Fprintf(os.Stderr, "  %s -i 52eb5 -d e+\n", binName)
	fmt.Fprintf(os.Stderr, "  %s -id 52eb5 -diff expert\n", binName)
	fmt.Fprintf(os.Stderr, "  %s 52eb5\n", binName)
	fmt.Fprintf(os.Stderr, "  %s https://beatleader.com/leaderboard/global/12345\n\n", binName)
}

func main() {
	var mapCode, diffCode string

	flag.StringVar(&mapCode, "id", "", "BeatSaver ID, hash, or map/leaderboard URL")
	flag.StringVar(&mapCode, "i", "", "Alias for -id")
	flag.StringVar(&diffCode, "diff", "", "Difficulty shorthand (e.g. e+, e, h, n, easy)")
	flag.StringVar(&diffCode, "d", "", "Alias for -diff")
	flag.Parse()

	rawInput := strings.TrimSpace(mapCode)
	if rawInput == "" && flag.NArg() > 0 {
		rawInput = flag.Arg(0)
	}
	if rawInput == "" {
		rawInput = readLine("Enter BeatSaver code, !bsr code, or BeatSaver/BeatLeader/ScoreSaber link: ")
	}

	kind, value := extractMapCode(rawInput)

	var mapInfo *BeatSaverMap
	var err error

	switch kind {
	case kindLeaderboardID:
		fmt.Fprintln(os.Stderr, cErr(colorDim, "Resolving BeatLeader leaderboard..."))
		hash, resolveErr := getBeatLeaderLeaderboardHash(value)
		if resolveErr != nil {
			fatalError("Failed to resolve BeatLeader leaderboard: %v", resolveErr)
		}
		fmt.Fprintln(os.Stderr, cErr(colorDim, "Fetching map details..."))
		mapInfo, err = getBeatSaverMapByHash(hash)
	case kindScoreSaberID:
		fmt.Fprintln(os.Stderr, cErr(colorDim, "Resolving ScoreSaber map..."))
		hash, resolveErr := getScoreSaberHash(value)
		if resolveErr != nil {
			fatalError("Failed to resolve ScoreSaber map: %v", resolveErr)
		}
		fmt.Fprintln(os.Stderr, cErr(colorDim, "Fetching map details..."))
		mapInfo, err = getBeatSaverMapByHash(hash)
	case kindHash:
		fmt.Fprintln(os.Stderr, cErr(colorDim, "Fetching map details..."))
		mapInfo, err = getBeatSaverMapByHash(value)
	default:
		fmt.Fprintln(os.Stderr, cErr(colorDim, "Fetching map details..."))
		mapInfo, err = getBeatSaverMap(value)
	}
	if err != nil {
		fatalError("Failed to query BeatSaver: %v", err)
	}

	if len(mapInfo.Versions) == 0 || mapInfo.Versions[0].DownloadURL == "" {
		fatalError("No valid versions or download URL found for this map.")
	}
	latestVersion := mapInfo.Versions[0]

	availableDiffs := parseAvailableDiffs(latestVersion)
	if len(availableDiffs) == 0 {
		fatalError("No difficulties found for this map.")
	}

	selectedDiff, err := resolveDifficulty(availableDiffs, diffCode)
	if err != nil {
		fatalError("Difficulty selection failed: %v", err)
	}

	fmt.Fprintln(os.Stderr, cErr(colorDim, "Fetching calculations..."))
	blData, err := getBeatLeaderStars(selectedDiff.Characteristic, selectedDiff.Value, latestVersion.DownloadURL)
	if err != nil {
		fatalError("Failed to query BeatLeader: %v", err)
	}

	printResults(mapInfo, latestVersion.Hash, selectedDiff, blData)
}

func extractMapCode(input string) (kind inputKind, value string) {
	input = strings.TrimSpace(input)
	lower := strings.ToLower(input)

	if strings.HasPrefix(lower, "!bsr ") {
		return kindMapCode, strings.TrimSpace(input[len("!bsr "):])
	}

	urlMarkers := []struct {
		marker string
		kind   inputKind
	}{
		{"beatsaver.com/maps/", kindMapCode},
		{"beatleader.com/leaderboard/global/", kindLeaderboardID},
		{"scoresaber.com/map/", kindScoreSaberID},
	}
	for _, um := range urlMarkers {
		if idx := strings.Index(lower, um.marker); idx != -1 {
			return um.kind, firstPathSegment(input[idx+len(um.marker):])
		}
	}

	if looksLikeHash(input) {
		return kindHash, input
	}

	return kindMapCode, input
}

func firstPathSegment(s string) string {
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(s, sep); i != -1 {
			s = s[:i]
		}
	}
	return s
}

func looksLikeHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func fetchJSON[T any](apiURL, notFoundMsg string) (T, error) {
	var zero T

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound && notFoundMsg != "" {
		return zero, fmt.Errorf("%s", notFoundMsg)
	}
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var data T
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return zero, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return data, nil
}

func getBeatSaverMap(mapCode string) (*BeatSaverMap, error) {
	apiURL := fmt.Sprintf("https://api.beatsaver.com/maps/id/%s", url.PathEscape(mapCode))
	m, err := fetchJSON[BeatSaverMap](apiURL, fmt.Sprintf("'%s' not found", mapCode))
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func getBeatSaverMapByHash(hash string) (*BeatSaverMap, error) {
	apiURL := fmt.Sprintf("https://api.beatsaver.com/maps/hash/%s", url.PathEscape(strings.ToLower(hash)))
	m, err := fetchJSON[BeatSaverMap](apiURL, fmt.Sprintf("'%s' not found", hash))
	if err != nil {
		return nil, err
	}
	return &m, nil
}

type beatLeaderLeaderboardResponse struct {
	Song struct {
		Hash string `json:"Hash"`
	} `json:"Song"`
}

func (r beatLeaderLeaderboardResponse) hash() string {
	if looksLikeHash(r.Song.Hash) {
		return r.Song.Hash
	}
	return ""
}

func getBeatLeaderLeaderboardHash(leaderboardID string) (string, error) {
	apiURL := fmt.Sprintf("https://api.beatleader.com/leaderboard/%s", url.PathEscape(leaderboardID))

	resp, err := fetchJSON[beatLeaderLeaderboardResponse](apiURL, fmt.Sprintf("leaderboard '%s' not found", leaderboardID))
	if err != nil {
		return "", err
	}
	hash := resp.hash()
	if hash == "" {
		return "", errors.New("no map hash found in leaderboard response")
	}
	return hash, nil
}

type scoreSaberMapResponse struct {
	Hash string `json:"hash"`
}

func (r scoreSaberMapResponse) hash() string {
	if looksLikeHash(r.Hash) {
		return r.Hash
	}
	return ""
}

func getScoreSaberHash(id string) (string, error) {
	apiURL := fmt.Sprintf("https://scoresaber.com/api/v2/maps/%s", url.PathEscape(id))

	resp, err := fetchJSON[scoreSaberMapResponse](apiURL, fmt.Sprintf("ScoreSaber map/difficulty '%s' not found", id))
	if err != nil {
		return "", err
	}
	hash := resp.hash()
	if hash == "" {
		return "", errors.New("no map hash found in ScoreSaber response")
	}
	return hash, nil
}

func getBeatLeaderStars(characteristic string, diffVal int, zipURL string) (map[string]ModifierData, error) {
	apiURL := fmt.Sprintf("https://stage.api.beatleader.net/ppai2/link/%s/%d", url.PathEscape(characteristic), diffVal)

	reqURL, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("link", zipURL)
	reqURL.RawQuery = q.Encode()

	return fetchJSON[map[string]ModifierData](reqURL.String(), "")
}

func resolveDifficulty(available []MapDifficulty, argDiff string) (*MapDifficulty, error) {
	if argDiff != "" {
		targetDiff := diffShorthands[strings.ToLower(strings.TrimSpace(argDiff))]
		if targetDiff == "" {
			return nil, fmt.Errorf("unknown difficulty shorthand: '%s'", argDiff)
		}

		var matched []MapDifficulty
		for _, d := range available {
			if d.Difficulty == targetDiff {
				matched = append(matched, d)
			}
		}

		if len(matched) == 0 {
			return nil, fmt.Errorf("difficulty '%s' not mapped for this song", targetDiff)
		}

		for _, d := range matched {
			if d.Characteristic == "Standard" {
				return &d, nil
			}
		}
		return &matched[0], nil
	}

	if len(available) == 1 {
		return &available[0], nil
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", cErr(colorBold, "Available Difficulties:"))
	defaultIdx := 1
	for i, opt := range available {
		fmt.Fprintf(os.Stderr, "  %s %s - %s\n", cErr(colorDim, fmt.Sprintf("[%d]", i+1)), opt.Characteristic, opt.Difficulty)
		if opt.Characteristic == "Standard" && opt.Difficulty == "ExpertPlus" {
			defaultIdx = i + 1
		}
	}

	promptMsg := fmt.Sprintf("\nSelect difficulty index (1-%d) [default %d]: ", len(available), defaultIdx)
	selection := readLine(cErr(colorBold, promptMsg))

	selectedIdx := defaultIdx
	if selection != "" {
		parsedIdx, err := strconv.Atoi(selection)
		if err != nil || parsedIdx < 1 || parsedIdx > len(available) {
			fmt.Fprintf(os.Stderr, "%s\n", cErr(colorYellow, "Warning: Invalid input. Using default."))
		} else {
			selectedIdx = parsedIdx
		}
	}

	return &available[selectedIdx-1], nil
}

func printResults(m *BeatSaverMap, hash string, diff *MapDifficulty, blData map[string]ModifierData) {
	if hash == "" {
		hash = "N/A"
	}

	fmt.Println()
	wMeta := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(wMeta, "%s\t%s\n", cOut(colorBold, "Map ID:"), m.ID)
	_, _ = fmt.Fprintf(wMeta, "%s\t%s\n", cOut(colorBold, "Title:"), m.Name)
	_, _ = fmt.Fprintf(wMeta, "%s\t%s\n", cOut(colorBold, "Artist:"), m.Metadata.SongAuthorName)
	_, _ = fmt.Fprintf(wMeta, "%s\t%s\n", cOut(colorBold, "Mapper:"), m.Metadata.LevelAuthorName)
	_, _ = fmt.Fprintf(wMeta, "%s\t%s\n", cOut(colorBold, "Hash:"), hash)
	_, _ = fmt.Fprintf(wMeta, "%s\t%s - %s\n", cOut(colorBold, "Difficulty:"), diff.Characteristic, diff.Difficulty)
	_ = wMeta.Flush()

	fmt.Println()
	wTable := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
	_, _ = fmt.Fprintln(wTable, "MODIFIER\tSTARS\tACC\tPASS\tTECH\tPRED. ACC")

	printed := make(map[string]bool, len(blData))
	knownOrder := []string{"none", "SFS", "FS", "SS"}
	for _, mod := range knownOrder {
		if data, exists := blData[mod]; exists {
			printRow(wTable, mod, data)
			printed[mod] = true
		}
	}

	remaining := make([]string, 0, len(blData))
	for mod := range blData {
		if !printed[mod] {
			remaining = append(remaining, mod)
		}
	}
	sort.Strings(remaining)
	for _, mod := range remaining {
		printRow(wTable, mod, blData[mod])
	}

	_ = wTable.Flush()
	fmt.Println()
}

func printRow(w *tabwriter.Writer, mod string, data ModifierData) {
	displayName := strings.ToUpper(mod)
	var passVal, techVal *float64

	if data.LackMapCalculation != nil {
		passVal = data.LackMapCalculation.BalancedPassDiff
		techVal = data.LackMapCalculation.BalancedTech
	}

	predAccStr := "N/A"
	if data.PredictedAcc != nil {
		scaledAcc := *data.PredictedAcc * 100
		predAccStr = fmtFloat(&scaledAcc, 2, "%")
	}

	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
		displayName,
		fmtFloat(data.StarRating, 2, ""),
		fmtFloat(data.AccRating, 2, ""),
		fmtFloat(passVal, 2, ""),
		fmtFloat(techVal, 2, ""),
		predAccStr,
	)
}

func parseAvailableDiffs(version MapVersion) []MapDifficulty {
	var diffs []MapDifficulty
	seen := make(map[string]bool)

	for _, d := range version.Diffs {
		if d.Characteristic == "" || d.Difficulty == "" {
			continue
		}

		key := d.Characteristic + "||" + d.Difficulty
		if !seen[key] {
			val, exists := diffMapping[d.Difficulty]
			if !exists {
				continue
			}
			diffs = append(diffs, MapDifficulty{
				Characteristic: d.Characteristic,
				Difficulty:     d.Difficulty,
				Value:          val,
			})
			seen[key] = true
		}
	}
	return diffs
}

func readLine(prompt string) string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return fallbackReadLine(prompt)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fallbackReadLine(prompt)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	rw := struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stderr}

	line, err := term.NewTerminal(rw, prompt).ReadLine()
	if err != nil {
		_ = term.Restore(fd, oldState)
		if errors.Is(err, io.EOF) {
			fmt.Println()
			os.Exit(0)
		}
		fatalError("Failed to read input: %v", err)
	}
	return strings.TrimSpace(line)
}

func fallbackReadLine(prompt string) string {
	_, _ = fmt.Fprint(os.Stderr, prompt)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Println()
			os.Exit(0)
		}
		fatalError("Failed to read input: %v", err)
	}
	return strings.TrimSpace(input)
}

func fmtFloat(val *float64, decimals int, suffix string) string {
	if val == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.*f%s", decimals, *val, suffix)
}

func cOut(code, text string) string {
	return colorize(useColorStdout, code, text)
}

func cErr(code, text string) string {
	return colorize(useColorStderr, code, text)
}

func colorize(enabled bool, code, text string) string {
	if !enabled {
		return text
	}
	return code + text + colorReset
}

func fatalError(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(os.Stderr, "%s\n", cErr(colorRed, "Error: "+msg))
	os.Exit(1)
}
