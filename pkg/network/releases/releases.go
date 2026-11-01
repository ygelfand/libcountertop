package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	httpfetch "github.com/ygelfand/libcountertop/pkg/fetch"
)

const (
	fetchTimeout = 10 * time.Second
	maxFeed      = 1 << 20
	maxList      = 4 << 20
	manifest     = "manifest.json"
	perKind      = 10
)

var version = regexp.MustCompile(`^\d+\.\d+\.\d+`)

type Release struct {
	Tag        string
	Prerelease bool
}

// The releases.atom feed lists the ten newest releases and bare tags, prereleases included, newest first.
func Newest(ctx context.Context, repo string) (string, error) {
	var tag string
	err := fetch(
		ctx,
		strings.TrimSuffix(repo, "/")+"/releases.atom",
		maxFeed,
		func(r io.Reader) error {
			var err error
			tag, err = Parse(r)
			return err
		},
	)
	return tag, err
}

func Parse(r io.Reader) (string, error) {
	var feed struct {
		Entries []struct {
			Link struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return "", fmt.Errorf("releases: reading the feed: %w", err)
	}
	for _, e := range feed.Entries {
		_, tag, _ := strings.Cut(e.Link.Href, "/releases/tag/")
		if version.MatchString(tag) {
			return tag, nil
		}
	}
	return "", errors.New("releases: the feed lists no release")
}

// The GitHub REST API allows 60 unauthenticated requests an hour per address.
func List(ctx context.Context, repo string) ([]Release, error) {
	path := strings.TrimPrefix(strings.TrimSuffix(repo, "/"), "https://github.com/")
	if strings.Count(path, "/") != 1 {
		return nil, fmt.Errorf("releases: %q is not a GitHub repository", repo)
	}
	var out []Release
	err := fetch(
		ctx,
		"https://api.github.com/repos/"+path+"/releases?per_page=100",
		maxList,
		func(r io.Reader) error {
			var err error
			out, err = ParseList(r, perKind)
			return err
		},
	)
	return out, err
}

type listed struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
}

func ParseList(r io.Reader, keep int) ([]Release, error) {
	var all []listed
	if err := json.NewDecoder(r).Decode(&all); err != nil {
		return nil, fmt.Errorf("releases: reading the release list: %w", err)
	}
	var out []Release
	kept := map[bool]int{}
	for _, l := range all {
		if l.Draft || kept[l.Prerelease] >= keep || !version.MatchString(l.Tag) ||
			!slices.Contains(l.Assets, asset{Name: manifest}) {
			continue
		}
		kept[l.Prerelease]++
		out = append(out, Release{Tag: l.Tag, Prerelease: l.Prerelease})
	}
	return out, nil
}

func Manifest(repo, tag string) string { return Asset(repo, tag, manifest) }

func Asset(repo, tag, name string) string {
	return strings.TrimSuffix(repo, "/") + "/releases/download/" + tag + "/" + name
}

var files string

var ErrNoFiles = errors.New("releases: this build has no file host")

func Hosts() []string {
	out := []string{"github.com", "api.github.com"}
	if u, err := url.Parse(files); err == nil && u.Hostname() != "" {
		out = append(out, u.Hostname())
	}
	return out
}

func File(folder, name string) (string, error) {
	if files == "" {
		return "", ErrNoFiles
	}
	return strings.TrimSuffix(files, "/") + "/" + folder + "/" + name, nil
}

func Download(ctx context.Context, url, sum, dest string, limit int64) error {
	return DownloadProgress(ctx, url, sum, dest, limit, nil)
}

type counter struct {
	done   int64
	report func(done int64)
}

func (c *counter) Write(p []byte) (int, error) {
	c.done += int64(len(p))
	if c.report != nil {
		c.report(c.done)
	}
	return len(p), nil
}

func DownloadProgress(
	ctx context.Context,
	url, sum, dest string,
	limit int64,
	report func(done int64),
) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".*")
	if err != nil {
		return fmt.Errorf("releases: %w", err)
	}
	defer os.Remove(tmp.Name())
	err = Stream(ctx, url, sum, limit, report, func(r io.Reader) error {
		_, err := io.Copy(tmp, r)
		return err
	})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

func Stream(
	ctx context.Context,
	url, sum string,
	limit int64,
	report func(done int64),
	read func(io.Reader) error,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpfetch.Client(0).Do(req)
	if err != nil {
		return fmt.Errorf("releases: fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("releases: fetching %s: %s", url, resp.Status)
	}
	h := sha256.New()
	seen := &counter{report: report}
	body := io.TeeReader(io.LimitReader(resp.Body, limit+1), io.MultiWriter(h, seen))
	if err := read(body); err != nil {
		return fmt.Errorf("releases: fetching %s: %w", url, err)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return fmt.Errorf("releases: fetching %s: %w", url, err)
	}
	if seen.done > limit {
		return fmt.Errorf("releases: %s is larger than %d bytes", url, limit)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("releases: %s has sha256 %s, want %s", url, got, sum)
	}
	return nil
}

func fetch(ctx context.Context, url string, limit int64, read func(io.Reader) error) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpfetch.Client(0).Do(req)
	if err != nil {
		return fmt.Errorf("releases: fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("releases: fetching %s: %s", url, resp.Status)
	}
	return read(io.LimitReader(resp.Body, limit))
}
