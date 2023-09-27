package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var configFile = getConfigFile()
var hashtagRegex = regexp.MustCompile(`\B(\#[\w_-]+\b)`) // non-word boundary, hashtag, word boundary

// Configuration
var (
	cacheDuration           = 30 * time.Minute
	shortsThreshold         = 80 * time.Second // Duration to consider a video a YouTube Short
	enableAuthorNamePadding = true             // Enables padding of author names to align the FZF output
)

type FeedEntry struct {
	ID        string `xml:"id" json:"id"`
	YTVideoID string `xml:"videoId" json:"yt_video_id"`
	Published string `xml:"published" json:"published"`
	Updated   string `xml:"updated" json:"updated"`
	Author    struct {
		Name string `xml:"name" json:"name"`
	} `xml:"author" json:"author"`
	MediaGroup struct {
		Title   string `xml:"title" json:"title"`
		Content struct {
			URL string `xml:"url,attr" json:"url"`
		} `xml:"content" json:"content"`
	} `xml:"group" json:"media_group"`

	// ExtraMetadata contains metadata not part of YouTube's RSS feed.
	ExtraMetadata struct {
		VideoDuration   time.Duration `json:"video_duration"`
		NormalizedTitle string        `json:"normalized_title"`
	} `json:"extra_metadata"`
}

func (e FeedEntry) GetPublishedDate() time.Time {
	t, _ := time.Parse(time.RFC3339, e.Published)
	return t
}

type Feed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []FeedEntry `xml:"entry"`
}

func getFeeds(feedURLs []string) ([]Feed, error) {
	var feeds []Feed
	concurrency := 10

	progressBar := progressbar.Default(
		int64(len(feedURLs)),
		"Fetching feeds",
	)

	worker := func(wg *sync.WaitGroup, ch <-chan string, errCh chan<- error, progressBar *progressbar.ProgressBar) {
		defer wg.Done()
		for feedURL := range ch {
			feed, err := getFeed(feedURL)
			if err != nil {
				progressBar.Add(1)
				errCh <- err
				return
			}
			feeds = append(feeds, *feed)
			progressBar.Add(1)
		}
	}

	ch := make(chan string, concurrency)
	errCh := make(chan error, len(feedURLs))
	wg := &sync.WaitGroup{}

	// start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(wg, ch, errCh, progressBar)
	}

	// Queue feed URLs
	for _, feedURL := range feedURLs {
		ch <- feedURL
	}
	close(ch)

	// Wait for workers to finish
	wg.Wait()

	// Print errors, if any
	if len(errCh) > 0 {
		for err := range errCh {
			fmt.Fprintln(os.Stderr, err)
		}
	}

	return feeds, nil
}

func getFeed(feedURL string) (*Feed, error) {
	resp, err := http.Get(feedURL)
	if err != nil {
		return nil, errors.Wrap(err, "fetch feed")
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response body")
	}

	feed := &Feed{}
	err = xml.Unmarshal(b, feed)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal response body")
	}

	return feed, nil
}

func getFeedEntries(feeds []Feed, cachedFeedEntries []FeedEntry) []FeedEntry {
	// Create a lookup for entries we've seen before, so we can avoid
	// postprocessing them again later.
	cachedEntryLookup := make(map[string]FeedEntry)
	for _, v := range cachedFeedEntries {
		cachedEntryLookup[v.ID] = v
	}

	// Concat cached and new feed entries. Prioritize cached entries if
	// there are duplicates, because the cached entries have additional
	// metadata fetched already. Fetching metadata can be expensive, so we
	// should avoid doing it where unnecessary.
	var entries []FeedEntry
	seen := make(map[string]bool)
	for _, v := range cachedFeedEntries {
		if _, ok := seen[v.ID]; !ok {
			// Append if entry has not been added before
			seen[v.ID] = true
			entries = append(entries, v)
		}
	}
	for _, feed := range feeds {
		for _, v := range feed.Entries {
			if _, ok := seen[v.ID]; !ok {
				// Append if entry has not been added before
				seen[v.ID] = true
				entries = append(entries, v)
			}
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetPublishedDate().After(entries[j].GetPublishedDate())
	})

	entries = bulkAddMetadata(entries)

	return entries
}

func bulkAddMetadata(entries []FeedEntry) []FeedEntry {
	concurrency := 10
	progressBar := progressbar.Default(
		int64(len(entries)),
		"Adding metadata",
	)

	// Worker to add metadata to each entry.
	// Note that the slice index is being passed instead of a pointer to
	// each struct in the slice. Would prefer to do the latter, but I can't
	// get it to work. This method is less ideal, but it works for now.
	worker := func(wg *sync.WaitGroup, ch <-chan int, errCh chan<- error, progressBar *progressbar.ProgressBar) {
		defer wg.Done()
		for i := range ch {
			addMetadata(&entries[i])
			progressBar.Add(1)
		}
	}

	ch := make(chan int, concurrency)
	errCh := make(chan error, len(entries))
	wg := &sync.WaitGroup{}

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(wg, ch, errCh, progressBar)
	}

	// Queue feed entries
	for i := range entries {
		i := i
		ch <- i
	}
	close(ch)

	// Wait for workers to finish
	wg.Wait()

	// Print errors, if any
	if len(errCh) > 0 {
		for err := range errCh {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	return entries
}

func normalizeTitle(title string) string {
	// Sentence case
	caser := cases.Lower(language.English)
	title = caser.String(title)
	r := []rune(title)
	r[0] = unicode.ToUpper(r[0])
	title = string(r)

	// Remove hashtags
	title = hashtagRegex.ReplaceAllString(title, "")

	return title
}

func addMetadata(entry *FeedEntry) {
	// Add video duration
	if entry.ExtraMetadata.VideoDuration == 0 {
		duration, err := getVideoDuration(entry.MediaGroup.Content.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get video duration for %s\n", entry.MediaGroup.Content.URL)
			return
		}
		entry.ExtraMetadata.VideoDuration = duration
	}

	// Normalize titles
	if entry.ExtraMetadata.NormalizedTitle == "" {
		entry.ExtraMetadata.NormalizedTitle = normalizeTitle(entry.MediaGroup.Title)
	}

	return
}

func shouldFilterOutEntry(entry FeedEntry) bool {
	// Filter out short videos
	if entry.ExtraMetadata.VideoDuration > 0 && entry.ExtraMetadata.VideoDuration < shortsThreshold {
		return true
	}
	return false
}

func findLongestAuthorNameLength(entries []FeedEntry) int {
	var maxLength int
	for _, v := range entries {
		if len(v.Author.Name) > maxLength {
			maxLength = len(v.Author.Name)
		}
	}
	return maxLength
}

// buildFZFContent builds the content to show in a fzf instance. This function
// also returns a map, mapping each line in the fzf content to the
// corresponding feed entry. The map allows the feed entry to be looked up
// based on the selected line.
func buildFZFContent(entries []FeedEntry) (fzfContent string, feedEntryLookup map[string]FeedEntry, err error) {
	authorNameFormatString := "%s"
	if enableAuthorNamePadding {
		maxAuthorNameLength := findLongestAuthorNameLength(entries)
		authorNameFormatString = fmt.Sprintf("%%-%ds", maxAuthorNameLength) // e.g. "%-16s"
	}
	feedEntryLookup = make(map[string]FeedEntry)
	for i, v := range entries {
		if shouldFilterOutEntry(v) {
			continue
		}
		parsedDate, err := time.Parse(time.RFC3339, v.Published)
		if err != nil {
			return "", nil, err
		}
		formattedDate := parsedDate.Format("02 Jan")
		duration := fmt.Sprintf("%02d:%02d", int(v.ExtraMetadata.VideoDuration.Minutes()), int(v.ExtraMetadata.VideoDuration.Seconds())%60)
		authorName := fmt.Sprintf(authorNameFormatString, v.Author.Name)
		title := v.ExtraMetadata.NormalizedTitle
		line := fmt.Sprintf("%s | %s | %s | %s", color.YellowString(formattedDate), color.BlueString(duration), color.GreenString(authorName), title)
		rawLine := fmt.Sprintf("%s | %s | %s | %s", formattedDate, duration, authorName, title)

		feedEntryLookup[rawLine] = v

		fzfContent += line
		if i < len(entries)-1 {
			fzfContent += "\n"
		}
	}
	return fzfContent, feedEntryLookup, nil
}

func selectAndPlay(entries []FeedEntry) error {
	// Get fzf content
	fzfContent, feedEntryLookup, err := buildFZFContent(entries)
	if err != nil {
		return err
	}

	// Select in fzf
	r := strings.NewReader(fzfContent)
	b := &bytes.Buffer{}
	err = runShellCommand("fzf", []string{"--ansi", "--tiebreak=index"}, r, b)
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			// Exit code 2 indicates an unexpected error. Other
			// exit codes are either due to no matches, or
			// user-invoked ctrl-C; both of which can be gracefully
			// ignored.
			if e.ExitCode() == 2 {
				return err
			}
			return nil
		}
		return err
	}

	// Parse selection, play in mpv
	selection := strings.Trim(b.String(), "\n")
	if selection != "" {
		if feedEntry, ok := feedEntryLookup[selection]; ok {
			url := feedEntry.MediaGroup.Content.URL
			fmt.Fprintf(os.Stderr, "Playing %s\n", url)
			err := runShellCommand("mpv", []string{url}, nil, os.Stdout)
			if err != nil {
				return err
			}
		} else {
			return errors.New("url not found for selection")
		}
	}
	return nil
}

func runShellCommand(command string, args []string, r io.Reader, w io.Writer) error {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = w
	cmd.Stdin = r
	return cmd.Run()
}

func getConfigDir() string {
	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		usr, _ := user.Current()
		homeDir := usr.HomeDir
		xdgConfigHome = path.Join(homeDir, ".config/")
	}
	return xdgConfigHome
}

func getConfigFile() string {
	fileName := path.Join(getConfigDir(), "yt-rss/urls")
	// TODO: create dir and file if it does not exist
	return fileName
}

func getFeedURLs() ([]string, error) {
	f, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}

	var feedURLs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#") {
			feedURLs = append(feedURLs, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return feedURLs, nil
}

func main() {
	feedEntries, isStale, err := getFromCache()
	if err != nil {
		log.Fatal(err)
	}
	if isStale {
		feedURLs, err := getFeedURLs()
		if err != nil {
			log.Fatal(err)
		}

		feeds, err := getFeeds(feedURLs)
		if err != nil {
			log.Fatal(err)
		}

		feedEntries = getFeedEntries(feeds, feedEntries)

		err = writeToCache(feedEntries)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Using cached feeds\n")
	}

	err = selectAndPlay(feedEntries)
	if err != nil {
		log.Fatal(err)
	}

}
