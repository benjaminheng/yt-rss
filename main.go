package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

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

func printFeedEntries(entries []FeedEntry) error {
	for _, v := range entries {
		parsedDate, err := time.Parse(time.RFC3339, v.Published)
		if err != nil {
			return err
		}
		formattedDate := parsedDate.Format("02 Jan")
		line := fmt.Sprintf("[%s] [%s] %s", formattedDate, v.Author.Name, v.MediaGroup.Title)
		fmt.Println(line)
	}
	return nil
}

func main() {
	feed, err := getFeed(feeds[0])
	if err != nil {
		log.Fatal(err)
	}
	// b, _ := json.Marshal(feed)
	// fmt.Println(string(b))
	printFeedEntries(feed.Entries)
}
