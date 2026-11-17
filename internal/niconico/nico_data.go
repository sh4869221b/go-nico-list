package niconico

import (
	"encoding/json/v2"
	"time"
)

// NicoData represents the niconico API response payload.
type NicoData struct {
	Meta struct {
		Status int `json:"status"`
	} `json:"meta"`
	Data struct {
		TotalCount *int `json:"totalCount"`
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

// videoItem holds the common fields needed to filter user and mylist entries.
type videoItem struct {
	ID           string
	CommentCount int
	RegisteredAt time.Time
}

// parseUserVideoPage extracts video items and pagination metadata from a user response.
func parseUserVideoPage(body []byte) (parsedPage, error) {
	var nicoData NicoData
	if err := json.Unmarshal(body, &nicoData); err != nil {
		return parsedPage{}, err
	}
	items := make([]videoItem, 0, len(nicoData.Data.Items))
	for _, item := range nicoData.Data.Items {
		items = append(items, videoItem{
			ID:           item.Essential.ID,
			CommentCount: item.Essential.Count.Comment,
			RegisteredAt: item.Essential.RegisteredAt,
		})
	}
	return parsedPage{
		Items:      items,
		Status:     nicoData.Meta.Status,
		TotalCount: nicoData.Data.TotalCount,
	}, nil
}

// parseMylistPage extracts videos and resolves the alternative total-count fields in a mylist response.
func parseMylistPage(body []byte) (parsedPage, error) {
	var payload struct {
		Meta struct {
			Status int `json:"status"`
		} `json:"meta"`
		Data struct {
			TotalCount *int `json:"totalCount"`
			Mylist     struct {
				TotalCount     *int `json:"totalCount"`
				TotalItemCount *int `json:"totalItemCount"`
				Items          []struct {
					Video struct {
						ID           string    `json:"id"`
						RegisteredAt time.Time `json:"registeredAt"`
						Count        struct {
							Comment int `json:"comment"`
						} `json:"count"`
					} `json:"video"`
				} `json:"items"`
			} `json:"mylist"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return parsedPage{}, err
	}
	items := make([]videoItem, 0, len(payload.Data.Mylist.Items))
	for _, item := range payload.Data.Mylist.Items {
		items = append(items, videoItem{
			ID:           item.Video.ID,
			CommentCount: item.Video.Count.Comment,
			RegisteredAt: item.Video.RegisteredAt,
		})
	}
	totalCount := payload.Data.Mylist.TotalCount
	if totalCount == nil {
		totalCount = payload.Data.Mylist.TotalItemCount
	}
	if totalCount == nil {
		totalCount = payload.Data.TotalCount
	}
	return parsedPage{
		Items:      items,
		Status:     payload.Meta.Status,
		TotalCount: totalCount,
	}, nil
}
