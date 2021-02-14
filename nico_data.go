package main

import (
	"time"
)


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
