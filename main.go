package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/pkg/errors"
)

var feeds = []string{
	"https://www.youtube.com/feeds/videos.xml?channel_id=UCBa659QWEk1AI4Tg--mrJ2A",
}

type FeedEntry struct {
	ID        string `xml:"id"`
	YTVideoID string `xml:"videoId"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	MediaGroup struct {
		Title   string `xml:"title"`
		Content struct {
			URL string `xml:"url,attr"`
		} `xml:"content"`
	} `xml:"group"`
}

type Feed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []FeedEntry `xml:"entry"`
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

// buildFZFContent builds the content to show in a fzf instance. This function
// also returns a map, mapping each line in the fzf content to the
// corresponding feed entry. The map allows the feed entry to be looked up
// based on the selected line.
func buildFZFContent(entries []FeedEntry) (fzfContent string, feedEntryLookup map[string]FeedEntry, err error) {
	feedEntryLookup = make(map[string]FeedEntry)
	for i, v := range entries {
		parsedDate, err := time.Parse(time.RFC3339, v.Published)
		if err != nil {
			return "", nil, err
		}
		formattedDate := parsedDate.Format("02 Jan")
		line := fmt.Sprintf("[%s] [%s] %s", color.YellowString(formattedDate), color.GreenString(v.Author.Name), v.MediaGroup.Title)
		rawLine := fmt.Sprintf("[%s] [%s] %s", formattedDate, v.Author.Name, v.MediaGroup.Title)

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
	err = runShellCommand("fzf", []string{"--ansi"}, r, b)
	if err != nil {
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

func main() {
	feed, err := getFeed(feeds[0])
	if err != nil {
		log.Fatal(err)
	}
	err = selectAndPlay(feed.Entries)
	if err != nil {
		log.Fatal(err)
	}
}
