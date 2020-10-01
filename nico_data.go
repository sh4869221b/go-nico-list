package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

// GetVideoList is aaa
func GetVideoList(userID string) string {

	var resStr string
	var req *http.Request

	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://nvapi.nicovideo.jp/v1/users/%s/videos?sortKey=registeredAt&sortOrder=desc&pageSize=100&page=%d", userID, i+1)
		req, _ = http.NewRequest("GET", url, nil)
		req.Header.Set("X-Frontend-Id", "6")
		client := new(http.Client)

		res, err := client.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		if res.StatusCode != 200 {
			break
		}

		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			log.Fatal(err)
		}

		var nicoData nicoData
		if err := json.Unmarshal(body, &nicoData); err != nil {
			os.Exit(0)
		}
		if len(nicoData.Data.Items) == 0 {
			break
		}
		for _, s := range nicoData.Data.Items {
			resStr += s.ID + "\n"
		}
	}
	return resStr
}

type nicoData struct {
	Meta meta `json:"meta"`
	Data data `json:"data"`
}
type meta struct {
	Status int `json:"status"`
}
type count struct {
	View    int `json:"view"`
	Comment int `json:"comment"`
	Mylist  int `json:"mylist"`
}
type thumbnail struct {
	URL        string `json:"url"`
	MiddleURL  string `json:"middleUrl"`
	LargeURL   string `json:"largeUrl"`
	ListingURL string `json:"listingUrl"`
	NHdURL     string `json:"nHdUrl"`
}
type items struct {
	Type                  string      `json:"type"`
	IsCommunityMemberOnly bool        `json:"isCommunityMemberOnly"`
	ID                    string      `json:"id"`
	Title                 string      `json:"title"`
	RegisteredAt          time.Time   `json:"registeredAt"`
	Count                 count       `json:"count"`
	Thumbnail             thumbnail   `json:"thumbnail"`
	Duration              int         `json:"duration"`
	ShortDescription      string      `json:"shortDescription"`
	LatestCommentSummary  string      `json:"latestCommentSummary"`
	IsChannelVideo        bool        `json:"isChannelVideo"`
	IsPaymentRequired     bool        `json:"isPaymentRequired"`
	PlaybackPosition      interface{} `json:"playbackPosition"`
	Owner                 interface{} `json:"owner"`
	NineD091F87           bool        `json:"9d091f87"`
}
type data struct {
	TotalCount int     `json:"totalCount"`
	Items      []items `json:"items"`
}
