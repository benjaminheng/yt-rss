package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/pkg/errors"
)

var feeds = []string{
	"https://www.youtube.com/feeds/videos.xml?channel_id=UCBa659QWEk1AI4Tg--mrJ2A",
}

type Feed struct {
	XMLName xml.Name `xml:"feed"`
	Title   string   `xml:"title"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Entries []struct {
		ID         string `xml:"id"`
		YTVideoID  string `xml:"videoId"`
		Published  string `xml:"published"`
		Updated    string `xml:"updated"`
		MediaGroup struct {
			Title   string `xml:"title"`
			Content struct {
				URL string `xml:"url,attr"`
			} `xml:"content"`
		} `xml:"group"`
	} `xml:"entry"`
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

func main() {
	feed, err := getFeed(feeds[0])
	if err != nil {
		log.Fatal(err)
	}
	b, _ := json.Marshal(feed)
	fmt.Println(string(b))
}
