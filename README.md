# bl-star-fetch

A command-line tool to look up BeatLeader star ratings, modifier calculations, and predicted accuracy for Beat Saber maps (including unranked maps).

## About

While ranked maps already display star ratings on BeatLeader, checking star ratings for **unranked maps** normally requires a Patreon subscription or manually querying staging tools. This utility allows you to look up BeatLeader star ratings and modifier calculations for **any map, whether it is ranked or unranked.**

I built this project to:

- Easily check star ratings and modifier metrics in one place.
- Avoid manually querying API endpoints or navigating web tools.
- Learn Go and GitHub Actions workflows.

### External API Usage

This tool makes HTTP requests to:

- **BeatSaver API** (`api.beatsaver.com`) to fetch map metadata and zip files.
- **BeatLeader API** (`api.beatleader.com` & `stage.api.beatleader.net`) to resolve leaderboard links and compute modifier star ratings.
- **ScoreSaber API** (`scoresaber.com/api`) to resolve ScoreSaber map links into map hashes.

## Features

- Calculates star ratings, pass/tech difficulty, and predicted accuracy for standard speed modifiers (`SFS`, `FS`, `SS`).
- Accepts BeatSaver codes (`49124`), `!bsr` strings, map hashes, or direct links (BeatSaver, BeatLeader, ScoreSaber).
- Supports difficulty selection via flags (`-diff e`, `-d e+`) or an interactive prompt.

## Installation

Download a pre-compiled binary from the [Releases](https://github.com/V1PEX7/bl-star-fetch/releases) page.

Or build from source (requires Go 1.22+):

```bash
git clone https://github.com/V1PEX7/bl-star-fetch.git
cd bl-star-fetch
go build -o bl-star-fetch .
```

## Usage

Pass a map ID, link, hash, or `!bsr` command:

```bash
./bl-star-fetch 49124
./bl-star-fetch "!bsr 49124"
./bl-star-fetch https://beatleader.com/leaderboard/global/123456/1
```

Specify a difficulty shorthand to skip the prompt:

```bash
./bl-star-fetch -id 49124 -diff e+
# OR
./bl-star-fetch -i 49124 -d e+
```

## Example Output

```text
Map ID:      49124
Title:       Astra Sound Team - Operation: ANNIHILATION
Artist:      Astra Sound Team
Mapper:      SMT
Hash:        80c5be9508778cccb85539092e761a3a8cb26ec6
Difficulty:  Standard - ExpertPlus

MODIFIER    STARS    ACC      PASS     TECH    PRED. ACC
NONE        10.42    11.19    6.94     8.23    97.26%
SFS         13.71    12.65    11.59    9.46    96.61%
FS          11.67    11.88    8.57     8.60    96.99%
SS          9.36     10.54    5.70     7.77    97.48%
```
