// Package update checks GitHub Releases for a newer Agent X build so the app
// can surface a non-blocking "update available" prompt. The check is best
// effort: any network, rate-limit, or parse error is reported as "no update"
// rather than an error the UI has to handle.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultEndpoint is GitHub's public latest-release API. Unauthenticated
	// requests are rate-limited per IP (60/hour) — ample for a once-per-launch
	// version check.
	defaultEndpoint = "https://api.github.com/repos/amantech90/agentx/releases/latest"
	requestTimeout  = 6 * time.Second
	maxBodyBytes    = 1 << 20
	maxNotesLength  = 400
)

// Info is the result of a release check.
type Info struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Notes     string `json:"notes"`
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Checker queries a releases endpoint and compares versions.
type Checker struct {
	endpoint string
	client   *http.Client
}

// NewChecker returns a Checker pointed at the public Agent X releases feed.
func NewChecker() *Checker {
	return &Checker{
		endpoint: defaultEndpoint,
		client:   &http.Client{Timeout: requestTimeout},
	}
}

// Check fetches the latest published release and compares it to current. It
// never returns Available=true on error; callers can ignore the error and use
// the returned Info directly.
func (c *Checker) Check(ctx context.Context, current string) (Info, error) {
	info := Info{Current: normalize(current)}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return info, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "AgentX-update-check")

	response, err := c.client.Do(request)
	if err != nil {
		return info, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return info, fmt.Errorf("release check returned status %d", response.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBodyBytes)).Decode(&release); err != nil {
		return info, err
	}
	if release.Draft || release.Prerelease {
		return info, nil
	}

	info.Latest = normalize(release.TagName)
	info.URL = strings.TrimSpace(release.HTMLURL)
	info.Notes = truncateNotes(release.Body)
	info.Available = info.Latest != "" && compare(info.Latest, info.Current) > 0
	return info, nil
}

// normalize strips a leading v/V and any pre-release or build metadata so
// "v1.2.0-beta+ci" becomes "1.2.0".
func normalize(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	if index := strings.IndexAny(version, "-+"); index >= 0 {
		version = version[:index]
	}
	return strings.TrimSpace(version)
}

// compare returns 1 if a > b, -1 if a < b, and 0 if equal, comparing dotted
// numeric version components. Missing or non-numeric components count as 0.
func compare(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	length := len(aParts)
	if len(bParts) > length {
		length = len(bParts)
	}
	for i := 0; i < length; i++ {
		av, bv := part(aParts, i), part(bParts, i)
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

func part(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(parts[index]))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func truncateNotes(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= maxNotesLength {
		return body
	}
	return strings.TrimSpace(body[:maxNotesLength]) + "…"
}
