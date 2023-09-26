package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	"github.com/pkg/errors"
)

var (
	youtubeDurationRegex = regexp.MustCompile(`<meta itemprop="duration" content="(.+?)">`)

	// This is not a proper ISO8601 parser. I'm only parsing the format
	// typically seen in youtube's HTML.
	iso8601DurationSimplifiedRegex = regexp.MustCompile(`PT(?P<minutes>\d+)M(?P<seconds>\d+)S`)
)

func getVideoDuration(url string) (time.Duration, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	matches := youtubeDurationRegex.FindStringSubmatch(string(b))
	if len(matches) < 2 {
		return 0, errors.New("duration not found")
	}
	durationString := matches[1] // iso8601 duration

	// Parse ISO8601 duration
	matches = iso8601DurationSimplifiedRegex.FindStringSubmatch(durationString)
	if len(matches) < 3 {
		return 0, errors.New("duration not parsed correctly")
	}
	minutes := matches[1]
	seconds := matches[2]
	duration, err := time.ParseDuration(fmt.Sprintf("%sm%ss", minutes, seconds))
	if err != nil {
		return 0, err
	}
	return duration, nil
}
