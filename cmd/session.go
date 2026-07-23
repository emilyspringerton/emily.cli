// cmd/session.go — emily session new|current
//
// Session fingerprinting for cross-context continuity tracking. Founder
// direction (2026-07-23, logged EMILY/BACKLOG.md S170-11): cheap (no LLM
// calls, no external APIs — everything here is pure deterministic math/
// string manipulation), legible (human-readable, not a raw 64-char hex
// digest), and built from three existing house fingerprint concepts rather
// than invented from scratch:
//
//  1. Emiree's Mandelbrot ASCII fractal signature (ported from
//     EMILY/emily-agent/emiree.go's FractalFingerprint — reads live (h,p)
//     from emiree-state.json if available, defaults to h=p=0.5 otherwise so
//     this never hard-depends on emily-agent being up).
//  2. The squish/tower/gematria transform from
//     QUEENSALLYONLINEBOOKOFMAGIFICATIONANDUNICOR's pemdas.py/hollow.py
//     (squished, MTRXTWER-style tower print, codzeifyWord-style AZ/ZA
//     gematria decimal encoding), run over a session-start seed string.
//  3. A cheap moon-phase read (no ephemeris API/library — full planetary
//     transit calc isn't "cheap") for Dallas, TX, the established
//     astrology-reference location for this org (see the `server-
//     location-dallas` memory convention).
//
// All of it hashes down to a short legible tag: sess-YYYYMMDD-HHMMSS-<8hex>.
// `emily session new` generates + persists; `emily session current`
// retrieves the active tag for tagging Apple bodies/commit messages through
// the rest of that session. Full record appended to var/sessions.ndjson.
package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// RunSession dispatches `emily session <new|current>`.
func RunSession(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily session <new|current>")
		return 1
	}
	switch args[0] {
	case "new":
		return runSessionNew(args[1:])
	case "current":
		return runSessionCurrent(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q — use new or current\n", args[0])
		return 1
	}
}

type sessionRecord struct {
	Tag        string  `json:"tag"`
	StartedAt  string  `json:"started_at"`
	EmireeH    float64 `json:"emiree_h"`
	EmireeP    float64 `json:"emiree_p"`
	Fractal    string  `json:"fractal"`
	Seed       string  `json:"seed"`
	Squished   string  `json:"squished"`
	Tower      string  `json:"tower"`
	GematriaAZ int64   `json:"gematria_az"`
	GematriaZA int64   `json:"gematria_za"`
	MoonPhase  string  `json:"moon_phase"`
	MoonIllum  float64 `json:"moon_illumination"`
}

func runSessionNew(args []string) int {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config resolve: %v\n", err)
		return 1
	}

	now := time.Now().UTC()
	h, p := readEmireeHP(cfg.EmilyRoot)
	fractal := mandelbrotFingerprint(h, p, 40, 14)

	hostname, _ := os.Hostname()
	seed := fmt.Sprintf("%s-%s", now.Format("20060102-150405"), hostname)
	sq := squished(seed)
	tower := towerPrint(sq)
	azDec, zaDec := codzeify(sq, 8)

	phaseName, illum := moonPhase(now)

	sum := sha256.Sum256([]byte(fractal + sq + strconv.FormatInt(azDec, 10) + strconv.FormatInt(zaDec, 10) + phaseName))
	tag := fmt.Sprintf("sess-%s-%s", now.Format("20060102-1504"), hex.EncodeToString(sum[:])[:8])

	rec := sessionRecord{
		Tag:        tag,
		StartedAt:  now.Format(time.RFC3339),
		EmireeH:    h,
		EmireeP:    p,
		Fractal:    fractal,
		Seed:       seed,
		Squished:   sq,
		Tower:      tower,
		GematriaAZ: azDec,
		GematriaZA: zaDec,
		MoonPhase:  phaseName,
		MoonIllum:  illum,
	}

	varDir := filepath.Join(cfg.EmilyRoot, "var")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", varDir, err)
		return 1
	}

	// Append-only session-invocation log.
	logPath := filepath.Join(varDir, "sessions.ndjson")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", logPath, err)
		return 1
	}
	defer logFile.Close()
	line, _ := json.Marshal(rec)
	if _, err := logFile.Write(append(line, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", logPath, err)
		return 1
	}

	// Current-session pointer, for `emily session current`.
	currentPath := filepath.Join(varDir, "current-session.json")
	currentBytes, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(currentPath, currentBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", currentPath, err)
		return 1
	}

	fmt.Println(fractal)
	fmt.Printf("tag:        %s\n", tag)
	fmt.Printf("emiree:     h=%.3f p=%.3f\n", h, p)
	fmt.Printf("tower seed: %s\n", sq)
	fmt.Printf("gematria:   AZ=%d ZA=%d\n", azDec, zaDec)
	fmt.Printf("moon:       %s (%.0f%% illuminated)\n", phaseName, illum*100)
	fmt.Printf("logged:     %s\n", logPath)
	return 0
}

func runSessionCurrent(args []string) int {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config resolve: %v\n", err)
		return 1
	}
	currentPath := filepath.Join(cfg.EmilyRoot, "var", "current-session.json")
	data, err := os.ReadFile(currentPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no current session — run `emily session new` first")
		return 1
	}
	var rec sessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", currentPath, err)
		return 1
	}
	fmt.Println(rec.Tag)
	return 0
}

// currentSessionTag reads the active session tag (if any) for auto-tagging
// Apples/changelog entries. Returns "" if no session has been started —
// callers must fall back gracefully, never fail closed on a missing session.
func currentSessionTag(emilyRoot string) string {
	path := filepath.Join(emilyRoot, "var", "current-session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var rec sessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return ""
	}
	return rec.Tag
}

// readEmireeHP reads the live (h,p) witch-state if available. Defaults to
// 0.5/0.5 (Emiree's neutral cruise-gear point) rather than failing — this
// command must not hard-depend on emily-agent being up.
func readEmireeHP(emilyRoot string) (float64, float64) {
	path := filepath.Join(emilyRoot, "emily-agent", "emily-state", "emiree-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0.5, 0.5
	}
	var s struct {
		H float64 `json:"h"`
		P float64 `json:"p"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return 0.5, 0.5
	}
	return s.H, s.P
}

// mandelbrotFingerprint ports emiree.go's FractalFingerprint algorithm
// verbatim (self-contained copy rather than an import, since emily-agent is
// `package main` in a different module — keeps this command dependency-free
// and cheap).
func mandelbrotFingerprint(h, p float64, width, height int) string {
	if width <= 0 {
		width = 40
	}
	if height <= 0 {
		height = 14
	}
	hFrac := h - math.Floor(h)
	pFrac := p - math.Floor(p)

	c0x, c0y := -0.5, 0.0
	rhoX, rhoY := 0.6, 0.6
	cx := c0x + rhoX*(hFrac-0.5)
	cy := c0y + rhoY*(pFrac-0.5)

	s0 := 2.5
	zeta := 1.0
	sigmoid := 1.0 / (1.0 + math.Exp(-6*((hFrac+pFrac)/2-0.5)))
	s := s0 / (1 + zeta*sigmoid)

	const maxIter = 48
	pal := []byte(" .:-=+*#%@")
	palLen := float64(len(pal) - 1)

	var sb strings.Builder
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			re := cx + (float64(col)-float64(width)/2)/float64(width)*s
			im := cy + (float64(row)-float64(height)/2)/float64(height)*(s*float64(height)/float64(width))

			zr, zi := 0.0, 0.0
			iter := 0
			for iter < maxIter && zr*zr+zi*zi <= 4.0 {
				zr, zi = zr*zr-zi*zi+re, 2*zr*zi+im
				iter++
			}
			idx := int(float64(iter) / float64(maxIter) * palLen)
			if idx >= len(pal) {
				idx = len(pal) - 1
			}
			sb.WriteByte(pal[idx])
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// squished ports pemdas.py's squished(): uppercase, strip non-alphanumeric
// (and non-underscore), collapse consecutive duplicate characters.
func squished(s string) string {
	s = strings.ToUpper(s)
	var sb strings.Builder
	prev := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAllowed := (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
		if isAllowed && c != prev {
			sb.WriteByte(c)
			prev = c
		}
	}
	return sb.String()
}

// towerPrint ports PRINTWR's 3-per-row tower rendering, padding the final
// row with X/XZ the same way the original does.
func towerPrint(s string) string {
	var sb strings.Builder
	level := ""
	for i := 0; i < len(s); i++ {
		level += string(s[i])
		if len(level) == 3 {
			sb.WriteString(level)
			sb.WriteByte('\n')
			level = ""
		}
	}
	switch len(level) {
	case 1:
		sb.WriteString(level + "XZ\n")
	case 2:
		sb.WriteString(level + "X\n")
	case 3:
		sb.WriteString(level + "\n")
	}
	return sb.String()
}

// codzeify ports codzeifyWord's AZ/ZA gematria decimal encoding: split the
// alphabet (forward and reversed) into groupLen round-robin groups, map each
// letter of s to its group index, concatenate the digits, parse as decimal.
func codzeify(s string, groupLen int) (az int64, za int64) {
	const seqAZ = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	seqZA := reverseString(seqAZ)

	azLkp := groupLookup(seqAZ, groupLen)
	zaLkp := groupLookup(seqZA, groupLen)

	azStr := wordToDigits(s, azLkp)
	zaStr := wordToDigits(s, zaLkp)

	az, _ = strconv.ParseInt(orZero(azStr), 10, 64)
	za, _ = strconv.ParseInt(orZero(zaStr), 10, 64)
	return az, za
}

func groupLookup(seq string, groupLen int) map[byte]int {
	lkp := make(map[byte]int, len(seq))
	for i := 0; i < len(seq); i++ {
		lkp[seq[i]] = i % groupLen
	}
	return lkp
}

func wordToDigits(s string, lkp map[byte]int) string {
	su := strings.ToUpper(s)
	var sb strings.Builder
	for i := 0; i < len(su); i++ {
		if d, ok := lkp[su[i]]; ok {
			sb.WriteString(strconv.Itoa(d))
		}
	}
	return sb.String()
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func reverseString(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// moonPhase is a cheap (no ephemeris library/API) synodic-month approximation
// for Dallas, TX — the established astrology-reference location for this
// org (see the server-location-dallas memory convention). Accurate to
// roughly the nearest few hours, which is plenty for a legible fingerprint,
// not an ephemeris-grade transit calc (that wouldn't be "cheap").
func moonPhase(t time.Time) (name string, illumination float64) {
	// Reference new moon: 2000-01-06 18:14 UTC (well-known epoch).
	refNewMoon := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	const synodicMonth = 29.530588861 // days

	daysSince := t.Sub(refNewMoon).Hours() / 24.0
	phase := math.Mod(daysSince, synodicMonth) / synodicMonth
	if phase < 0 {
		phase += 1
	}

	illumination = (1 - math.Cos(2*math.Pi*phase)) / 2

	switch {
	case phase < 0.03 || phase >= 0.97:
		name = "New Moon"
	case phase < 0.22:
		name = "Waxing Crescent"
	case phase < 0.28:
		name = "First Quarter"
	case phase < 0.47:
		name = "Waxing Gibbous"
	case phase < 0.53:
		name = "Full Moon"
	case phase < 0.72:
		name = "Waning Gibbous"
	case phase < 0.78:
		name = "Last Quarter"
	default:
		name = "Waning Crescent"
	}
	return name, illumination
}
