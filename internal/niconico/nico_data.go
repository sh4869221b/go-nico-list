package niconico

import (
	"encoding/json/v2"
	"sync"
	"time"
)

// NicoData represents the niconico API response payload.
type NicoData struct {
	Meta struct {
		Status int `json:"status"`
	} `json:"meta"`
	Data struct {
		TotalCount *int            `json:"totalCount"`
		Items      []nicoVideoItem `json:"items"`
	} `json:"data"`
}

// nicoVideoItem represents an item in the user video response.
type nicoVideoItem = struct {
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
}

// mylistVideoItem holds the fields decoded from each mylist item.
type mylistVideoItem = struct {
	Video struct {
		ID           string    `json:"id"`
		RegisteredAt time.Time `json:"registeredAt"`
		Count        struct {
			Comment int `json:"comment"`
		} `json:"count"`
	} `json:"video"`
}

// videoItem holds the common fields needed to filter user and mylist entries.
type videoItem struct {
	ID           string
	CommentCount int
	RegisteredAt time.Time
}

// pageItemPool reuses fixed-size arrays for decoding and extracting page items.
type pageItemPool[T any] struct {
	items sync.Pool
}

// get returns an empty page array with room for pageSize entries.
func (p *pageItemPool[T]) get() *[pageSize]T {
	if items := p.items.Get(); items != nil {
		return items.(*[pageSize]T)
	}
	return new([pageSize]T)
}

// put drops item references before returning the original array to the pool.
func (p *pageItemPool[T]) put(items *[pageSize]T) {
	// Decoding can leave references in this array even if it fails or grows
	// the slice onto a different backing array.
	clear(items[:])
	p.items.Put(items)
}

// minPooledPageBytes keeps short responses off the pooled allocation path.
const minPooledPageBytes = 8 << 10

var userPageItemPool pageItemPool[nicoVideoItem]
var mylistPageItemPool pageItemPool[mylistVideoItem]
var projectedPageItemPool pageItemPool[videoItem]

// makePageItems allocates space for projected items, reusing arrays for full pages.
func makePageItems(count int) []videoItem {
	if count == pageSize {
		return projectedPageItemPool.get()[:]
	}
	return make([]videoItem, count)
}

// parseUserVideoPage extracts video items and pagination metadata from a user response.
func parseUserVideoPage(body []byte) (parsedPage, error) {
	var nicoData NicoData
	// Large responses often contain a full page. Reuse space for up to the
	// requested page size and let the decoder grow it for longer responses.
	if len(body) >= minPooledPageBytes {
		items := userPageItemPool.get()
		defer userPageItemPool.put(items)
		nicoData.Data.Items = items[:0]
	}
	if err := json.Unmarshal(body, &nicoData); err != nil {
		return parsedPage{}, err
	}
	items := makePageItems(len(nicoData.Data.Items))
	for i := range nicoData.Data.Items {
		item := &nicoData.Data.Items[i].Essential
		items[i] = videoItem{
			ID:           item.ID,
			CommentCount: item.Count.Comment,
			RegisteredAt: item.RegisteredAt,
		}
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
				TotalCount     *int              `json:"totalCount"`
				TotalItemCount *int              `json:"totalItemCount"`
				Items          []mylistVideoItem `json:"items"`
			} `json:"mylist"`
		} `json:"data"`
	}
	if len(body) >= minPooledPageBytes {
		items := mylistPageItemPool.get()
		defer mylistPageItemPool.put(items)
		payload.Data.Mylist.Items = items[:0]
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return parsedPage{}, err
	}
	items := makePageItems(len(payload.Data.Mylist.Items))
	for i, item := range payload.Data.Mylist.Items {
		items[i] = videoItem{
			ID:           item.Video.ID,
			CommentCount: item.Video.Count.Comment,
			RegisteredAt: item.Video.RegisteredAt,
		}
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
