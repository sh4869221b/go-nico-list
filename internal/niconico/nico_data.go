package niconico

import (
	"time"
)

// NicoData represents the niconico API response payload.
type NicoData struct {
	Meta struct {
		Status int `json:"status"`
	} `json:"meta"`
	Data struct {
		TotalCount int `json:"totalCount"`
		Items      []struct {
			Series struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
				Order int    `json:"order"`
			} `json:"series"`
			Essential struct {
				Type         string    `json:"type"`
				ID           string    `json:"id"`
				Title        string    `json:"title"`
				RegisteredAt time.Time `json:"registeredAt"`
				Count        struct {
					View    int `json:"view"`
					Comment int `json:"comment"`
					Mylist  int `json:"mylist"`
					Like    int `json:"like"`
				} `json:"count"`
				Thumbnail struct {
					URL        string `json:"url"`
					MiddleURL  string `json:"middleUrl"`
					LargeURL   string `json:"largeUrl"`
					ListingURL string `json:"listingUrl"`
					NHdURL     string `json:"nHdUrl"`
				} `json:"thumbnail"`
				Duration             int    `json:"duration"`
				ShortDescription     string `json:"shortDescription"`
				LatestCommentSummary string `json:"latestCommentSummary"`
				IsChannelVideo       bool   `json:"isChannelVideo"`
				IsPaymentRequired    bool   `json:"isPaymentRequired"`
				PlaybackPosition     any    `json:"playbackPosition"`
				Owner                struct {
					OwnerType string `json:"ownerType"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					IconURL   string `json:"iconUrl"`
				} `json:"owner"`
				RequireSensitiveMasking bool `json:"requireSensitiveMasking"`
				VideoLive               any  `json:"videoLive"`
				NineD091F87             bool `json:"9d091f87"`
				Acf68865                bool `json:"acf68865"`
			} `json:"essential"`
		} `json:"items"`
	} `json:"data"`
}
