package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"time"
)

type Cache struct {
	LastQueryTimestamp time.Time   `json:"last_query_timestamp"`
	FeedEntries        []FeedEntry `json:"feed_entries"`
}

func getCacheFile() string {
	fileName := path.Join(getConfigDir(), "yt-rss/cache.json")
	return fileName
}

func getFromCache() (entries []FeedEntry, isStale bool, err error) {
	cacheFile := getCacheFile()

	_, err = os.Stat(cacheFile)
	if os.IsNotExist(err) {
		return nil, true, nil
	}

	f, err := os.Open(cacheFile)
	if err != nil {
		return nil, false, err
	}

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, false, err
	}

	cache := &Cache{}
	err = json.Unmarshal(b, cache)
	if err != nil {
		return nil, false, err
	}

	// Check if cache is stale
	if time.Now().Sub(cache.LastQueryTimestamp) > cacheDuration {
		return cache.FeedEntries, true, nil
	}

	// Cache is still valid
	return cache.FeedEntries, false, nil
}

func writeToCache(entries []FeedEntry) (err error) {
	cacheFile := getCacheFile()
	cache := &Cache{
		LastQueryTimestamp: time.Now(),
		FeedEntries:        entries,
	}
	b, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	err = os.WriteFile(cacheFile, b, 0600)
	if err != nil {
		return err
	}
	return nil
}
