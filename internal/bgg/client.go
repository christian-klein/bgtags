package bgg

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const BaseURL = "https://boardgamegeek.com/xmlapi2"

// SanitizeToken cleans user-provided tokens, stripping "v1:", "v1: ", "Bearer ", "bearer ", and surrounding spaces
func SanitizeToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	token = strings.TrimPrefix(token, "v1:")
	token = strings.TrimPrefix(token, "v1: ")
	token = strings.TrimSpace(token)
	return token
}

// Client is the BoardGameGeek XMLAPI2 client
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	ApiToken   string
}

func NewClient(apiToken string) *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		BaseURL:  BaseURL,
		ApiToken: SanitizeToken(apiToken),
	}
}

// SearchItem represents an item returned from BGG search
type SearchItem struct {
	ID            int    `xml:"id,attr"`
	Type          string `xml:"type,attr"`
	Name          string
	YearPublished int
}

type rawSearchItem struct {
	ID   int    `xml:"id,attr"`
	Type string `xml:"type,attr"`
	Name struct {
		Value string `xml:"value,attr"`
	} `xml:"name"`
	YearPublished struct {
		Value int `xml:"value,attr"`
	} `xml:"yearpublished"`
}

type rawSearchResponse struct {
	Items []rawSearchItem `xml:"item"`
}

// GameDetails contains detailed information about a board game fetched from BGG
type GameDetails struct {
	ID          int
	Name        string
	Year        int
	Image       string
	Thumbnail   string
	Description string
	MinPlayers  int
	MaxPlayers  int
	BestPlayers string
	Complexity  float64
	Rating      float64
	BggURL      string
}

type rawThingItem struct {
	ID        int    `xml:"id,attr"`
	Type      string `xml:"type,attr"`
	Thumbnail string `xml:"thumbnail"`
	Image     string `xml:"image"`
	Names     []struct {
		Type  string `xml:"type,attr"`
		Value string `xml:"value,attr"`
	} `xml:"name"`
	YearPublished struct {
		Value int `xml:"value,attr"`
	} `xml:"yearpublished"`
	Description string `xml:"description"`
	MinPlayers  struct {
		Value int `xml:"value,attr"`
	} `xml:"minplayers"`
	MaxPlayers struct {
		Value int `xml:"value,attr"`
	} `xml:"maxplayers"`
	Polls []struct {
		Name       string `xml:"name,attr"`
		TotalVotes int    `xml:"totalvotes,attr"`
		Results    []struct {
			NumPlayers string `xml:"numplayers,attr"`
			Results    []struct {
				Value    string `xml:"value,attr"`
				NumVotes int    `xml:"numvotes,attr"`
			} `xml:"result"`
		} `xml:"results"`
	} `xml:"poll"`
	Statistics struct {
		Ratings struct {
			Average struct {
				Value float64 `xml:"value,attr"`
			} `xml:"average"`
			AverageWeight struct {
				Value float64 `xml:"value,attr"`
			} `xml:"averageweight"`
		} `xml:"ratings"`
	} `xml:"statistics"`
}

type rawThingResponse struct {
	Items []rawThingItem `xml:"item"`
}

// Search searches BoardGameGeek for board games and expansions matching the query
func (c *Client) Search(query string) ([]SearchItem, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/search?query=%s&type=boardgame,boardgameexpansion", c.BaseURL, url.QueryEscape(query))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "bgtags/2.0")
	if c.ApiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.ApiToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("[BGG API] 401 Unauthorized for URL %s (token present: %v, length: %d). Response: %s", u, c.ApiToken != "", len(c.ApiToken), strings.TrimSpace(string(body)))
		return nil, fmt.Errorf("BGG API Error: Unauthorized (401). If using an API token, verify it in Settings (BGG rejected: %s)", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("[BGG API] HTTP %d for URL %s. Response: %s", resp.StatusCode, u, strings.TrimSpace(string(body)))
		return nil, fmt.Errorf("BGG API Error: status %d (%s)", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawResp rawSearchResponse
	if err := xml.Unmarshal(body, &rawResp); err != nil {
		return nil, err
	}

	var results []SearchItem
	for _, it := range rawResp.Items {
		results = append(results, SearchItem{
			ID:            it.ID,
			Type:          it.Type,
			Name:          it.Name.Value,
			YearPublished: it.YearPublished.Value,
		})
	}
	return results, nil
}

// GetThingDetails fetches full metadata for a single BGG ID
func (c *Client) GetThingDetails(bggID int) (*GameDetails, error) {
	if bggID <= 0 {
		return nil, fmt.Errorf("invalid bgg ID: %d", bggID)
	}

	u := fmt.Sprintf("%s/thing?id=%d&stats=1", c.BaseURL, bggID)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "bgtags/2.0")
	if c.ApiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.ApiToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("[BGG API] 401 Unauthorized for thing %d (token present: %v, length: %d). Response: %s", bggID, c.ApiToken != "", len(c.ApiToken), strings.TrimSpace(string(body)))
		return nil, fmt.Errorf("BGG API Error: Unauthorized (401). If using an API token, verify it in Settings (BGG rejected: %s)", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("[BGG API] HTTP %d for thing %d. Response: %s", resp.StatusCode, bggID, strings.TrimSpace(string(body)))
		return nil, fmt.Errorf("BGG API Error: status %d (%s)", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rawResp rawThingResponse
	if err := xml.NewDecoder(resp.Body).Decode(&rawResp); err != nil {
		return nil, err
	}
	if len(rawResp.Items) == 0 {
		return nil, fmt.Errorf("no game found on BGG with ID %d", bggID)
	}

	item := rawResp.Items[0]
	details := &GameDetails{
		ID:          item.ID,
		Thumbnail:   item.Thumbnail,
		Image:       item.Image,
		Year:        item.YearPublished.Value,
		Description: item.Description,
		MinPlayers:  item.MinPlayers.Value,
		MaxPlayers:  item.MaxPlayers.Value,
		Complexity:  item.Statistics.Ratings.AverageWeight.Value,
		Rating:      item.Statistics.Ratings.Average.Value,
		BggURL:      fmt.Sprintf("https://boardgamegeek.com/boardgame/%d", item.ID),
	}

	// Pick primary name
	for _, n := range item.Names {
		if n.Type == "primary" {
			details.Name = n.Value
			break
		}
	}
	if details.Name == "" && len(item.Names) > 0 {
		details.Name = item.Names[0].Value
	}

	// Determine best player count from community poll
	details.BestPlayers = extractBestPlayers(item.Polls)

	return details, nil
}

func extractBestPlayers(polls []struct {
	Name       string `xml:"name,attr"`
	TotalVotes int    `xml:"totalvotes,attr"`
	Results    []struct {
		NumPlayers string `xml:"numplayers,attr"`
		Results    []struct {
			Value    string `xml:"value,attr"`
			NumVotes int    `xml:"numvotes,attr"`
		} `xml:"result"`
	} `xml:"results"`
}) string {
	for _, p := range polls {
		if p.Name != "suggested_numplayers" || p.TotalVotes < 1 {
			continue
		}
		var maxVotes int
		var bestCount string

		for _, r := range p.Results {
			for _, res := range r.Results {
				if res.Value == "Best" && res.NumVotes > maxVotes {
					maxVotes = res.NumVotes
					bestCount = r.NumPlayers
				}
			}
		}

		if bestCount != "" {
			// clean up (e.g., "4+" or "4")
			return strings.TrimSpace(bestCount)
		}
	}
	return ""
}

// ExtractBggIDFromURL parses a BoardGameGeek URL or numeric string and returns the ID
func ExtractBggIDFromURL(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	// Direct integer
	if id, err := strconv.Atoi(raw); err == nil && id > 0 {
		return id, true
	}

	// URL formats: /boardgame/12345 or /boardgameexpansion/12345
	lower := strings.ToLower(raw)
	markers := []string{"/boardgame/", "/boardgameexpansion/"}
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx != -1 {
			rest := raw[idx+len(marker):]
			parts := strings.Split(rest, "/")
			if len(parts) > 0 {
				if id, err := strconv.Atoi(parts[0]); err == nil && id > 0 {
					return id, true
				}
			}
		}
	}
	return 0, false
}
